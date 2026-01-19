package ibft

import (
	"context"
	"time"

	"github.com/usernamenenad/bft-quic/core"
)

type Transport struct{}

func NewTransport() *Transport {
	return &Transport{}
}

func (t *Transport) Broadcast(ctx context.Context, msg core.Message) error {
	time.Sleep(500)
	return nil
}

func (t *Transport) Send(ctx context.Context, nodeId core.NodeId, msg core.Message) error {
	time.Sleep(500)
	return nil
}

func (t *Transport) Subscribe() <-chan core.Message {
	return make(<-chan core.Message)
}
