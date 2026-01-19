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

	ibft.wg.Add(1)
	go ibft.startMessageHandler()

	ibft.wg.Add(1)
	go ibft.startTimerHandler()

	if ibft.nodeData.IsLeader(instance, 1) {
		msg := &Message{
			MessageType: MessageTypePrePrepare,
			Instance:    instance,
			Round:       1,
			Value:       inputValue,
		}
		if err := ibft.network.Broadcast(ctx, msg); err != nil {
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
		return nil
	case MessageTypeCommit:
		return nil
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

	var prepareCert []*Message

	prepareMsgs, err := ibft.store.GetMessagesByKey("")
	if err != nil {
		prepareCert = make([]*Message, 0)
	} else {
		// TODO: see if there's a better way for this
		for _, msg := range prepareMsgs {
			prepareMsg, ok := msg.(*Message)
			if !ok {
				continue
			}
			prepareCert = append(prepareCert, prepareMsg)
		}
	}

	// send ROUND-CHANGE request
	roundChangeMsg := &Message{
		MessageType: MessageTypeRoundChange,
		PrepareCert: prepareCert,
	}

	go func() {
		if err := ibft.network.Broadcast(ibft.ctx, roundChangeMsg); err != nil {
			ibft.logger.Error(
				"error broadcasting ROUND-CHANGE:",
				"nodeId", ibft.nodeData.Id,
				"error", err,
			)
		}
	}()

}

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

	ibft.logger.Info(
		"received a valid message",
		"type", MessageTypePrePrepare.String(),
		"round", msg.Round,
		"value", msg.Value,
	)

	prepareMsg := &Message{
		MessageType: MessageTypePrepare,
		Instance:    msg.Instance,
		Round:       msg.Round,
		Value:       msg.Value,
	}
	ibft.store.AddMessage(prepareMsg)
	go func() {
		if err := ibft.network.Broadcast(ibft.ctx, prepareMsg); err != nil {
			ibft.logger.Error(
				"error broadcasting PREPARE:",
				"nodeId", ibft.nodeData.Id,
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

func (ibft *Ibft) startMessageHandler() {
	defer ibft.wg.Done()

	ibft.logger.Info("start listening on messages")

	for {
		select {
		case <-ibft.ctx.Done():
			return
		case msg := <-ibft.network.Subscribe():
			if msg == nil {
				return
			}

			ibftMsg, ok := msg.(*Message)
			if !ok {
				ibft.logger.Error("received message of unexpected type", "type", fmt.Sprintf("%T", msg))
				continue
			}
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
