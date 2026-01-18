package core

import "context"

type Message interface{}

// Represents a general transport interface for BFT algorithms
type Transport interface {
	Broadcast(ctx context.Context, msg Message) error

	Send(ctx context.Context, nodeId NodeId, msg Message)

	Subscribe() <-chan Message
}
