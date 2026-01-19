package ibft

import (
	"github.com/usernamenenad/bft-quic/core"
)

type Validator struct {
	Config *Config
}

func NewValidator(config *Config) *Validator {
	return &Validator{
		Config: config,
	}
}

func (v *Validator) HighestPrepared(messages []*Message) (*core.Round, core.Value) {
	var highestRound *core.Round = nil
	var highestValue core.Value = nil

	for _, msg := range messages {
		if msg.PreparedRound == nil {
			continue
		}

		if highestRound == nil || *msg.PreparedRound > *highestRound {
			highestRound = msg.PreparedRound
			highestValue = msg.PreparedValue
		}
	}

	return highestRound, highestValue
}

// Verifies that the PRE-PREPARE message is justified
func (v *Validator) JustifyPrePrepare(msg *Message) bool {
	if msg.MessageType != MessageTypePrePrepare {
		return false
	}

	// if first round, then it is justified
	if msg.Round == 1 {
		return true
	}

	// else, there must be a quorum length of ROUND-CHANGE certificate (Qrc)
	if msg.RoundChangeCert == nil || len(msg.RoundChangeCert) < int(v.Config.QuorumSize()) {
		return false
	}

	return v.JustifyRoundChange(msg.RoundChangeCert, msg.Round, msg.Value)
}

// Verifies that the set of ROUND-CHANGE messages is justified
//
// A set Qrc of ROUND-CHANGE messages is justified if:
//
// - J1:  All messages must have prepared round and prepared value set to nil
//
// - J2:  There exists a quorum of PREPARE messages for the highest prepared value
func (v *Validator) JustifyRoundChange(
	roundChangeMsgs RoundChangeCert,
	round core.Round,
	proposedValue core.Value,
) bool {
	if len(roundChangeMsgs) < int(v.Config.QuorumSize()) {
		return false
	}

	for _, rcMsg := range roundChangeMsgs {
		if rcMsg.MessageType != MessageTypeRoundChange || rcMsg.Round != round {
			return false
		}
	}

	// case J1 - All are unprepared
	highestRound, highestValue := v.HighestPrepared(roundChangeMsgs)
	if highestRound == nil {
		return true
	}

	if proposedValue == nil || !proposedValue.Equal(highestValue) {
		return false
	}

	prepareCount := make(map[core.NodeId]bool)
	for _, rcMsg := range roundChangeMsgs {
		if rcMsg.PrepareCert == nil {
			continue
		}

		for _, prepMsg := range rcMsg.PrepareCert {
			if prepMsg.MessageType != MessageTypePrepare {
				continue
			}
			if prepMsg.Round != *highestRound {
				continue
			}
			if prepMsg.Value == nil || !prepMsg.Value.Equal(highestValue) {
				continue
			}

			prepareCount[prepMsg.From] = true
		}
	}

	return len(prepareCount) >= int(v.Config.QuorumSize())
}
