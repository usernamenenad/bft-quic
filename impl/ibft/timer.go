package ibft

import (
	"context"
	"sync"
	"time"

	"github.com/usernamenenad/bft-quic/core"
)

type Timer struct {
	mu       sync.Mutex
	timer    *time.Timer
	expireCh chan core.Round
}

func NewTimer() *Timer {
	return &Timer{
		expireCh: make(chan core.Round, 1),
	}
}

func (t *Timer) Start(ctx context.Context, round core.Round, duration time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.timer != nil {
		t.timer.Stop()
	}

	t.timer = time.AfterFunc(duration, func() {
		select {
		case <-ctx.Done():
			return
		default:
			t.mu.Lock()
			defer t.mu.Unlock()

			select {
			case t.expireCh <- round:
			default:
			}
		}
	})
}

func (t *Timer) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.timer != nil {
		t.timer.Stop()
	}
}

func (t *Timer) GetExpiryChan() <-chan core.Round {
	return t.expireCh
}
