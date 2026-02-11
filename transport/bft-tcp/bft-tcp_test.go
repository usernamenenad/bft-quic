package bfttcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/usernamenenad/bft-quic/core"
	"github.com/usernamenenad/bft-quic/impl/ibft"
	bfttcp "github.com/usernamenenad/bft-quic/transport/bft-tcp"
)

func setupTCPNetwork(t *testing.T, validators []core.NodeId) ([]*bfttcp.TCPTransport, func()) {
	t.Helper()

	codec := ibft.NewCodec()

	// Phase 1: Create transports (start listeners on random ports)
	transports := make([]*bfttcp.TCPTransport, len(validators))
	for i, id := range validators {
		tr, err := bfttcp.NewTCPTransport(id, "127.0.0.1:0", codec, nil)
		if err != nil {
			t.Fatalf("failed to create transport for %s: %v", id, err)
		}
		transports[i] = tr
	}

	// Discover assigned addresses
	addrs := make(map[core.NodeId]string)
	for i, id := range validators {
		addrs[id] = transports[i].Addr()
	}

	// Phase 2: Connect peers
	for i, id := range validators {
		peers := make(map[core.NodeId]string)
		for pid, paddr := range addrs {
			if pid != id {
				peers[pid] = paddr
			}
		}
		transports[i].Connect(peers)
	}

	cleanup := func() {
		for _, tr := range transports {
			tr.Close()
		}
	}

	return transports, cleanup
}

func TestTCPHappyPath(t *testing.T) {
	validators := []core.NodeId{"node1", "node2", "node3", "node4"}
	transports, cleanup := setupTCPNetwork(t, validators)
	defer cleanup()

	config := ibft.NewConfig(validators, func(r core.Round) time.Duration {
		return 10 * time.Second
	})

	value := ibft.NewValue([]byte("tcp-test-value"))
	nodes := make([]*ibft.Ibft, len(validators))
	for i, v := range validators {
		nodes[i] = ibft.NewIbft(v, config, transports[i], ibft.NewStore(), nil)
	}

	ctx := context.Background()
	for _, node := range nodes {
		go node.Start(ctx, "instance-1", value)
	}

	for i, node := range nodes {
		select {
		case <-node.Done():
		case <-time.After(10 * time.Second):
			t.Fatalf("node %d (%s) timed out waiting for consensus", i, validators[i])
		}
	}

	for _, node := range nodes {
		node.Stop()
	}
}

func TestTCPFiveNodes(t *testing.T) {
	validators := []core.NodeId{"n1", "n2", "n3", "n4", "n5"}
	transports, cleanup := setupTCPNetwork(t, validators)
	defer cleanup()

	config := ibft.NewConfig(validators, func(r core.Round) time.Duration {
		return 10 * time.Second
	})

	value := ibft.NewValue([]byte("five-nodes"))
	nodes := make([]*ibft.Ibft, len(validators))
	for i, v := range validators {
		nodes[i] = ibft.NewIbft(v, config, transports[i], ibft.NewStore(), nil)
	}

	ctx := context.Background()
	for _, node := range nodes {
		go node.Start(ctx, "inst-1", value)
	}

	for i, node := range nodes {
		select {
		case <-node.Done():
		case <-time.After(10 * time.Second):
			t.Fatalf("node %d timed out", i)
		}
	}

	for _, node := range nodes {
		node.Stop()
	}
}

func TestTCPRoundChange(t *testing.T) {
	// 4 validators but only 3 nodes start (leader is faulty)
	validators := []core.NodeId{"node1", "node2", "node3", "node4"}
	codec := ibft.NewCodec()

	// Only create transports for non-leader nodes
	activeValidators := validators[1:] // node2, node3, node4
	transports := make([]*bfttcp.TCPTransport, len(activeValidators))
	for i, id := range activeValidators {
		tr, err := bfttcp.NewTCPTransport(id, "127.0.0.1:0", codec, nil)
		if err != nil {
			t.Fatalf("failed to create transport for %s: %v", id, err)
		}
		transports[i] = tr
	}

	addrs := make(map[core.NodeId]string)
	for i, id := range activeValidators {
		addrs[id] = transports[i].Addr()
	}

	for i, id := range activeValidators {
		peers := make(map[core.NodeId]string)
		for pid, paddr := range addrs {
			if pid != id {
				peers[pid] = paddr
			}
		}
		transports[i].Connect(peers)
	}
	defer func() {
		for _, tr := range transports {
			tr.Close()
		}
	}()

	config := ibft.NewConfig(validators, func(r core.Round) time.Duration {
		return 500 * time.Millisecond
	})

	value := ibft.NewValue([]byte("round-change-tcp"))
	nodes := make([]*ibft.Ibft, len(activeValidators))
	for i, v := range activeValidators {
		nodes[i] = ibft.NewIbft(v, config, transports[i], ibft.NewStore(), nil)
	}

	ctx := context.Background()
	for _, node := range nodes {
		go node.Start(ctx, "instance-1", value)
	}

	for i, node := range nodes {
		select {
		case <-node.Done():
		case <-time.After(15 * time.Second):
			t.Fatalf("node %d timed out waiting for consensus after round change", i)
		}
	}

	for _, node := range nodes {
		node.Stop()
	}
}
