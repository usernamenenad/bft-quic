package ibft

import (
	"fmt"
	"sync"

	"github.com/usernamenenad/bft-quic/core"
)

type Store struct {
	store map[string][]core.Message
	mu    sync.RWMutex
}

func NewStore() *Store {
	return &Store{
		store: make(map[string][]core.Message),
	}
}

func (s *Store) AddMessage(msg core.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := msg.(*Message)
	if !ok {
		return fmt.Errorf("error when casting message")
	}

	key := string(m.Instance) + "-" + fmt.Sprint(m.Round) + "-" + m.MessageType.String()

	s.store[key] = append(s.store[key], m)

	return nil
}

func (s *Store) GetMessagesByKey(key string) ([]core.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.store[key], nil
}
