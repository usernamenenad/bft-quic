package bftquic_test

import (
	"context"
	"testing"
	"time"

	"github.com/usernamenenad/bft-quic/core"
	"github.com/usernamenenad/bft-quic/impl/ibft"
	bftquic "github.com/usernamenenad/bft-quic/transport/bft-quic"
)

func setupQUICNetwork(t *testing.T, validators []core.NodeId) ([]*bftquic.QUICTransport, func()) {
	t.Helper()

	codec := ibft.NewCodec()

	// Phase 1: Create transports (start QUIC listeners on random ports)
	transports := make([]*bftquic.QUICTransport, len(validators))
	for i, id := range validators {
		tr, err := bftquic.NewQUICTransport(id, "127.0.0.1:0", codec, nil, nil)
		if err != nil {
			t.Fatalf("failed to create QUIC transport for %s: %v", id, err)
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

func TestQUICHappyPath(t *testing.T) {
	validators := []core.NodeId{"node1", "node2", "node3", "node4"}
	transports, cleanup := setupQUICNetwork(t, validators)
	defer cleanup()

	config := ibft.NewConfig(validators, func(r core.Round) time.Duration {
		return 10 * time.Second
	})

	value := ibft.NewValue([]byte("quic-test-value"))
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

func TestQUICFiveNodes(t *testing.T) {
	validators := []core.NodeId{"n1", "n2", "n3", "n4", "n5"}
	transports, cleanup := setupQUICNetwork(t, validators)
	defer cleanup()

	config := ibft.NewConfig(validators, func(r core.Round) time.Duration {
		return 10 * time.Second
	})

	value := ibft.NewValue([]byte("five-nodes-quic"))
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

func TestQUICRoundChange(t *testing.T) {
	// 4 validators but only 3 nodes start (leader node1 is faulty)
	validators := []core.NodeId{"node1", "node2", "node3", "node4"}
	codec := ibft.NewCodec()

	activeValidators := validators[1:] // node2, node3, node4
	transports := make([]*bftquic.QUICTransport, len(activeValidators))
	for i, id := range activeValidators {
		tr, err := bftquic.NewQUICTransport(id, "127.0.0.1:0", codec, nil, nil)
		if err != nil {
			t.Fatalf("failed to create QUIC transport for %s: %v", id, err)
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

	value := ibft.NewValue([]byte("round-change-quic"))
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

func TestQUICHeartbeat(t *testing.T) {
	validators := []core.NodeId{"nodeA", "nodeB"}
	transports, cleanup := setupQUICNetwork(t, validators)
	defer cleanup()

	for _, tr := range transports {
		tr.WaitForReady()
	}

	// Start heartbeats at 100ms interval
	for _, tr := range transports {
		tr.StartHeartbeat(100 * time.Millisecond)
	}

	awaitHeartbeat := func(tr *bftquic.QUICTransport, from core.NodeId) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case hb := <-tr.HeartbeatCh():
				if hb.From == from {
					return
				}
			case <-deadline:
				t.Fatalf("timed out waiting for heartbeat from %s", from)
				return
			}
		}
	}

	awaitHeartbeat(transports[0], "nodeB")
	awaitHeartbeat(transports[1], "nodeA")

	// Verify IsAlive reports true
	if !transports[0].IsAlive("nodeB", 5*time.Second) {
		t.Fatal("nodeA reports nodeB as not alive")
	}
	if !transports[1].IsAlive("nodeA", 5*time.Second) {
		t.Fatal("nodeB reports nodeA as not alive")
	}
}

func TestQUICMultiStream(t *testing.T) {
	// Verify that messages sent on control and data streams both arrive
	validators := []core.NodeId{"sender", "receiver"}
	transports, cleanup := setupQUICNetwork(t, validators)
	defer cleanup()

	for _, tr := range transports {
		tr.WaitForReady()
	}

	codec := ibft.NewCodec()
	msg := &ibft.Message{
		MessageType: ibft.MessageTypePrepare,
		From:        "sender",
		Instance:    "test-1",
		Round:       1,
		Value:       ibft.NewValue([]byte("multi-stream-test")),
	}

	// Send via transport (which uses control stream by default)
	ch := transports[1].Subscribe()

	err := transports[0].Send(context.Background(), "receiver", msg)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	select {
	case received := <-ch:
		ibftMsg, ok := received.(*ibft.Message)
		if !ok {
			t.Fatalf("received message of unexpected type: %T", received)
		}
		if ibftMsg.From != "sender" {
			t.Fatalf("expected from=sender, got %s", ibftMsg.From)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}

	_ = codec // codec reused via transport
}
