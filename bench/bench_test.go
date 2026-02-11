// Package bench provides reproducible benchmarks comparing TCP vs QUIC
// as the transport layer for the IBFT consensus algorithm.
//
// Benchmarks:
//   1. Consensus latency      – time for all honest nodes to decide (4-node, 7-node)
//   2. Round-change latency   – time to recover when the leader is absent
//   3. Connection setup time  – time to establish full-mesh connectivity
//   4. Message throughput      – sustained broadcast messages/sec
//   5. Single-message latency – point-to-point Send→Subscribe RTT
//   6. Payload size scaling   – throughput vs message size (128B → 64KB)
//   7. HOL-blocking resistance – control-plane latency under data-plane load

package bench

import (
	"context"
	"fmt"
	"log/slog"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/usernamenenad/bft-quic/core"
	"github.com/usernamenenad/bft-quic/impl/ibft"
	bfttcp "github.com/usernamenenad/bft-quic/transport/bft-tcp"
	bftquic "github.com/usernamenenad/bft-quic/transport/bft-quic"
)

// ─── helpers ────────────────────────────────────────────────────────────────

var silentLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// transportFactory abstracts creation of a full-mesh transport network.
type transportFactory struct {
	name string
	// setup returns N transports wired together + cleanup.
	setup func(b *testing.B, ids []core.NodeId) ([]core.Transport, func())
}

func tcpFactory() transportFactory {
	return transportFactory{
		name: "TCP",
		setup: func(b *testing.B, ids []core.NodeId) ([]core.Transport, func()) {
			b.Helper()
			codec := ibft.NewCodec()
			trs := make([]*bfttcp.TCPTransport, len(ids))
			for i, id := range ids {
				tr, err := bfttcp.NewTCPTransport(id, "127.0.0.1:0", codec, silentLogger)
				if err != nil {
					b.Fatalf("tcp create %s: %v", id, err)
				}
				trs[i] = tr
			}
			addrs := make(map[core.NodeId]string, len(ids))
			for i, id := range ids {
				addrs[id] = trs[i].Addr()
			}
			for i, id := range ids {
				peers := make(map[core.NodeId]string)
				for pid, pa := range addrs {
					if pid != id {
						peers[pid] = pa
					}
				}
				trs[i].Connect(peers)
			}
			out := make([]core.Transport, len(trs))
			for i := range trs {
				out[i] = trs[i]
			}
			return out, func() {
				for _, tr := range trs {
					tr.Close()
				}
			}
		},
	}
}

func quicFactory() transportFactory {
	return transportFactory{
		name: "QUIC",
		setup: func(b *testing.B, ids []core.NodeId) ([]core.Transport, func()) {
			b.Helper()
			codec := ibft.NewCodec()
			trs := make([]*bftquic.QUICTransport, len(ids))
			for i, id := range ids {
				tr, err := bftquic.NewQUICTransport(id, "127.0.0.1:0", codec, nil, silentLogger)
				if err != nil {
					b.Fatalf("quic create %s: %v", id, err)
				}
				trs[i] = tr
			}
			addrs := make(map[core.NodeId]string, len(ids))
			for i, id := range ids {
				addrs[id] = trs[i].Addr()
			}
			for i, id := range ids {
				peers := make(map[core.NodeId]string)
				for pid, pa := range addrs {
					if pid != id {
						peers[pid] = pa
					}
				}
				trs[i].Connect(peers)
			}
			out := make([]core.Transport, len(trs))
			for i := range trs {
				out[i] = trs[i]
			}
			return out, func() {
				for _, tr := range trs {
					tr.Close()
				}
			}
		},
	}
}

func makeValidators(n int) []core.NodeId {
	ids := make([]core.NodeId, n)
	for i := range ids {
		ids[i] = core.NodeId(fmt.Sprintf("node%d", i+1))
	}
	return ids
}

// ─── 1. Consensus Latency ──────────────────────────────────────────────────
// Measures wall-clock time for all N nodes to reach consensus via IBFT.
// Each iteration creates fresh transports + IBFT instances.

func benchConsensus(b *testing.B, factory transportFactory, n int) {
	validators := makeValidators(n)
	for i := 0; i < b.N; i++ {
		transports, cleanup := factory.setup(b, validators)
		config := ibft.NewConfig(validators, func(r core.Round) time.Duration { return 30 * time.Second })
		value := ibft.NewValue([]byte("bench-consensus"))

		nodes := make([]*ibft.Ibft, n)
		for j, v := range validators {
			nodes[j] = ibft.NewIbft(v, config, transports[j], ibft.NewStore(), silentLogger)
		}

		ctx := context.Background()
		b.StartTimer()
		for _, nd := range nodes {
			go nd.Start(ctx, core.Instance(fmt.Sprintf("i-%d", i)), value)
		}
		for _, nd := range nodes {
			<-nd.Done()
		}
		b.StopTimer()

		for _, nd := range nodes {
			nd.Stop()
		}
		cleanup()
	}
}

func BenchmarkConsensus_TCP_4Nodes(b *testing.B)  { b.StopTimer(); benchConsensus(b, tcpFactory(), 4) }
func BenchmarkConsensus_QUIC_4Nodes(b *testing.B) { b.StopTimer(); benchConsensus(b, quicFactory(), 4) }
func BenchmarkConsensus_TCP_7Nodes(b *testing.B)  { b.StopTimer(); benchConsensus(b, tcpFactory(), 7) }
func BenchmarkConsensus_QUIC_7Nodes(b *testing.B) { b.StopTimer(); benchConsensus(b, quicFactory(), 7) }

// ─── 2. Round-Change Latency ───────────────────────────────────────────────
// Leader (node1) never starts → remaining 3f nodes must round-change and decide.

func benchRoundChange(b *testing.B, factory transportFactory) {
	validators := makeValidators(4)
	activeIDs := validators[1:] // node2, node3, node4 — leader absent
	for i := 0; i < b.N; i++ {
		transports, cleanup := factory.setup(b, activeIDs)
		config := ibft.NewConfig(validators, func(r core.Round) time.Duration { return 200 * time.Millisecond })
		value := ibft.NewValue([]byte("round-change"))

		nodes := make([]*ibft.Ibft, len(activeIDs))
		for j, v := range activeIDs {
			nodes[j] = ibft.NewIbft(v, config, transports[j], ibft.NewStore(), silentLogger)
		}

		ctx := context.Background()
		b.StartTimer()
		for _, nd := range nodes {
			go nd.Start(ctx, core.Instance(fmt.Sprintf("rc-%d", i)), value)
		}
		for _, nd := range nodes {
			<-nd.Done()
		}
		b.StopTimer()

		for _, nd := range nodes {
			nd.Stop()
		}
		cleanup()
	}
}

func BenchmarkRoundChange_TCP(b *testing.B)  { b.StopTimer(); benchRoundChange(b, tcpFactory()) }
func BenchmarkRoundChange_QUIC(b *testing.B) { b.StopTimer(); benchRoundChange(b, quicFactory()) }

// ─── 3. Connection Setup Time ──────────────────────────────────────────────
// Time to create transports + establish full mesh + WaitForReady.

func benchConnSetup(b *testing.B, factory transportFactory, n int) {
	ids := makeValidators(n)
	for i := 0; i < b.N; i++ {
		b.StartTimer()
		transports, cleanup := factory.setup(b, ids)
		for _, tr := range transports {
			tr.WaitForReady()
		}
		b.StopTimer()
		cleanup()
	}
}

func BenchmarkConnSetup_TCP_4(b *testing.B)  { b.StopTimer(); benchConnSetup(b, tcpFactory(), 4) }
func BenchmarkConnSetup_QUIC_4(b *testing.B) { b.StopTimer(); benchConnSetup(b, quicFactory(), 4) }
func BenchmarkConnSetup_TCP_7(b *testing.B)  { b.StopTimer(); benchConnSetup(b, tcpFactory(), 7) }
func BenchmarkConnSetup_QUIC_7(b *testing.B) { b.StopTimer(); benchConnSetup(b, quicFactory(), 7) }

// ─── 4. Message Throughput ─────────────────────────────────────────────────
// Sustained Broadcast of IBFT PREPARE messages; measures ops/sec.

func benchThroughput(b *testing.B, factory transportFactory) {
	ids := makeValidators(4)
	transports, cleanup := factory.setup(b, ids)
	defer cleanup()
	for _, tr := range transports {
		tr.WaitForReady()
	}

	// drain all subscribers
	for _, tr := range transports {
		ch := tr.Subscribe()
		go func(c <-chan core.Message) {
			for range c {
			}
		}(ch)
	}

	msg := &ibft.Message{
		MessageType: ibft.MessageTypePrepare,
		From:        ids[0],
		Instance:    "throughput",
		Round:       1,
		Value:       ibft.NewValue([]byte("bench")),
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		transports[0].Broadcast(ctx, msg)
	}
}

func BenchmarkThroughput_TCP(b *testing.B)  { benchThroughput(b, tcpFactory()) }
func BenchmarkThroughput_QUIC(b *testing.B) { benchThroughput(b, quicFactory()) }

// ─── 5. Single-Message Latency ─────────────────────────────────────────────
// Point-to-point: Send from node0→node1, measure until received.

func benchMsgLatency(b *testing.B, factory transportFactory) {
	ids := makeValidators(2)
	transports, cleanup := factory.setup(b, ids)
	defer cleanup()
	for _, tr := range transports {
		tr.WaitForReady()
	}

	ch := transports[1].Subscribe()
	msg := &ibft.Message{
		MessageType: ibft.MessageTypePrepare,
		From:        ids[0],
		Instance:    "latency",
		Round:       1,
		Value:       ibft.NewValue([]byte("ping")),
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		transports[0].Send(ctx, ids[1], msg)
		<-ch
	}
}

func BenchmarkMsgLatency_TCP(b *testing.B)  { benchMsgLatency(b, tcpFactory()) }
func BenchmarkMsgLatency_QUIC(b *testing.B) { benchMsgLatency(b, quicFactory()) }

// ─── 6. Payload Size Scaling ───────────────────────────────────────────────
// Broadcast messages of increasing size; measures throughput in bytes/sec.

func benchPayloadSize(b *testing.B, factory transportFactory, size int) {
	ids := makeValidators(4)
	transports, cleanup := factory.setup(b, ids)
	defer cleanup()
	for _, tr := range transports {
		tr.WaitForReady()
	}

	for _, tr := range transports {
		ch := tr.Subscribe()
		go func(c <-chan core.Message) {
			for range c {
			}
		}(ch)
	}

	payload := make([]byte, size)
	for j := range payload {
		payload[j] = byte(j % 256)
	}
	msg := &ibft.Message{
		MessageType: ibft.MessageTypePrepare,
		From:        ids[0],
		Instance:    "payload",
		Round:       1,
		Value:       ibft.NewValue(payload),
	}

	ctx := context.Background()
	b.ResetTimer()
	b.SetBytes(int64(size))
	for i := 0; i < b.N; i++ {
		transports[0].Broadcast(ctx, msg)
	}
}

func BenchmarkPayload128B_TCP(b *testing.B)   { benchPayloadSize(b, tcpFactory(), 128) }
func BenchmarkPayload128B_QUIC(b *testing.B)  { benchPayloadSize(b, quicFactory(), 128) }
func BenchmarkPayload1KB_TCP(b *testing.B)    { benchPayloadSize(b, tcpFactory(), 1024) }
func BenchmarkPayload1KB_QUIC(b *testing.B)   { benchPayloadSize(b, quicFactory(), 1024) }
func BenchmarkPayload16KB_TCP(b *testing.B)   { benchPayloadSize(b, tcpFactory(), 16*1024) }
func BenchmarkPayload16KB_QUIC(b *testing.B)  { benchPayloadSize(b, quicFactory(), 16*1024) }
func BenchmarkPayload64KB_TCP(b *testing.B)   { benchPayloadSize(b, tcpFactory(), 64*1024) }
func BenchmarkPayload64KB_QUIC(b *testing.B)  { benchPayloadSize(b, quicFactory(), 64*1024) }

// ─── 7. HOL-Blocking Resistance ────────────────────────────────────────────
// Simulates data-plane load (large messages) while measuring control-plane
// latency. For QUIC, control and data go on separate streams; for TCP,
// everything shares the same connection and write mutex.
// Lower control-plane latency under data load = better HOL resistance.

// instanceClassifier routes messages based on Instance field.
// "data-*" → data stream, everything else → control stream.
type instanceClassifier struct{}

func (c *instanceClassifier) StreamType(msg core.Message) byte {
	if m, ok := msg.(*ibft.Message); ok && m.Instance == "data" {
		return bftquic.StreamTypeData
	}
	return bftquic.StreamTypeControl
}

func quicFactoryWithClassifier() transportFactory {
	return transportFactory{
		name: "QUIC-multistream",
		setup: func(b *testing.B, ids []core.NodeId) ([]core.Transport, func()) {
			b.Helper()
			codec := ibft.NewCodec()
			cls := &instanceClassifier{}
			trs := make([]*bftquic.QUICTransport, len(ids))
			for i, id := range ids {
				tr, err := bftquic.NewQUICTransport(id, "127.0.0.1:0", codec, cls, silentLogger)
				if err != nil {
					b.Fatalf("quic create %s: %v", id, err)
				}
				trs[i] = tr
			}
			addrs := make(map[core.NodeId]string, len(ids))
			for i, id := range ids {
				addrs[id] = trs[i].Addr()
			}
			for i, id := range ids {
				peers := make(map[core.NodeId]string)
				for pid, pa := range addrs {
					if pid != id {
						peers[pid] = pa
					}
				}
				trs[i].Connect(peers)
			}
			out := make([]core.Transport, len(trs))
			for i := range trs {
				out[i] = trs[i]
			}
			return out, func() {
				for _, tr := range trs {
					tr.Close()
				}
			}
		},
	}
}

func benchHOL(b *testing.B, factory transportFactory) {
	ids := makeValidators(2)
	transports, cleanup := factory.setup(b, ids)
	defer cleanup()
	for _, tr := range transports {
		tr.WaitForReady()
	}

	// Drain sender's self-delivered messages to prevent Broadcast blocking
	senderCh := transports[0].Subscribe()
	go func() { for range senderCh {} }()

	ch := transports[1].Subscribe()
	ctx := context.Background()

	controlMsg := &ibft.Message{
		MessageType: ibft.MessageTypePrepare,
		From:        ids[0],
		Instance:    "control",
		Round:       1,
		Value:       ibft.NewValue([]byte("ctrl")),
	}

	bigPayload := make([]byte, 64*1024)
	for j := range bigPayload {
		bigPayload[j] = 0xAB
	}
	dataMsg := &ibft.Message{
		MessageType: ibft.MessageTypePrepare,
		From:        ids[0],
		Instance:    "data",
		Round:       1,
		Value:       ibft.NewValue(bigPayload),
	}

	const dataFlood = 50

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Fire data flood in background (non-blocking)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < dataFlood; j++ {
				transports[0].Broadcast(ctx, dataMsg)
			}
		}()

		// Let data start flowing, then measure control path
		time.Sleep(100 * time.Microsecond)
		start := time.Now()
		transports[0].Send(ctx, ids[1], controlMsg)

		// Count received messages: we expect dataFlood data + 1 control at transport[1]
		dataReceived := 0
		controlReceived := false
		var elapsed time.Duration
		for dataReceived < dataFlood || !controlReceived {
			select {
			case m := <-ch:
				if msg, ok := m.(*ibft.Message); ok {
					if msg.Instance == "control" && !controlReceived {
						elapsed = time.Since(start)
						controlReceived = true
					} else if msg.Instance == "data" {
						dataReceived++
					}
				}
			case <-time.After(10 * time.Second):
				b.Fatalf("HOL iter %d: timed out (data=%d, ctrl=%v)", i, dataReceived, controlReceived)
			}
		}
		b.ReportMetric(float64(elapsed.Nanoseconds()), "ns/control-msg")
		wg.Wait()
	}
}

func BenchmarkHOLBlocking_TCP(b *testing.B)  { benchHOL(b, tcpFactory()) }
func BenchmarkHOLBlocking_QUIC(b *testing.B) { benchHOL(b, quicFactoryWithClassifier()) }
