package ibft

import (
	"encoding/json"
	"fmt"

	"github.com/usernamenenad/bft-quic/core"
)

// wireMessage is the JSON-serializable representation of an IBFT Message.
// It avoids interface fields and nil-pointer ambiguity.
type wireMessage struct {
	MessageType     uint8          `json:"t"`
	From            string         `json:"f"`
	Instance        string         `json:"i"`
	Round           uint64         `json:"r"`
	Value           []byte         `json:"v,omitempty"`
	PreparedRound   *uint64        `json:"pr,omitempty"`
	PreparedValue   []byte         `json:"pv,omitempty"`
	RoundChangeCert []wireMessage  `json:"rcc,omitempty"`
	PrepareCert     []wireMessage  `json:"pc,omitempty"`
}

// Codec serializes and deserializes IBFT messages using JSON.
type Codec struct{}

func NewCodec() *Codec {
	return &Codec{}
}

func (c *Codec) Marshal(msg core.Message) ([]byte, error) {
	m, ok := msg.(*Message)
	if !ok {
		return nil, fmt.Errorf("unsupported message type: %T", msg)
	}
	return json.Marshal(c.toWire(m))
}

func (c *Codec) Unmarshal(data []byte) (core.Message, error) {
	var w wireMessage
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	return c.fromWire(&w), nil
}

func (c *Codec) toWire(m *Message) *wireMessage {
	w := &wireMessage{
		MessageType: uint8(m.MessageType),
		From:        string(m.From),
		Instance:    string(m.Instance),
		Round:       uint64(m.Round),
	}

	if m.Value != nil {
		if v, ok := m.Value.(*Value); ok && !v.IsNil() {
			w.Value = v.Data
		}
	}

	if m.PreparedRound != nil {
		r := uint64(*m.PreparedRound)
		w.PreparedRound = &r
	}

	if m.PreparedValue != nil {
		if v, ok := m.PreparedValue.(*Value); ok && !v.IsNil() {
			w.PreparedValue = v.Data
		}
	}

	if len(m.RoundChangeCert) > 0 {
		w.RoundChangeCert = make([]wireMessage, len(m.RoundChangeCert))
		for i, rc := range m.RoundChangeCert {
			w.RoundChangeCert[i] = *c.toWire(rc)
		}
	}

	if len(m.PrepareCert) > 0 {
		w.PrepareCert = make([]wireMessage, len(m.PrepareCert))
		for i, pc := range m.PrepareCert {
			w.PrepareCert[i] = *c.toWire(pc)
		}
	}

	return w
}

func (c *Codec) fromWire(w *wireMessage) *Message {
	m := &Message{
		MessageType: MessageType(w.MessageType),
		From:        core.NodeId(w.From),
		Instance:    core.Instance(w.Instance),
		Round:       core.Round(w.Round),
	}

	if len(w.Value) > 0 {
		m.Value = NewValue(w.Value)
	}

	if w.PreparedRound != nil {
		r := core.Round(*w.PreparedRound)
		m.PreparedRound = &r
	}

	if len(w.PreparedValue) > 0 {
		m.PreparedValue = NewValue(w.PreparedValue)
	}

	if len(w.RoundChangeCert) > 0 {
		m.RoundChangeCert = make(RoundChangeCert, len(w.RoundChangeCert))
		for i := range w.RoundChangeCert {
			m.RoundChangeCert[i] = c.fromWire(&w.RoundChangeCert[i])
		}
	}

	if len(w.PrepareCert) > 0 {
		m.PrepareCert = make(PrepareCert, len(w.PrepareCert))
		for i := range w.PrepareCert {
			m.PrepareCert[i] = c.fromWire(&w.PrepareCert[i])
		}
	}

	return m
}
