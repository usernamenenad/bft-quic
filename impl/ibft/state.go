package ibft

import (
	"sync"

	"github.com/usernamenenad/bft-quic/core"
)

type State struct {
	mu            sync.RWMutex
	Instance      core.Instance
	Round         core.Round
	PreparedRound core.Round
	PreparedValue core.Value
	InputValue    core.Value
	Decided       bool
	DecidedValue  core.Value
}

func NewState(instance core.Instance, inputValue core.Value) *State {
	return &State{
		Instance:      instance,
		Round:         1,
		PreparedRound: 0,
		PreparedValue: nil,
		InputValue:    inputValue,
		Decided:       false,
	}
}
