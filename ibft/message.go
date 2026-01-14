package ibft

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
	Instance    ConsensusInstance
	Round       Round
	Value       Value

	PreparedRound Round
	PreparedValue Value
}
