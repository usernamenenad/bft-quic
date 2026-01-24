package ibft

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/usernamenenad/bft-quic/core"
)

type Ibft struct {
	nodeData Node

	state     *State
	config    *Config
	validator *Validator
	network   core.Transport
	store     core.Store
	timer     *Timer

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	logger *slog.Logger
}

func NewIbft(
	nodeId core.NodeId,
	config *Config,
	network core.Transport,
	store core.Store,
	logger *slog.Logger,
) *Ibft {
	if logger == nil {
		logger = slog.Default()
	}

	return &Ibft{
		nodeData:  *NewNode(nodeId),
		config:    config,
		validator: NewValidator(config),
		network:   network,
		store:     store,
		logger:    logger,
		timer:     NewTimer(),
	}
}

func (ibft *Ibft) Start(
	ctx context.Context,
	instance core.Instance,
	inputValue core.Value,
) error {
	ibft.ctx, ibft.cancel = context.WithCancel(ctx)
	ibft.state = NewState(instance, inputValue)

	ch := ibft.network.Subscribe()
	ibft.wg.Add(1)
	go ibft.startMessageHandler(ch)

	ibft.wg.Add(1)
	go ibft.startTimerHandler()

	if ibft.nodeData.IsLeader(instance, 1) {
		ibft.network.WaitForReady()

		prePrepareMsg := &Message{
			MessageType: MessageTypePrePrepare,
			From:        ibft.nodeData.GetNodeId(),
			Instance:    instance,
			Round:       1,
			Value:       inputValue,
		}

		if err := ibft.network.Broadcast(ctx, prePrepareMsg); err != nil {
			return fmt.Errorf("failed to broadcast %s: %w", MessageTypePrePrepare.String(), err)
		}
	}

	// start timer
	ibft.timer.Start(
		ibft.ctx,
		ibft.state.Round,
		ibft.config.Timeout(1),
	)

	return nil
}

func (ibft *Ibft) Stop() {
	if ibft.cancel != nil {
		ibft.cancel()
	}

	ibft.wg.Wait()
}

func (ibft *Ibft) handleMessage(msg *Message) error {
	switch msg.MessageType {
	case MessageTypePrePrepare:
		return ibft.handlePrePrepare(msg)
	case MessageTypePrepare:
		return ibft.handlePrepare(msg)
	case MessageTypeCommit:
		return ibft.handleCommit(msg)
	case MessageTypeRoundChange:
		return nil
	default:
		return nil
	}
}

func (ibft *Ibft) onTimerExpired(expiredRound core.Round) {
	ibft.state.mu.Lock()
	defer ibft.state.mu.Unlock()

	if expiredRound != ibft.state.Round {
		return
	}

	// increment round
	ibft.state.Round = expiredRound + 1

	// set timer expiry based on incremented round
	ibft.timer.Start(
		ibft.ctx,
		expiredRound+1,
		ibft.config.Timeout(expiredRound+1),
	)

	// send ROUND-CHANGE request
	roundChangeMsg := &Message{
		MessageType: MessageTypeRoundChange,
		From:        ibft.nodeData.GetNodeId(),
		Round:       ibft.state.Round,
	}
	go func() {
		if err := ibft.network.Broadcast(ibft.ctx, roundChangeMsg); err != nil {
			ibft.logger.Error(
				"error broadcasting ROUND-CHANGE:",
				"nodeId", ibft.nodeData.id,
				"error", err,
			)
		}
	}()

}

// message handlers

func (ibft *Ibft) handlePrePrepare(msg *Message) error {
	ibft.state.mu.RLock()
	currentRound := ibft.state.Round
	instance := ibft.state.Instance
	ibft.state.mu.RUnlock()

	if msg.Round != currentRound {
		return nil
	}

	leader := ibft.nodeData.GetLeader(instance)
	if msg.From != leader {
		return fmt.Errorf("%s from a non-leader %s", MessageTypePrePrepare.String(), msg.From)
	}

	if !ibft.validator.JustifyPrePrepare(msg) {
		return fmt.Errorf("message did not pass justification")
	}

	prepareMsg := &Message{
		MessageType: MessageTypePrepare,
		From:        ibft.nodeData.GetNodeId(),
		Instance:    msg.Instance,
		Round:       msg.Round,
		Value:       msg.Value,
	}

	ibft.store.AddMessage(prepareMsg)

	go func() {
		if err := ibft.network.Broadcast(ibft.ctx, prepareMsg); err != nil {
			ibft.logger.Error(
				"error broadcasting PRE-PREPARE:",
				"id", ibft.nodeData.GetNodeId(),
				"type", prepareMsg.MessageType.String(),
				"error", err,
			)
		}
	}()

	// reset timer by starting again
	ibft.timer.Start(
		ibft.ctx,
		msg.Round,
		ibft.config.Timeout(msg.Round),
	)

	return nil
}

func (ibft *Ibft) handlePrepare(msg *Message) error {
	ibft.state.mu.Lock()
	defer ibft.state.mu.Unlock()

	if ibft.state.PrepareQuorumReached {
		return nil
	}

	// add PREPARE message to store, assuming it is valid
	if err := ibft.store.AddMessage(msg); err != nil {
		ibft.logger.Warn("message not added to PREPARE log")
		return err
	}

	// check if we have a quorum of PREPARE messages
	key := string(msg.Instance) + "-" + fmt.Sprint(msg.Round) + "-" + MessageTypePrepare.String()
	prepareMsgs, err := ibft.store.GetMessagesByKey(key)
	if err != nil {
		return nil
	}

	if len(prepareMsgs) < int(ibft.config.QuorumSize()) {
		return nil
	}

	// We have quorum and haven't sent COMMIT yet - mark as sent
	ibft.state.PrepareQuorumReached = true
	ibft.state.PreparedRound = msg.Round
	ibft.state.PreparedValue = msg.Value

	commitMsg := &Message{
		MessageType:   MessageTypeCommit,
		From:          ibft.nodeData.GetNodeId(),
		PreparedRound: &msg.Round,
		PreparedValue: msg.Value,
	}

	ibft.store.AddMessage(commitMsg)

	// Broadcast in goroutine to avoid blocking
	go func() {
		if err := ibft.network.Broadcast(ibft.ctx, commitMsg); err != nil {
			ibft.logger.Error(
				"error broadcasting COMMIT:",
				"nodeId", ibft.nodeData.id,
				"error", err,
			)
		}
	}()

	return nil
}

func (ibft *Ibft) handleCommit(msg *Message) error {
	ibft.state.mu.RLock()
	if ibft.state.Decided {
		ibft.state.mu.RUnlock()
		return nil // already decided!
	}
	ibft.state.mu.RUnlock()

	// add COMMIT message to store, assuming it is valid
	if err := ibft.store.AddMessage(msg); err != nil {
		ibft.logger.Warn("message not added to COMMIT log")
	}

	// check if we have a quorum of COMMIT messages
	// if we do, set prepared values and broadcast decisio

	key := string(msg.Instance) + "-" + fmt.Sprint(msg.Round) + "-" + MessageTypeCommit.String() // COMMIT key
	commitMsgs, err := ibft.store.GetMessagesByKey(key)
	if err != nil {
		// error when fetching messages, abort handling
		return nil
	}

	if len(commitMsgs) < int(ibft.config.QuorumSize()) {
		ibft.logger.Debug(
			"no quorum of COMMIT messages",
			"id", ibft.nodeData.GetNodeId(),
		)
		return nil
	}

	ibft.state.mu.Lock()
	ibft.state.Decided = true
	ibft.state.mu.Unlock()

	ibft.logger.Info(
		"decided!",
		"id", ibft.nodeData.GetNodeId(),
		"count",
		fmt.Sprint(len(commitMsgs)),
	)

	return nil
}

func (ibft *Ibft) startMessageHandler(ch <-chan core.Message) {
	defer ibft.wg.Done()

	ibft.logger.Info("start listening on messages", "id", ibft.nodeData.id)

	for {
		select {
		case <-ibft.ctx.Done():
			return
		case msg := <-ch:
			if msg == nil {
				return
			}

			ibftMsg, ok := msg.(*Message)
			if !ok {
				ibft.logger.Error("received message of unexpected type", "type", fmt.Sprintf("%T", msg))
				continue
			}

			ibft.logger.Info(
				"received message",
				"id", ibft.nodeData.GetNodeId(),
				"from", ibftMsg.From,
				"message-type", ibftMsg.MessageType.String(),
			)

			ibft.handleMessage(ibftMsg)
		}
	}
}

func (ibft *Ibft) startTimerHandler() {
	defer ibft.wg.Done()

	ibft.logger.Debug("start timer handler")

	for {
		select {
		case <-ibft.ctx.Done():
			return
		case round := <-ibft.timer.GetExpiryChan():
			ibft.logger.Warn("timer expired!")
			ibft.onTimerExpired(round)
		}
	}
}
