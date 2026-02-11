// Network simulation benchmarks using tc netem.
//
// These benchmarks apply realistic network conditions (latency, packet loss,
// jitter, bandwidth limits) to the loopback interface and measure TCP vs QUIC
// performance under each scenario.
//
// Prerequisites:
//   - Linux with tc (iproute2) and sch_netem kernel module
//   - sudo access for tc commands (passwordless recommended)
//
// Run:
//   go test -run=^$ -bench='BenchmarkNetem' -benchtime=5x -count=5 -timeout=600s -tags netem

package bench

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/usernamenenad/bft-quic/core"
	"github.com/usernamenenad/bft-quic/impl/ibft"
)

// ─── tc netem helpers ──────────────────────────────────────────────────────

// netemRule describes a tc netem configuration.
type netemRule struct {
	name string
	args []string // args after "sudo tc qdisc add dev lo root netem"
}

func applyNetem(b *testing.B, rule netemRule) {
	b.Helper()
	args := append([]string{"tc", "qdisc", "add", "dev", "lo", "root", "netem"}, rule.args...)
	out, err := exec.Command("sudo", args...).CombinedOutput()
	if err != nil {
		b.Fatalf("tc add failed: %v: %s", err, out)
	}
}

func clearNetem(b *testing.B) {
	b.Helper()
	exec.Command("sudo", "tc", "qdisc", "del", "dev", "lo", "root").CombinedOutput()
}

// withNetem applies a netem rule for the duration of fn, then clears it.
func withNetem(b *testing.B, rule netemRule, fn func()) {
	b.Helper()
	clearNetem(b) // ensure clean state
	applyNetem(b, rule)
	defer clearNetem(b)
	fn()
}

// ─── scenarios ─────────────────────────────────────────────────────────────

var netemScenarios = []netemRule{
	{name: "10ms", args: []string{"delay", "10ms"}},
	{name: "50ms", args: []string{"delay", "50ms"}},
	{name: "100ms", args: []string{"delay", "100ms", "10ms"}}, // 100ms ± 10ms jitter
	{name: "1pct_loss", args: []string{"delay", "10ms", "loss", "1%"}},
	{name: "5pct_loss", args: []string{"delay", "10ms", "loss", "5%"}},
	{name: "10pct_loss", args: []string{"delay", "10ms", "loss", "10%"}},
	{name: "jitter_30ms", args: []string{"delay", "20ms", "30ms", "25%"}}, // 20ms ± 30ms, 25% correlation
	{name: "bw_10mbit", args: []string{"delay", "10ms", "rate", "10mbit"}},
	{name: "bw_1mbit", args: []string{"delay", "10ms", "rate", "1mbit"}},
	// Real-world combined: WAN with moderate loss + jitter
	{name: "wan_realistic", args: []string{"delay", "30ms", "10ms", "25%", "loss", "1%", "rate", "10mbit"}},
	// Harsh conditions: high latency + significant loss
	{name: "harsh", args: []string{"delay", "50ms", "20ms", "25%", "loss", "5%", "rate", "5mbit"}},
}

// ─── 8. Consensus Under Latency ────────────────────────────────────────────
// Measures how added network latency affects consensus time.
// With real RTT, QUIC's connection setup cost is amortized differently —
// TCP needs 1.5 RTT for handshake, QUIC needs 1 RTT (with 0-RTT potential).

func benchConsensusNetem(b *testing.B, factory transportFactory, n int, rule netemRule) {
	validators := makeValidators(n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		transports, cleanup := factory.setup(b, validators)
		config := ibft.NewConfig(validators, func(r core.Round) time.Duration { return 30 * time.Second })
		value := ibft.NewValue([]byte("netem-consensus"))
		nodes := make([]*ibft.Ibft, n)
		for j, v := range validators {
			nodes[j] = ibft.NewIbft(v, config, transports[j], ibft.NewStore(), silentLogger)
		}
		// Wait for mesh to be ready before applying netem
		for _, tr := range transports {
			tr.WaitForReady()
		}
		ctx := context.Background()
		b.StartTimer()
		for _, nd := range nodes {
			go nd.Start(ctx, core.Instance(fmt.Sprintf("nc-%d", i)), value)
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

func BenchmarkNetem_Consensus_TCP_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchConsensusNetem(b, tcpFactory(), 4, netemScenarios[0]) })
}
func BenchmarkNetem_Consensus_QUIC_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchConsensusNetem(b, quicFactory(), 4, netemScenarios[0]) })
}
func BenchmarkNetem_Consensus_TCP_50ms(b *testing.B) {
	withNetem(b, netemScenarios[1], func() { benchConsensusNetem(b, tcpFactory(), 4, netemScenarios[1]) })
}
func BenchmarkNetem_Consensus_QUIC_50ms(b *testing.B) {
	withNetem(b, netemScenarios[1], func() { benchConsensusNetem(b, quicFactory(), 4, netemScenarios[1]) })
}
func BenchmarkNetem_Consensus_TCP_100ms(b *testing.B) {
	withNetem(b, netemScenarios[2], func() { benchConsensusNetem(b, tcpFactory(), 4, netemScenarios[2]) })
}
func BenchmarkNetem_Consensus_QUIC_100ms(b *testing.B) {
	withNetem(b, netemScenarios[2], func() { benchConsensusNetem(b, quicFactory(), 4, netemScenarios[2]) })
}

// ─── 9. Consensus Under Packet Loss ────────────────────────────────────────
// This is where QUIC should clearly win: TCP retransmits at connection level
// (blocking ALL data), while QUIC retransmits per-stream.

func BenchmarkNetem_Consensus_TCP_1pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[3], func() { benchConsensusNetem(b, tcpFactory(), 4, netemScenarios[3]) })
}
func BenchmarkNetem_Consensus_QUIC_1pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[3], func() { benchConsensusNetem(b, quicFactory(), 4, netemScenarios[3]) })
}
func BenchmarkNetem_Consensus_TCP_5pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[4], func() { benchConsensusNetem(b, tcpFactory(), 4, netemScenarios[4]) })
}
func BenchmarkNetem_Consensus_QUIC_5pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[4], func() { benchConsensusNetem(b, quicFactory(), 4, netemScenarios[4]) })
}
func BenchmarkNetem_Consensus_TCP_10pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[5], func() { benchConsensusNetem(b, tcpFactory(), 4, netemScenarios[5]) })
}
func BenchmarkNetem_Consensus_QUIC_10pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[5], func() { benchConsensusNetem(b, quicFactory(), 4, netemScenarios[5]) })
}

// ─── 10. Throughput Under Latency+Loss ─────────────────────────────────────
// Sustained broadcast rate under various conditions.

func benchThroughputNetem(b *testing.B, factory transportFactory, rule netemRule) {
	ids := makeValidators(4)
	transports, cleanup := factory.setup(b, ids)
	defer cleanup()
	for _, tr := range transports {
		tr.WaitForReady()
	}
	for _, tr := range transports {
		ch := tr.Subscribe()
		go func(c <-chan core.Message) { for range c {} }(ch)
	}
	msg := &ibft.Message{
		MessageType: ibft.MessageTypePrepare,
		From:        ids[0],
		Instance:    "throughput-netem",
		Round:       1,
		Value:       ibft.NewValue([]byte("bench")),
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		transports[0].Broadcast(ctx, msg)
	}
}

func BenchmarkNetem_Throughput_TCP_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchThroughputNetem(b, tcpFactory(), netemScenarios[0]) })
}
func BenchmarkNetem_Throughput_QUIC_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchThroughputNetem(b, quicFactory(), netemScenarios[0]) })
}
func BenchmarkNetem_Throughput_TCP_1pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[3], func() { benchThroughputNetem(b, tcpFactory(), netemScenarios[3]) })
}
func BenchmarkNetem_Throughput_QUIC_1pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[3], func() { benchThroughputNetem(b, quicFactory(), netemScenarios[3]) })
}
func BenchmarkNetem_Throughput_TCP_5pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[4], func() { benchThroughputNetem(b, tcpFactory(), netemScenarios[4]) })
}
func BenchmarkNetem_Throughput_QUIC_5pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[4], func() { benchThroughputNetem(b, quicFactory(), netemScenarios[4]) })
}

// ─── 11. HOL Blocking Under Real Latency+Loss ─────────────────────────────
// The definitive test: with packet loss, TCP's connection-level retransmission
// blocks ALL data on the connection, while QUIC retransmits only the affected
// stream. Control messages on a separate QUIC stream are unaffected.

func benchHOLNetem(b *testing.B, factory transportFactory, rule netemRule) {
	ids := makeValidators(2)
	transports, cleanup := factory.setup(b, ids)
	defer cleanup()
	for _, tr := range transports {
		tr.WaitForReady()
	}

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
	const dataFlood = 20 // fewer than localhost bench due to slower network

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < dataFlood; j++ {
				transports[0].Broadcast(ctx, dataMsg)
			}
		}()

		time.Sleep(500 * time.Microsecond)
		start := time.Now()
		transports[0].Send(ctx, ids[1], controlMsg)

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
			case <-time.After(30 * time.Second):
				b.Fatalf("HOL netem iter %d: timed out (data=%d/%d, ctrl=%v)", i, dataReceived, dataFlood, controlReceived)
			}
		}
		b.ReportMetric(float64(elapsed.Nanoseconds()), "ns/control-msg")
		wg.Wait()
	}
}

func BenchmarkNetem_HOL_TCP_1pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[3], func() { benchHOLNetem(b, tcpFactory(), netemScenarios[3]) })
}
func BenchmarkNetem_HOL_QUIC_1pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[3], func() { benchHOLNetem(b, quicFactoryWithClassifier(), netemScenarios[3]) })
}
func BenchmarkNetem_HOL_TCP_5pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[4], func() { benchHOLNetem(b, tcpFactory(), netemScenarios[4]) })
}
func BenchmarkNetem_HOL_QUIC_5pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[4], func() { benchHOLNetem(b, quicFactoryWithClassifier(), netemScenarios[4]) })
}

// ─── 12. Message Latency Under Network Conditions ──────────────────────────
// Point-to-point latency under different network conditions.

func benchMsgLatencyNetem(b *testing.B, factory transportFactory) {
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
		Instance:    "latency-netem",
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

func BenchmarkNetem_Latency_TCP_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchMsgLatencyNetem(b, tcpFactory()) })
}
func BenchmarkNetem_Latency_QUIC_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchMsgLatencyNetem(b, quicFactory()) })
}
func BenchmarkNetem_Latency_TCP_50ms(b *testing.B) {
	withNetem(b, netemScenarios[1], func() { benchMsgLatencyNetem(b, tcpFactory()) })
}
func BenchmarkNetem_Latency_QUIC_50ms(b *testing.B) {
	withNetem(b, netemScenarios[1], func() { benchMsgLatencyNetem(b, quicFactory()) })
}
func BenchmarkNetem_Latency_TCP_jitter30ms(b *testing.B) {
	withNetem(b, netemScenarios[6], func() { benchMsgLatencyNetem(b, tcpFactory()) })
}
func BenchmarkNetem_Latency_QUIC_jitter30ms(b *testing.B) {
	withNetem(b, netemScenarios[6], func() { benchMsgLatencyNetem(b, quicFactory()) })
}

// ─── 13. Payload Under Bandwidth Limits ────────────────────────────────────
// Large payload transfer under bandwidth constraints.

func benchPayloadNetem(b *testing.B, factory transportFactory, size int) {
	ids := makeValidators(4)
	transports, cleanup := factory.setup(b, ids)
	defer cleanup()
	for _, tr := range transports {
		tr.WaitForReady()
	}
	for _, tr := range transports {
		ch := tr.Subscribe()
		go func(c <-chan core.Message) { for range c {} }(ch)
	}
	payload := make([]byte, size)
	for j := range payload {
		payload[j] = byte(j % 256)
	}
	msg := &ibft.Message{
		MessageType: ibft.MessageTypePrepare,
		From:        ids[0],
		Instance:    "payload-netem",
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

func BenchmarkNetem_Payload64K_TCP_10mbit(b *testing.B) {
	withNetem(b, netemScenarios[7], func() { benchPayloadNetem(b, tcpFactory(), 64*1024) })
}
func BenchmarkNetem_Payload64K_QUIC_10mbit(b *testing.B) {
	withNetem(b, netemScenarios[7], func() { benchPayloadNetem(b, quicFactory(), 64*1024) })
}
func BenchmarkNetem_Payload64K_TCP_1mbit(b *testing.B) {
	withNetem(b, netemScenarios[8], func() { benchPayloadNetem(b, tcpFactory(), 64*1024) })
}
func BenchmarkNetem_Payload64K_QUIC_1mbit(b *testing.B) {
	withNetem(b, netemScenarios[8], func() { benchPayloadNetem(b, quicFactory(), 64*1024) })
}

// ─── 14. Realistic WAN Scenario ────────────────────────────────────────────
// Combined latency + loss + bandwidth limit simulating a real WAN deployment.

func BenchmarkNetem_Consensus_TCP_WAN(b *testing.B) {
	withNetem(b, netemScenarios[9], func() { benchConsensusNetem(b, tcpFactory(), 4, netemScenarios[9]) })
}
func BenchmarkNetem_Consensus_QUIC_WAN(b *testing.B) {
	withNetem(b, netemScenarios[9], func() { benchConsensusNetem(b, quicFactory(), 4, netemScenarios[9]) })
}
func BenchmarkNetem_Consensus_TCP_Harsh(b *testing.B) {
	withNetem(b, netemScenarios[10], func() { benchConsensusNetem(b, tcpFactory(), 4, netemScenarios[10]) })
}
func BenchmarkNetem_Consensus_QUIC_Harsh(b *testing.B) {
	withNetem(b, netemScenarios[10], func() { benchConsensusNetem(b, quicFactory(), 4, netemScenarios[10]) })
}

func BenchmarkNetem_HOL_TCP_WAN(b *testing.B) {
	withNetem(b, netemScenarios[9], func() { benchHOLNetem(b, tcpFactory(), netemScenarios[9]) })
}
func BenchmarkNetem_HOL_QUIC_WAN(b *testing.B) {
	withNetem(b, netemScenarios[9], func() { benchHOLNetem(b, quicFactoryWithClassifier(), netemScenarios[9]) })
}
func BenchmarkNetem_HOL_TCP_Harsh(b *testing.B) {
	withNetem(b, netemScenarios[10], func() { benchHOLNetem(b, tcpFactory(), netemScenarios[10]) })
}
func BenchmarkNetem_HOL_QUIC_Harsh(b *testing.B) {
	withNetem(b, netemScenarios[10], func() { benchHOLNetem(b, quicFactoryWithClassifier(), netemScenarios[10]) })
}

// ─── 15. Concurrent Message Streams ────────────────────────────────────────
// Multiple goroutines simultaneously broadcast messages on the same transport.
// QUIC's per-stream ordering (vs TCP's single-connection mutex) should show
// better parallelism under network conditions.

func benchConcurrentStreams(b *testing.B, factory transportFactory, senders int) {
	ids := makeValidators(4)
	transports, cleanup := factory.setup(b, ids)
	defer cleanup()
	for _, tr := range transports {
		tr.WaitForReady()
	}
	for _, tr := range transports {
		ch := tr.Subscribe()
		go func(c <-chan core.Message) { for range c {} }(ch)
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for s := 0; s < senders; s++ {
			wg.Add(1)
			go func(senderIdx int) {
				defer wg.Done()
				msg := &ibft.Message{
					MessageType: ibft.MessageTypePrepare,
					From:        ids[senderIdx%len(ids)],
					Instance:    core.Instance(fmt.Sprintf("stream-%d", senderIdx)),
					Round:       1,
					Value:       ibft.NewValue([]byte("concurrent")),
				}
				for j := 0; j < 50; j++ {
					transports[senderIdx%len(ids)].Broadcast(ctx, msg)
				}
			}(s)
		}
		wg.Wait()
	}
}

func BenchmarkNetem_ConcStreams4_TCP_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchConcurrentStreams(b, tcpFactory(), 4) })
}
func BenchmarkNetem_ConcStreams4_QUIC_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchConcurrentStreams(b, quicFactory(), 4) })
}
func BenchmarkNetem_ConcStreams8_TCP_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchConcurrentStreams(b, tcpFactory(), 8) })
}
func BenchmarkNetem_ConcStreams8_QUIC_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchConcurrentStreams(b, quicFactory(), 8) })
}
func BenchmarkNetem_ConcStreams4_TCP_WAN(b *testing.B) {
	withNetem(b, netemScenarios[9], func() { benchConcurrentStreams(b, tcpFactory(), 4) })
}
func BenchmarkNetem_ConcStreams4_QUIC_WAN(b *testing.B) {
	withNetem(b, netemScenarios[9], func() { benchConcurrentStreams(b, quicFactory(), 4) })
}

// ─── 16. Recovery After Packet Burst Loss ──────────────────────────────────
// Simulates a burst of packet loss (via reorder + loss), measuring how quickly
// each transport recovers and delivers a post-burst control message.

func benchBurstRecovery(b *testing.B, factory transportFactory) {
	ids := makeValidators(2)
	transports, cleanup := factory.setup(b, ids)
	defer cleanup()
	for _, tr := range transports {
		tr.WaitForReady()
	}

	senderCh := transports[0].Subscribe()
	go func() { for range senderCh {} }()
	ch := transports[1].Subscribe()
	ctx := context.Background()

	// Pre-burst: send some messages to warm up
	warmup := &ibft.Message{
		MessageType: ibft.MessageTypePrepare,
		From:        ids[0],
		Instance:    "warmup",
		Round:       1,
		Value:       ibft.NewValue([]byte("warm")),
	}
	for w := 0; w < 10; w++ {
		transports[0].Send(ctx, ids[1], warmup)
		<-ch
	}

	// The netem rule will be applied externally before this function is called
	msg := &ibft.Message{
		MessageType: ibft.MessageTypePrepare,
		From:        ids[0],
		Instance:    "recovery",
		Round:       1,
		Value:       ibft.NewValue([]byte("post-burst")),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		transports[0].Send(ctx, ids[1], msg)
		select {
		case <-ch:
			b.ReportMetric(float64(time.Since(start).Nanoseconds()), "ns/recovery")
		case <-time.After(10 * time.Second):
			b.Fatalf("burst recovery timed out at iter %d", i)
		}
	}
}

// Burst loss scenario: high loss rate simulating congestion burst
var burstLoss = netemRule{name: "burst_loss", args: []string{"delay", "10ms", "loss", "25%", "25%"}} // 25% loss with 25% correlation (bursty)

func BenchmarkNetem_BurstRecovery_TCP(b *testing.B) {
	withNetem(b, burstLoss, func() { benchBurstRecovery(b, tcpFactory()) })
}
func BenchmarkNetem_BurstRecovery_QUIC(b *testing.B) {
	withNetem(b, burstLoss, func() { benchBurstRecovery(b, quicFactory()) })
}

// ─── 17. Sustained Consensus Rounds Under Network Degradation ──────────────
// Runs many consecutive consensus rounds on a pre-established mesh,
// measuring per-round time. This amortizes setup cost and shows steady-state.

func benchSteadyState(b *testing.B, factory transportFactory, rule netemRule) {
	validators := makeValidators(4)
	n := len(validators)
	transports, cleanup := factory.setup(b, validators)
	defer cleanup()
	for _, tr := range transports {
		tr.WaitForReady()
	}
	config := ibft.NewConfig(validators, func(r core.Round) time.Duration { return 30 * time.Second })

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value := ibft.NewValue([]byte(fmt.Sprintf("steady-%d", i)))
		inst := core.Instance(fmt.Sprintf("ss-%d", i))
		nodes := make([]*ibft.Ibft, n)
		for j, v := range validators {
			nodes[j] = ibft.NewIbft(v, config, transports[j], ibft.NewStore(), silentLogger)
		}
		for _, nd := range nodes {
			go nd.Start(context.Background(), inst, value)
		}
		for _, nd := range nodes {
			<-nd.Done()
		}
		for _, nd := range nodes {
			nd.Stop()
		}
	}
}

func BenchmarkNetem_SteadyState_TCP_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchSteadyState(b, tcpFactory(), netemScenarios[0]) })
}
func BenchmarkNetem_SteadyState_QUIC_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchSteadyState(b, quicFactory(), netemScenarios[0]) })
}
func BenchmarkNetem_SteadyState_TCP_50ms(b *testing.B) {
	withNetem(b, netemScenarios[1], func() { benchSteadyState(b, tcpFactory(), netemScenarios[1]) })
}
func BenchmarkNetem_SteadyState_QUIC_50ms(b *testing.B) {
	withNetem(b, netemScenarios[1], func() { benchSteadyState(b, quicFactory(), netemScenarios[1]) })
}
func BenchmarkNetem_SteadyState_TCP_WAN(b *testing.B) {
	withNetem(b, netemScenarios[9], func() { benchSteadyState(b, tcpFactory(), netemScenarios[9]) })
}
func BenchmarkNetem_SteadyState_QUIC_WAN(b *testing.B) {
	withNetem(b, netemScenarios[9], func() { benchSteadyState(b, quicFactory(), netemScenarios[9]) })
}
func BenchmarkNetem_SteadyState_TCP_Harsh(b *testing.B) {
	withNetem(b, netemScenarios[10], func() { benchSteadyState(b, tcpFactory(), netemScenarios[10]) })
}
func BenchmarkNetem_SteadyState_QUIC_Harsh(b *testing.B) {
	withNetem(b, netemScenarios[10], func() { benchSteadyState(b, quicFactory(), netemScenarios[10]) })
}

// ─── 18. 7-Node Consensus Under Network Conditions ─────────────────────────
// Larger validator set amplifies the difference: more connections = more
// chances for HOL blocking and loss-induced stalls.

func BenchmarkNetem_Consensus7N_TCP_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchConsensusNetem(b, tcpFactory(), 7, netemScenarios[0]) })
}
func BenchmarkNetem_Consensus7N_QUIC_10ms(b *testing.B) {
	withNetem(b, netemScenarios[0], func() { benchConsensusNetem(b, quicFactory(), 7, netemScenarios[0]) })
}
func BenchmarkNetem_Consensus7N_TCP_1pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[3], func() { benchConsensusNetem(b, tcpFactory(), 7, netemScenarios[3]) })
}
func BenchmarkNetem_Consensus7N_QUIC_1pctLoss(b *testing.B) {
	withNetem(b, netemScenarios[3], func() { benchConsensusNetem(b, quicFactory(), 7, netemScenarios[3]) })
}
func BenchmarkNetem_Consensus7N_TCP_WAN(b *testing.B) {
	withNetem(b, netemScenarios[9], func() { benchConsensusNetem(b, tcpFactory(), 7, netemScenarios[9]) })
}
func BenchmarkNetem_Consensus7N_QUIC_WAN(b *testing.B) {
	withNetem(b, netemScenarios[9], func() { benchConsensusNetem(b, quicFactory(), 7, netemScenarios[9]) })
}

// ─── Utility: verify netem is functional ───────────────────────────────────

func init() {
	// Quick sanity: make sure tc is available
	var out bytes.Buffer
	cmd := exec.Command("sudo", "tc", "qdisc", "show", "dev", "lo")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		panic(fmt.Sprintf("tc not available (needed for netem benchmarks): %v: %s", err, out.String()))
	}
}
