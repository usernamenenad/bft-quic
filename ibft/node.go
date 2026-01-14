package ibft

import (
	"github.com/usernamenenad/bft-quic/core"
)

type IbftNode struct {
	Id core.NodeId
}

func NewIbftNode(nodeId core.NodeId) *IbftNode {
	return &IbftNode{
		Id: nodeId,
	}
}

func (ibft *IbftNode) GetNodeId() core.NodeId {
	return ibft.Id
}

// TODO: mechanism for determining a leader
func (ibft *IbftNode) IsLeader(instance ConsensusInstance, round Round) bool {
	return true
}
