package ibft

import "github.com/usernamenenad/bft-quic/core"

type IbftMessageType uint8

const (
	IbftMessageTypePrePrepare IbftMessageType = iota
	IbftMessageTypePrepare
	IbftMessageTypeCommit
	IbftMessageTypeRoundChange
)

func (mt IbftMessageType) String() string {
	switch mt {
	case IbftMessageTypePrePrepare:
		return "PRE-PREPARE"
	case IbftMessageTypePrepare:
		return "PREPARE"
	case IbftMessageTypeCommit:
		return "COMMIT"
	case IbftMessageTypeRoundChange:
		return "ROUND-CHANGE"
	default:
		return "UNKNOWN"
	}
}

type IbftMessage struct {
	MessageType IbftMessageType
	From        core.NodeId
	Instance    core.Instance
	Round       core.Round
	Value       *Value

	PreparedRound *core.Round
	PreparedValue *Value

	RoundChangeCert RoundChangeCert
	PrepareCert     PrepareCert
}
