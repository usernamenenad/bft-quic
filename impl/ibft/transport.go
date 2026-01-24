package ibft

import (
	"context"
	"sync"
	"time"

	"github.com/usernamenenad/bft-quic/core"
)

type Transport struct {
	subs  []chan core.Message
	mu    sync.RWMutex
	ready sync.WaitGroup
}

func NewTransport(numberOfNodes int) *Transport {
	t := &Transport{}
	t.ready.Add(5)

	return t
}

func (t *Transport) Broadcast(ctx context.Context, msg core.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, ch := range t.subs {
		ch <- msg
	}

	return nil
}

func (t *Transport) Send(ctx context.Context, nodeId core.NodeId, msg core.Message) error {
	time.Sleep(500)
	return nil
}

func (t *Transport) Subscribe() <-chan core.Message {
	ch := make(chan core.Message, 16)

	t.mu.Lock()
	t.subs = append(t.subs, ch)
	t.mu.Unlock()

	t.ready.Done()

	return ch
}

func (t *Transport) WaitForReady() {
	t.ready.Wait()
}
