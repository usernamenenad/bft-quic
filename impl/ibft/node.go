package ibft

import (
	"github.com/usernamenenad/bft-quic/core"
)

type Node struct {
	Id core.NodeId
}

func NewNode(nodeId core.NodeId) *Node {
	return &Node{
		Id: nodeId,
	}
}

func (ibft *Node) GetNodeId() core.NodeId {
	return ibft.Id
}

func (ibft *Node) IsLeader(instance core.Instance, round core.Round) bool {
	return ibft.Id == ibft.GetLeader(instance)
}

// TODO: mechanism for determining a leader
func (ibft *Node) GetLeader(instance core.Instance) core.NodeId {
	return ""
}
