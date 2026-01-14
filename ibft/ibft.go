package ibft

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/usernamenenad/bft-quic/core"
)

type Ibft struct {
	node    IbftNode
	state   *State
	config  *Config
	network core.Transport

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
		node: IbftNode{
			Id: nodeId,
		},
		config: config,
		logger: logger,
	}
}

func (ibft *Ibft) Start(
	ctx context.Context,
	instance ConsensusInstance,
	inputValue Value,
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

func (ibft *Ibft) handleMessage(msg *IbftMessage) {
	switch msg.MessageType {
	case IbftMessageTypePrePrepare:
	case IbftMessageTypePrepare:
	case IbftMessageTypeCommit:
	case IbftMessageTypeRoundChange:
	default:
		return
	}
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
