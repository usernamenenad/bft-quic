package ibft_test

import (
	"testing"

	"github.com/usernamenenad/bft-quic/core"
	"github.com/usernamenenad/bft-quic/impl/ibft"
)

func TestCodecRoundTrip(t *testing.T) {
	codec := ibft.NewCodec()

	t.Run("simple message", func(t *testing.T) {
		msg := &ibft.Message{
			MessageType: ibft.MessageTypePrePrepare,
			From:        "node1",
			Instance:    "inst-1",
			Round:       1,
			Value:       ibft.NewValue([]byte("hello")),
		}

		data, err := codec.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		decoded, err := codec.Unmarshal(data)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		dm := decoded.(*ibft.Message)
		if dm.MessageType != msg.MessageType {
			t.Errorf("MessageType: got %v, want %v", dm.MessageType, msg.MessageType)
		}
		if dm.From != msg.From {
			t.Errorf("From: got %v, want %v", dm.From, msg.From)
		}
		if dm.Instance != msg.Instance {
			t.Errorf("Instance: got %v, want %v", dm.Instance, msg.Instance)
		}
		if dm.Round != msg.Round {
			t.Errorf("Round: got %v, want %v", dm.Round, msg.Round)
		}
		if !dm.Value.Equal(msg.Value) {
			t.Errorf("Value mismatch")
		}
	})

	t.Run("nil PreparedRound", func(t *testing.T) {
		msg := &ibft.Message{
			MessageType:   ibft.MessageTypeRoundChange,
			From:          "node2",
			Instance:      "inst-1",
			Round:         2,
			PreparedRound: nil,
			PreparedValue: nil,
		}

		data, err := codec.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		decoded, err := codec.Unmarshal(data)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		dm := decoded.(*ibft.Message)
		if dm.PreparedRound != nil {
			t.Errorf("PreparedRound should be nil, got %v", *dm.PreparedRound)
		}
		if dm.PreparedValue != nil {
			t.Errorf("PreparedValue should be nil")
		}
	})

	t.Run("with PreparedRound", func(t *testing.T) {
		pr := core.Round(3)
		msg := &ibft.Message{
			MessageType:   ibft.MessageTypeRoundChange,
			From:          "node3",
			Instance:      "inst-1",
			Round:         4,
			PreparedRound: &pr,
			PreparedValue: ibft.NewValue([]byte("prepared")),
		}

		data, err := codec.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		decoded, err := codec.Unmarshal(data)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		dm := decoded.(*ibft.Message)
		if dm.PreparedRound == nil {
			t.Fatal("PreparedRound should not be nil")
		}
		if *dm.PreparedRound != pr {
			t.Errorf("PreparedRound: got %v, want %v", *dm.PreparedRound, pr)
		}
		if !dm.PreparedValue.Equal(msg.PreparedValue) {
			t.Errorf("PreparedValue mismatch")
		}
	})

	t.Run("with RoundChangeCert", func(t *testing.T) {
		pr := core.Round(1)
		msg := &ibft.Message{
			MessageType: ibft.MessageTypePrePrepare,
			From:        "node1",
			Instance:    "inst-1",
			Round:       2,
			Value:       ibft.NewValue([]byte("value")),
			RoundChangeCert: ibft.RoundChangeCert{
				{
					MessageType:   ibft.MessageTypeRoundChange,
					From:          "node2",
					Round:         2,
					PreparedRound: &pr,
					PreparedValue: ibft.NewValue([]byte("prepared")),
				},
				{
					MessageType: ibft.MessageTypeRoundChange,
					From:        "node3",
					Round:       2,
				},
			},
		}

		data, err := codec.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		decoded, err := codec.Unmarshal(data)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		dm := decoded.(*ibft.Message)
		if len(dm.RoundChangeCert) != 2 {
			t.Fatalf("RoundChangeCert length: got %d, want 2", len(dm.RoundChangeCert))
		}
		if dm.RoundChangeCert[0].From != "node2" {
			t.Errorf("RCC[0].From: got %v, want node2", dm.RoundChangeCert[0].From)
		}
		if dm.RoundChangeCert[0].PreparedRound == nil || *dm.RoundChangeCert[0].PreparedRound != 1 {
			t.Errorf("RCC[0].PreparedRound should be 1")
		}
		if dm.RoundChangeCert[1].PreparedRound != nil {
			t.Errorf("RCC[1].PreparedRound should be nil")
		}
	})
}
