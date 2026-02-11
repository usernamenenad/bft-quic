package ibft_test

import (
	"context"
	"testing"
	"time"

	"github.com/usernamenenad/bft-quic/core"
	"github.com/usernamenenad/bft-quic/impl/ibft"
)

func TestHappyPath(t *testing.T) {
	validators := []core.NodeId{"node1", "node2", "node3", "node4"}
	config := ibft.NewConfig(validators, func(r core.Round) time.Duration {
		return 10 * time.Second
	})
	network := ibft.NewTransport(len(validators))

	value := ibft.NewValue([]byte("test-value"))
	nodes := make([]*ibft.Ibft, len(validators))
	for i, v := range validators {
		nodes[i] = ibft.NewIbft(v, config, network, ibft.NewStore(), nil)
	}

	ctx := context.Background()
	for _, node := range nodes {
		go node.Start(ctx, "instance-1", value)
	}

	// All nodes should decide
	for i, node := range nodes {
		select {
		case <-node.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("node %d timed out waiting for consensus", i)
		}
	}

	for _, node := range nodes {
		node.Stop()
	}
}

func TestHappyPathFiveNodes(t *testing.T) {
	validators := []core.NodeId{"leader1", "non-leader2", "non-leader3", "non-leader4", "non-leader5"}
	config := ibft.NewConfig(validators, func(r core.Round) time.Duration {
		return 10 * time.Second
	})
	network := ibft.NewTransport(len(validators))

	value := ibft.NewValue([]byte("value-1"))
	nodes := make([]*ibft.Ibft, len(validators))
	for i, v := range validators {
		nodes[i] = ibft.NewIbft(v, config, network, ibft.NewStore(), nil)
	}

	ctx := context.Background()
	for _, node := range nodes {
		go node.Start(ctx, "instance-1", value)
	}

	for i, node := range nodes {
		select {
		case <-node.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("node %d timed out waiting for consensus", i)
		}
	}

	for _, node := range nodes {
		node.Stop()
	}
}

func TestRoundChangeOnLeaderFailure(t *testing.T) {
	validators := []core.NodeId{"node1", "node2", "node3", "node4"}
	config := ibft.NewConfig(validators, func(r core.Round) time.Duration {
		return 500 * time.Millisecond
	})

	// Only 3 nodes connect (node1/leader is faulty)
	network := ibft.NewTransport(3)
	value := ibft.NewValue([]byte("test-value"))

	nodes := make([]*ibft.Ibft, 3)
	for i, v := range validators[1:] {
		nodes[i] = ibft.NewIbft(v, config, network, ibft.NewStore(), nil)
	}

	ctx := context.Background()
	for _, node := range nodes {
		go node.Start(ctx, "instance-1", value)
	}

	// Nodes should decide after round change (round 2 leader = node2)
	for i, node := range nodes {
		select {
		case <-node.Done():
		case <-time.After(10 * time.Second):
			t.Fatalf("node %d timed out waiting for consensus after round change", i)
		}
	}

	for _, node := range nodes {
		node.Stop()
	}
}

func TestByzantineFaultTolerance(t *testing.T) {
	// 4 nodes, 1 Byzantine (not participating). N=4, F=1, quorum=3.
	validators := []core.NodeId{"node1", "node2", "node3", "node4"}
	config := ibft.NewConfig(validators, func(r core.Round) time.Duration {
		return 10 * time.Second
	})

	// Only 3 honest nodes connect
	network := ibft.NewTransport(3)
	value := ibft.NewValue([]byte("bft-value"))

	// node1 is the leader for round 1. Start node1 + 2 others = 3 honest nodes.
	honestValidators := []core.NodeId{"node1", "node2", "node3"}
	nodes := make([]*ibft.Ibft, 3)
	for i, v := range honestValidators {
		nodes[i] = ibft.NewIbft(v, config, network, ibft.NewStore(), nil)
	}

	ctx := context.Background()
	for _, node := range nodes {
		go node.Start(ctx, "instance-1", value)
	}

	for i, node := range nodes {
		select {
		case <-node.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("node %d timed out - BFT should tolerate 1 faulty node", i)
		}
	}

	for _, node := range nodes {
		node.Stop()
	}
}

func TestMinimalQuorum(t *testing.T) {
	// Minimum IBFT network: N=4, F=1, quorum=3
	validators := []core.NodeId{"a", "b", "c", "d"}
	config := ibft.NewConfig(validators, func(r core.Round) time.Duration {
		return 10 * time.Second
	})
	network := ibft.NewTransport(4)
	value := ibft.NewValue([]byte("minimal"))

	nodes := make([]*ibft.Ibft, 4)
	for i, v := range validators {
		nodes[i] = ibft.NewIbft(v, config, network, ibft.NewStore(), nil)
	}

	ctx := context.Background()
	for _, node := range nodes {
		go node.Start(ctx, "inst", value)
	}

	for i, node := range nodes {
		select {
		case <-node.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("node %d timed out", i)
		}
	}

	for _, node := range nodes {
		node.Stop()
	}
}
