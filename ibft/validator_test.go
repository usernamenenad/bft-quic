package ibft_test

import (
	"testing"

	"github.com/usernamenenad/bft-quic/core"
	"github.com/usernamenenad/bft-quic/ibft"
)

func TestValidator(t *testing.T) {
	config := &ibft.Config{N: 5}
	validator := &ibft.Validator{Config: *config}

	t.Run("HighestPrepared", func(t *testing.T) {
		t.Run("empty messages", func(t *testing.T) {
			round, value := validator.HighestPrepared([]*ibft.IbftMessage{})
			if round != nil || value != nil {
				t.Error("Expected nil round and value for empty messages")
			}
		})

		t.Run("no prepared messages", func(t *testing.T) {
			messages := []*ibft.IbftMessage{
				{PreparedRound: nil, PreparedValue: nil},
				{PreparedRound: nil, PreparedValue: nil},
			}

			round, value := validator.HighestPrepared(messages)
			if round != nil || value != nil {
				t.Error("Expected nil round and value when no messages are prepared")
			}
		})

		t.Run("single prepared message", func(t *testing.T) {
			expectedRound := core.Round(2)
			expectedValue := &ibft.Value{Data: []byte("test")}
			messages := []*ibft.IbftMessage{
				{PreparedRound: &expectedRound, PreparedValue: expectedValue},
				{PreparedRound: nil, PreparedValue: nil},
			}

			round, value := validator.HighestPrepared(messages)
			if *round != expectedRound || !value.Equal(expectedValue) {
				t.Error("Expected to return the single prepared round and value")
			}
		})

		t.Run("multiple prepared messages", func(t *testing.T) {
			round1 := core.Round(1)
			round2 := core.Round(3)
			round3 := core.Round(2)
			value1 := &ibft.Value{Data: []byte("value1")}
			value2 := &ibft.Value{Data: []byte("value2")}
			value3 := &ibft.Value{Data: []byte("value3")}
			messages := []*ibft.IbftMessage{
				{PreparedRound: &round1, PreparedValue: value1},
				{PreparedRound: &round2, PreparedValue: value2}, // highest
				{PreparedRound: &round3, PreparedValue: value3},
			}

			round, value := validator.HighestPrepared(messages)
			if *round != round2 || !value.Equal(value2) {
				t.Error("Expected to return the highest prepared round and its value")
			}
		})
	})

	t.Run("JustifyPrePrepare", func(t *testing.T) {
		t.Run("wrong message type", func(t *testing.T) {
			msg := &ibft.IbftMessage{MessageType: ibft.IbftMessageTypePrepare}
			if validator.JustifyPrePrepare(msg) {
				t.Error("Expected false for non PRE-PREPARE messages")
			}
		})

		t.Run("first round", func(t *testing.T) {
			msg := &ibft.IbftMessage{
				MessageType: ibft.IbftMessageTypePrePrepare,
				Round:       1,
			}
			if !validator.JustifyPrePrepare(msg) {
				t.Error("Expected true for first round PRE-PREPARE")
			}
		})

		t.Run("no ROUND-CHANGE cert", func(t *testing.T) {
			msg := &ibft.IbftMessage{
				MessageType:     ibft.IbftMessageTypePrePrepare,
				Round:           2,
				RoundChangeCert: nil,
			}
			if validator.JustifyPrePrepare(msg) {
				t.Error("Expected false for no ROUND-CHANGE cert")
			}
		})

		t.Run("insufficient ROUND-CHANGE cert", func(t *testing.T) {
			msg := &ibft.IbftMessage{
				MessageType:     ibft.IbftMessageTypePrePrepare,
				Round:           2,
				RoundChangeCert: make(ibft.RoundChangeCert, 2),
			}
			if validator.JustifyPrePrepare(msg) {
				t.Error("Expected false when insufficient ROUND-CHANGE cert")
			}
		})

		t.Run("valid ROUND-CHANGE cert", func(t *testing.T) {
			value := &ibft.Value{Data: []byte("test")}
			roundChangeCert := make(ibft.RoundChangeCert, 3)
			for i := range roundChangeCert {
				roundChangeCert[i] = &ibft.IbftMessage{
					MessageType:   ibft.IbftMessageTypeRoundChange,
					Round:         2,
					PreparedRound: nil,
					PreparedValue: nil,
					From:          core.NodeId(""),
				}
			}
			msg := &ibft.IbftMessage{
				MessageType:     ibft.IbftMessageTypePrePrepare,
				Round:           2,
				Value:           value,
				RoundChangeCert: roundChangeCert,
			}

			if !validator.JustifyPrePrepare(msg) {
				t.Error("Expected true for valid round change certificate")
			}
		})
	})

	t.Run("JustifyRoundChange", func(t *testing.T) {
		t.Run("insufficient messages", func(t *testing.T) {
			roundChangeMsgs := make([]*ibft.IbftMessage, 2) // less than quorum
			if validator.JustifyRoundChange(roundChangeMsgs, 2, nil) {
				t.Error("Expected false for insufficient round change messages")
			}
		})

		t.Run("wrong message type", func(t *testing.T) {
			roundChangeMsgs := []*ibft.IbftMessage{
				{MessageType: ibft.IbftMessageTypePrepare, Round: 2}, // wrong type
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 2},
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 2},
			}
			if validator.JustifyRoundChange(roundChangeMsgs, 2, nil) {
				t.Error("Expected false for wrong message type")
			}
		})

		t.Run("wrong round", func(t *testing.T) {
			roundChangeMsgs := []*ibft.IbftMessage{
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 1}, // wrong round
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 2},
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 2},
			}
			if validator.JustifyRoundChange(roundChangeMsgs, 2, nil) {
				t.Error("Expected false for wrong round")
			}
		})

		t.Run("J1: all unprepared", func(t *testing.T) {
			roundChangeMsgs := []*ibft.IbftMessage{
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 2, PreparedRound: nil},
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 2, PreparedRound: nil},
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 2, PreparedRound: nil},
			}
			if !validator.JustifyRoundChange(roundChangeMsgs, 2, nil) {
				t.Error("Expected true for all unprepared round change messages")
			}
		})

		t.Run("J2: proposed value doesn't match highest", func(t *testing.T) {
			preparedRound := core.Round(1)
			preparedValue := &ibft.Value{Data: []byte("prepared")}
			proposedValue := &ibft.Value{Data: []byte("different")}

			roundChangeMsgs := []*ibft.IbftMessage{
				{
					MessageType:   ibft.IbftMessageTypeRoundChange,
					Round:         2,
					PreparedRound: &preparedRound,
					PreparedValue: preparedValue,
				},
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 2, PreparedRound: nil},
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 2, PreparedRound: nil},
			}

			if validator.JustifyRoundChange(roundChangeMsgs, 2, proposedValue) {
				t.Error("Expected false when proposed value doesn't match highest prepared")
			}
		})

		t.Run("J2: valid with quorum of prepare messages", func(t *testing.T) {
			preparedRound := core.Round(1)
			preparedValue := &ibft.Value{Data: []byte("prepared")}

			prepareCert := []*ibft.IbftMessage{
				{
					MessageType: ibft.IbftMessageTypePrepare,
					Round:       1,
					Value:       preparedValue,
					From:        core.NodeId("0"),
				},
				{
					MessageType: ibft.IbftMessageTypePrepare,
					Round:       1,
					Value:       preparedValue,
					From:        core.NodeId("1"),
				},
				{
					MessageType: ibft.IbftMessageTypePrepare,
					Round:       1,
					Value:       preparedValue,
					From:        core.NodeId("2"),
				},
			}

			roundChangeMsgs := []*ibft.IbftMessage{
				{
					MessageType:   ibft.IbftMessageTypeRoundChange,
					Round:         2,
					PreparedRound: &preparedRound,
					PreparedValue: preparedValue,
					PrepareCert:   prepareCert,
				},
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 2, PreparedRound: nil},
				{MessageType: ibft.IbftMessageTypeRoundChange, Round: 2, PreparedRound: nil},
			}

			if !validator.JustifyRoundChange(roundChangeMsgs, 2, preparedValue) {
				t.Error("Expected true for valid round change with prepare certificates")
			}
		})
	})
}
