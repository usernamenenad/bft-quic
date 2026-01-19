package ibft

import (
	"github.com/usernamenenad/bft-quic/core"
)

type Store struct{}

func NewStore() *Store {
	return &Store{}
}

func (s *Store) AddMessage(msg core.Message) error {
	return nil
}

func (s *Store) GetMessagesByKey(key string) ([]core.Message, error) {
	return make([]core.Message, 0), nil
}
