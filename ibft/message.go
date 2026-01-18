package ibft

import "github.com/usernamenenad/bft-quic/core"

type MessageType uint8

const (
	MessageTypePrePrepare MessageType = iota
	MessageTypePrepare
	MessageTypeCommit
	MessageTypeRoundChange
)

func (mt MessageType) String() string {
	switch mt {
	case MessageTypePrePrepare:
		return "PRE-PREPARE"
	case MessageTypePrepare:
		return "PREPARE"
	case MessageTypeCommit:
		return "COMMIT"
	case MessageTypeRoundChange:
		return "ROUND-CHANGE"
	default:
		return "UNKNOWN"
	}
}

type Message struct {
	MessageType MessageType
	From        core.NodeId
	Instance    core.Instance
	Round       core.Round
	Value       core.Value

	PreparedRound *core.Round
	PreparedValue core.Value

	RoundChangeCert RoundChangeCert
	PrepareCert     PrepareCert
}
