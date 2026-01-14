package ibft

import "sync"

type State struct {
	mu            sync.RWMutex
	Instance      ConsensusInstance
	Round         Round
	PreparedRound Round
	PreparedValue *Value
	InputValue    *Value
	Decided       bool
	DecidedValue  *Value
}

func NewState(instance ConsensusInstance, inputValue *Value) *State {
	return &State{
		Instance:      instance,
		Round:         1,
		PreparedRound: 0,
		PreparedValue: nil,
		InputValue:    inputValue,
		Decided:       false,
	}
}
