package ibft

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/usernamenenad/bft-quic/core"
)

type Ibft struct {
	node      Node
	state     *State
	config    *Config
	validator *Validator
	network   core.Transport

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	logger *slog.Logger
}

func NewIbft(
	nodeId core.NodeId,
	config *Config,
	logger *slog.Logger,
) *Ibft {
	if logger == nil {
		logger = slog.Default()
	}

	return &Ibft{
		node:   *NewNode(nodeId),
		config: config,
		validator: &Validator{
			Config: *config,
		},
		logger: logger,
	}
}

func (ibft *Ibft) Start(
	ctx context.Context,
	instance core.Instance,
	inputValue *Value,
) error {
	ibft.ctx, ibft.cancel = context.WithCancel(ctx)
	ibft.state = NewState(instance, inputValue)

	ibft.wg.Add(1)
	go ibft.startMessageListener()

	if ibft.node.IsLeader(instance, 1) {
		msg := &IbftMessage{
			MessageType: IbftMessageTypePrePrepare,
			Instance:    instance,
			Round:       ibft.state.Round,
			Value:       inputValue,
		}
		if err := ibft.network.Broadcast(ctx, msg); err != nil {
			return fmt.Errorf("failed to broadcast %s: %w", IbftMessageTypePrePrepare.String(), err)
		}
	}

	return nil
}

func (ibft *Ibft) Stop() {
	if ibft.cancel != nil {
		ibft.cancel()
	}

	ibft.wg.Wait()
}

func (ibft *Ibft) handleMessage(msg *IbftMessage) error {
	switch msg.MessageType {
	case IbftMessageTypePrePrepare:
		return ibft.handlePrePrepare(msg)
	case IbftMessageTypePrepare:
		return nil
	case IbftMessageTypeCommit:
		return nil
	case IbftMessageTypeRoundChange:
		return nil
	default:
		return nil
	}
}

func (ibft *Ibft) handlePrePrepare(msg *IbftMessage) error {
	ibft.state.mu.RLock()
	currentRound := ibft.state.Round
	instance := ibft.state.Instance
	ibft.state.mu.RUnlock()

	if msg.Round != currentRound {
		return nil
	}

	leader := ibft.node.GetLeader(instance)
	if msg.From != leader {
		return fmt.Errorf("%s from a non-leader %s", IbftMessageTypePrePrepare.String(), msg.From)
	}

	if !ibft.validator.JustifyPrePrepare(msg) {
		return fmt.Errorf("message did not pass justification")
	}

	ibft.logger.Info(
		"received a valid message",
		"type", IbftMessageTypePrePrepare.String(),
		"round", msg.Round,
		"value", msg.Value,
	)

	return nil
}

func (ibft *Ibft) startMessageListener() {
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

			ibftMsg, ok := msg.(*IbftMessage)
			if !ok {
				ibft.logger.Error("received message of unexpected type", "type", fmt.Sprintf("%T", msg))
				continue
			}
			ibft.handleMessage(ibftMsg)
		}
	}
}
