package ibft

import (
	"github.com/usernamenenad/bft-quic/core"
)

type Node struct {
	id         core.NodeId
	validators []core.NodeId
}

func NewNode(nodeId core.NodeId, validators []core.NodeId) *Node {
	return &Node{
		id:         nodeId,
		validators: validators,
	}
}

func (n *Node) GetNodeId() core.NodeId {
	return n.id
}

func (n *Node) IsLeader(instance core.Instance, round core.Round) bool {
	return n.id == n.GetLeader(instance, round)
}

// GetLeader returns the leader for the given instance and round using round-robin rotation.
func (n *Node) GetLeader(instance core.Instance, round core.Round) core.NodeId {
	if len(n.validators) == 0 {
		return ""
	}
	idx := (uint64(round) - 1) % uint64(len(n.validators))
	return n.validators[idx]
}
