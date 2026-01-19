package ibft_test

import (
	"testing"
	"time"

	"github.com/usernamenenad/bft-quic/core"
	"github.com/usernamenenad/bft-quic/impl/ibft"
)

func TestValidator(t *testing.T) {
	config := ibft.NewConfig(5, func(r core.Round) time.Duration {
		return 0
	})
	validator := ibft.NewValidator(config)

	t.Run("HighestPrepared", func(t *testing.T) {
		t.Run("empty messages", func(t *testing.T) {
			round, value := validator.HighestPrepared([]*ibft.Message{})
			if round != nil || value != nil {
				t.Error("Expected nil round and value for empty messages")
			}
		})

		t.Run("no prepared messages", func(t *testing.T) {
			messages := []*ibft.Message{
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
			expectedValue := ibft.NewValue([]byte("test"))
			messages := []*ibft.Message{
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
			value1 := ibft.NewValue([]byte("value1"))
			value2 := ibft.NewValue([]byte("value2"))
			value3 := ibft.NewValue([]byte("value3"))
			messages := []*ibft.Message{
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
			msg := &ibft.Message{MessageType: ibft.MessageTypePrepare}
			if validator.JustifyPrePrepare(msg) {
				t.Error("Expected false for non PRE-PREPARE messages")
			}
		})

		t.Run("first round", func(t *testing.T) {
			msg := &ibft.Message{
				MessageType: ibft.MessageTypePrePrepare,
				Round:       1,
			}
			if !validator.JustifyPrePrepare(msg) {
				t.Error("Expected true for first round PRE-PREPARE")
			}
		})

		t.Run("no ROUND-CHANGE cert", func(t *testing.T) {
			msg := &ibft.Message{
				MessageType:     ibft.MessageTypePrePrepare,
				Round:           2,
				RoundChangeCert: nil,
			}
			if validator.JustifyPrePrepare(msg) {
				t.Error("Expected false for no ROUND-CHANGE cert")
			}
		})

		t.Run("insufficient ROUND-CHANGE cert", func(t *testing.T) {
			msg := &ibft.Message{
				MessageType:     ibft.MessageTypePrePrepare,
				Round:           2,
				RoundChangeCert: make(ibft.RoundChangeCert, 2),
			}
			if validator.JustifyPrePrepare(msg) {
				t.Error("Expected false when insufficient ROUND-CHANGE cert")
			}
		})

		t.Run("valid ROUND-CHANGE cert", func(t *testing.T) {
			value := ibft.NewValue([]byte("test"))
			roundChangeCert := make(ibft.RoundChangeCert, 3)
			for i := range roundChangeCert {
				roundChangeCert[i] = &ibft.Message{
					MessageType:   ibft.MessageTypeRoundChange,
					Round:         2,
					PreparedRound: nil,
					PreparedValue: nil,
					From:          core.NodeId(""),
				}
			}
			msg := &ibft.Message{
				MessageType:     ibft.MessageTypePrePrepare,
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
			roundChangeMsgs := make([]*ibft.Message, 2) // less than quorum
			if validator.JustifyRoundChange(roundChangeMsgs, 2, nil) {
				t.Error("Expected false for insufficient round change messages")
			}
		})

		t.Run("wrong message type", func(t *testing.T) {
			roundChangeMsgs := []*ibft.Message{
				{MessageType: ibft.MessageTypePrepare, Round: 2}, // wrong type
				{MessageType: ibft.MessageTypeRoundChange, Round: 2},
				{MessageType: ibft.MessageTypeRoundChange, Round: 2},
			}
			if validator.JustifyRoundChange(roundChangeMsgs, 2, nil) {
				t.Error("Expected false for wrong message type")
			}
		})

		t.Run("wrong round", func(t *testing.T) {
			roundChangeMsgs := []*ibft.Message{
				{MessageType: ibft.MessageTypeRoundChange, Round: 1}, // wrong round
				{MessageType: ibft.MessageTypeRoundChange, Round: 2},
				{MessageType: ibft.MessageTypeRoundChange, Round: 2},
			}
			if validator.JustifyRoundChange(roundChangeMsgs, 2, nil) {
				t.Error("Expected false for wrong round")
			}
		})

		t.Run("J1: all unprepared", func(t *testing.T) {
			roundChangeMsgs := []*ibft.Message{
				{MessageType: ibft.MessageTypeRoundChange, Round: 2, PreparedRound: nil},
				{MessageType: ibft.MessageTypeRoundChange, Round: 2, PreparedRound: nil},
				{MessageType: ibft.MessageTypeRoundChange, Round: 2, PreparedRound: nil},
			}
			if !validator.JustifyRoundChange(roundChangeMsgs, 2, nil) {
				t.Error("Expected true for all unprepared round change messages")
			}
		})

		t.Run("J2: proposed value doesn't match highest", func(t *testing.T) {
			preparedRound := core.Round(1)
			preparedValue := ibft.NewValue([]byte("prepared"))
			proposedValue := ibft.NewValue([]byte("different"))

			roundChangeMsgs := []*ibft.Message{
				{
					MessageType:   ibft.MessageTypeRoundChange,
					Round:         2,
					PreparedRound: &preparedRound,
					PreparedValue: preparedValue,
				},
				{MessageType: ibft.MessageTypeRoundChange, Round: 2, PreparedRound: nil},
				{MessageType: ibft.MessageTypeRoundChange, Round: 2, PreparedRound: nil},
			}

			if validator.JustifyRoundChange(roundChangeMsgs, 2, proposedValue) {
				t.Error("Expected false when proposed value doesn't match highest prepared")
			}
		})

		t.Run("J2: valid with quorum of prepare messages", func(t *testing.T) {
			preparedRound := core.Round(1)
			preparedValue := ibft.NewValue([]byte("prepared"))

			prepareCert := []*ibft.Message{
				{
					MessageType: ibft.MessageTypePrepare,
					Round:       1,
					Value:       preparedValue,
					From:        core.NodeId("0"),
				},
				{
					MessageType: ibft.MessageTypePrepare,
					Round:       1,
					Value:       preparedValue,
					From:        core.NodeId("1"),
				},
				{
					MessageType: ibft.MessageTypePrepare,
					Round:       1,
					Value:       preparedValue,
					From:        core.NodeId("2"),
				},
			}

			roundChangeMsgs := []*ibft.Message{
				{
					MessageType:   ibft.MessageTypeRoundChange,
					Round:         2,
					PreparedRound: &preparedRound,
					PreparedValue: preparedValue,
					PrepareCert:   prepareCert,
				},
				{MessageType: ibft.MessageTypeRoundChange, Round: 2, PreparedRound: nil},
				{MessageType: ibft.MessageTypeRoundChange, Round: 2, PreparedRound: nil},
			}

			if !validator.JustifyRoundChange(roundChangeMsgs, 2, preparedValue) {
				t.Error("Expected true for valid round change with prepare certificates")
			}
		})
	})
}
