package core

type NodeId string

type Node interface {
	GetNodeId() NodeId
}
