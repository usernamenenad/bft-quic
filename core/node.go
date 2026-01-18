package core

type NodeId string

// Represents a general node data interface
type Node interface {
	GetNodeId() NodeId
}
