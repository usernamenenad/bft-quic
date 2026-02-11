// CSV benchmark data collector.
//
// Produces per-sample CSV data for TCP vs QUIC comparison across
// multiple network scenarios. Each measurement is an individual sample
// (not aggregated), suitable for statistical analysis and graphing.
//
// Run:
//   go test -v -run='TestCSV' -timeout=3600s ./bench/
//
// Output files are written to bench/results/*.csv

package bench

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/usernamenenad/bft-quic/core"
	"github.com/usernamenenad/bft-quic/impl/ibft"
	bfttcp "github.com/usernamenenad/bft-quic/transport/bft-tcp"
	bftquic "github.com/usernamenenad/bft-quic/transport/bft-quic"
)

// ─── transport factories (testing.T variants) ──────────────────────────────

type tFactory struct {
	name  string
	setup func(t *testing.T, ids []core.NodeId) ([]core.Transport, func())
}

func tcpTFactory() tFactory {
	return tFactory{
		name: "TCP",
		setup: func(t *testing.T, ids []core.NodeId) ([]core.Transport, func()) {
			t.Helper()
			codec := ibft.NewCodec()
			trs := make([]*bfttcp.TCPTransport, len(ids))
			for i, id := range ids {
				tr, err := bfttcp.NewTCPTransport(id, "127.0.0.1:0", codec, silentLogger)
				if err != nil {
					t.Fatalf("tcp: %v", err)
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

func quicTFactory() tFactory {
	return tFactory{
		name: "QUIC",
		setup: func(t *testing.T, ids []core.NodeId) ([]core.Transport, func()) {
			t.Helper()
			codec := ibft.NewCodec()
			trs := make([]*bftquic.QUICTransport, len(ids))
			for i, id := range ids {
				tr, err := bftquic.NewQUICTransport(id, "127.0.0.1:0", codec, nil, silentLogger)
				if err != nil {
					t.Fatalf("quic: %v", err)
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

func quicMultistreamTFactory() tFactory {
	return tFactory{
		name: "QUIC",
		setup: func(t *testing.T, ids []core.NodeId) ([]core.Transport, func()) {
			t.Helper()
			codec := ibft.NewCodec()
			cls := &instanceClassifier{}
			trs := make([]*bftquic.QUICTransport, len(ids))
			for i, id := range ids {
				tr, err := bftquic.NewQUICTransport(id, "127.0.0.1:0", codec, cls, silentLogger)
				if err != nil {
					t.Fatalf("quic: %v", err)
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

// ─── netem helpers (testing.T variants) ────────────────────────────────────

type scenario struct {
	name string
	args []string // tc netem args
}

var csvScenarios = []scenario{
	{name: "localhost", args: nil},
	{name: "10ms", args: []string{"delay", "10ms"}},
	{name: "50ms", args: []string{"delay", "50ms"}},
	{name: "100ms_jitter", args: []string{"delay", "100ms", "10ms"}},
	{name: "1pct_loss", args: []string{"delay", "10ms", "loss", "1%"}},
	{name: "5pct_loss", args: []string{"delay", "10ms", "loss", "5%"}},
	{name: "10pct_loss", args: []string{"delay", "10ms", "loss", "10%"}},
	{name: "jitter_30ms", args: []string{"delay", "20ms", "30ms", "25%"}},
	{name: "wan", args: []string{"delay", "30ms", "10ms", "25%", "loss", "1%", "rate", "10mbit"}},
	{name: "harsh", args: []string{"delay", "50ms", "20ms", "25%", "loss", "5%", "rate", "5mbit"}},
	{name: "burst_loss", args: []string{"delay", "10ms", "loss", "25%", "25%"}},
}

func scenarioByName(name string) scenario {
	for _, s := range csvScenarios {
		if s.name == name {
			return s
		}
	}
	panic("unknown scenario: " + name)
}

func applyScenario(t *testing.T, s scenario) {
	t.Helper()
	clearScenario()
	if s.args == nil {
		return // localhost — no netem
	}
	args := append([]string{"tc", "qdisc", "add", "dev", "lo", "root", "netem"}, s.args...)
	out, err := exec.Command("sudo", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("tc add %s: %v: %s", s.name, err, out)
	}
}

func clearScenario() {
	exec.Command("sudo", "tc", "qdisc", "del", "dev", "lo", "root").CombinedOutput()
}

// ─── CSV writer helper ─────────────────────────────────────────────────────

type csvWriter struct {
	f *os.File
	w *csv.Writer
}

func newCSV(t *testing.T, filename string, header []string) *csvWriter {
	t.Helper()
	f, err := os.Create(filename)
	if err != nil {
		t.Fatalf("create %s: %v", filename, err)
	}
	w := csv.NewWriter(f)
	w.Write(header)
	return &csvWriter{f: f, w: w}
}

func (c *csvWriter) write(record []string) {
	c.w.Write(record)
}

func (c *csvWriter) close() {
	c.w.Flush()
	c.f.Close()
}

// ─── 1. Steady-State Consensus ─────────────────────────────────────────────
// Pre-established mesh, measures per-round consensus time.
// This is the most production-relevant benchmark.

func TestCSV_SteadyStateConsensus(t *testing.T) {
	const samples = 50
	scenarios := []string{"localhost", "10ms", "50ms", "1pct_loss", "5pct_loss", "10pct_loss", "wan", "harsh"}
	factories := []tFactory{tcpTFactory(), quicTFactory()}
	nodes := 4
	validators := makeValidators(nodes)

	out := newCSV(t, "results/steady_state_consensus.csv",
		[]string{"benchmark", "transport", "scenario", "nodes", "sample", "value_ns"})
	defer out.close()

	for _, sn := range scenarios {
		sc := scenarioByName(sn)
		for _, fac := range factories {
			t.Logf("steady_state: %s / %s", fac.name, sn)

			// Set up mesh once per transport×scenario
			transports, cleanup := fac.setup(t, validators)
			for _, tr := range transports {
				tr.WaitForReady()
			}

			applyScenario(t, sc)
			config := ibft.NewConfig(validators, func(r core.Round) time.Duration { return 30 * time.Second })

			for s := 1; s <= samples; s++ {
				value := ibft.NewValue([]byte(fmt.Sprintf("ss-%s-%s-%d", fac.name, sn, s)))
				inst := core.Instance(fmt.Sprintf("ss-%s-%s-%d", fac.name, sn, s))
				ns := make([]*ibft.Ibft, nodes)
				for j, v := range validators {
					ns[j] = ibft.NewIbft(v, config, transports[j], ibft.NewStore(), silentLogger)
				}

				start := time.Now()
				for _, nd := range ns {
					go nd.Start(context.Background(), inst, value)
				}
				for _, nd := range ns {
					<-nd.Done()
				}
				elapsed := time.Since(start)

				for _, nd := range ns {
					nd.Stop()
				}

				out.write([]string{
					"steady_state_consensus", fac.name, sn,
					fmt.Sprint(nodes), fmt.Sprint(s), fmt.Sprint(elapsed.Nanoseconds()),
				})
			}

			clearScenario()
			cleanup()
		}
	}
}

// ─── 2. Steady-State Consensus 7-Node ──────────────────────────────────────

func TestCSV_SteadyStateConsensus7N(t *testing.T) {
	const samples = 30
	scenarios := []string{"localhost", "10ms", "1pct_loss", "5pct_loss", "wan", "harsh"}
	factories := []tFactory{tcpTFactory(), quicTFactory()}
	nodes := 7
	validators := makeValidators(nodes)

	out := newCSV(t, "results/steady_state_consensus_7n.csv",
		[]string{"benchmark", "transport", "scenario", "nodes", "sample", "value_ns"})
	defer out.close()

	for _, sn := range scenarios {
		sc := scenarioByName(sn)
		for _, fac := range factories {
			t.Logf("steady_state_7n: %s / %s", fac.name, sn)

			transports, cleanup := fac.setup(t, validators)
			for _, tr := range transports {
				tr.WaitForReady()
			}

			applyScenario(t, sc)
			config := ibft.NewConfig(validators, func(r core.Round) time.Duration { return 30 * time.Second })

			for s := 1; s <= samples; s++ {
				value := ibft.NewValue([]byte(fmt.Sprintf("ss7-%s-%s-%d", fac.name, sn, s)))
				inst := core.Instance(fmt.Sprintf("ss7-%s-%s-%d", fac.name, sn, s))
				ns := make([]*ibft.Ibft, nodes)
				for j, v := range validators {
					ns[j] = ibft.NewIbft(v, config, transports[j], ibft.NewStore(), silentLogger)
				}

				start := time.Now()
				for _, nd := range ns {
					go nd.Start(context.Background(), inst, value)
				}
				for _, nd := range ns {
					<-nd.Done()
				}
				elapsed := time.Since(start)
				for _, nd := range ns {
					nd.Stop()
				}

				out.write([]string{
					"steady_state_consensus_7n", fac.name, sn,
					fmt.Sprint(nodes), fmt.Sprint(s), fmt.Sprint(elapsed.Nanoseconds()),
				})
			}

			clearScenario()
			cleanup()
		}
	}
}

// ─── 3. Consensus With Setup Cost ──────────────────────────────────────────
// Each sample includes full transport setup + consensus.

func TestCSV_ConsensusWithSetup(t *testing.T) {
	const samples = 30
	scenarios := []string{"localhost", "10ms", "50ms", "5pct_loss", "wan", "harsh"}
	factories := []tFactory{tcpTFactory(), quicTFactory()}
	nodes := 4
	validators := makeValidators(nodes)

	out := newCSV(t, "results/consensus_with_setup.csv",
		[]string{"benchmark", "transport", "scenario", "nodes", "sample", "value_ns"})
	defer out.close()

	for _, sn := range scenarios {
		sc := scenarioByName(sn)
		for _, fac := range factories {
			t.Logf("consensus_setup: %s / %s", fac.name, sn)
			applyScenario(t, sc)

			for s := 1; s <= samples; s++ {
				start := time.Now()

				transports, cleanup := fac.setup(t, validators)
				config := ibft.NewConfig(validators, func(r core.Round) time.Duration { return 30 * time.Second })
				value := ibft.NewValue([]byte(fmt.Sprintf("cs-%s-%s-%d", fac.name, sn, s)))
				inst := core.Instance(fmt.Sprintf("cs-%s-%s-%d", fac.name, sn, s))

				ns := make([]*ibft.Ibft, nodes)
				for j, v := range validators {
					ns[j] = ibft.NewIbft(v, config, transports[j], ibft.NewStore(), silentLogger)
				}
				for _, nd := range ns {
					go nd.Start(context.Background(), inst, value)
				}
				for _, nd := range ns {
					<-nd.Done()
				}
				elapsed := time.Since(start)
				for _, nd := range ns {
					nd.Stop()
				}
				cleanup()

				out.write([]string{
					"consensus_with_setup", fac.name, sn,
					fmt.Sprint(nodes), fmt.Sprint(s), fmt.Sprint(elapsed.Nanoseconds()),
				})
			}
			clearScenario()
		}
	}
}

// ─── 4. Message Latency ────────────────────────────────────────────────────
// Point-to-point Send→Receive latency.

func TestCSV_MessageLatency(t *testing.T) {
	const samples = 100
	scenarios := []string{"localhost", "10ms", "50ms", "jitter_30ms", "1pct_loss", "5pct_loss"}
	factories := []tFactory{tcpTFactory(), quicTFactory()}

	out := newCSV(t, "results/message_latency.csv",
		[]string{"benchmark", "transport", "scenario", "sample", "value_ns"})
	defer out.close()

	ids := makeValidators(2)

	for _, sn := range scenarios {
		sc := scenarioByName(sn)
		for _, fac := range factories {
			t.Logf("msg_latency: %s / %s", fac.name, sn)

			transports, cleanup := fac.setup(t, ids)
			for _, tr := range transports {
				tr.WaitForReady()
			}
			applyScenario(t, sc)

			ch := transports[1].Subscribe()
			msg := &ibft.Message{
				MessageType: ibft.MessageTypePrepare,
				From:        ids[0],
				Instance:    "latency",
				Round:       1,
				Value:       ibft.NewValue([]byte("ping")),
			}
			ctx := context.Background()

			for s := 1; s <= samples; s++ {
				start := time.Now()
				transports[0].Send(ctx, ids[1], msg)
				<-ch
				elapsed := time.Since(start)

				out.write([]string{
					"message_latency", fac.name, sn,
					fmt.Sprint(s), fmt.Sprint(elapsed.Nanoseconds()),
				})
			}

			clearScenario()
			cleanup()
		}
	}
}

// ─── 5. Throughput ─────────────────────────────────────────────────────────
// Measures time for a batch of broadcasts. Each sample = 100 broadcasts.

func TestCSV_Throughput(t *testing.T) {
	const samples = 50
	const batchSize = 100
	scenarios := []string{"localhost", "10ms", "1pct_loss", "5pct_loss"}
	factories := []tFactory{tcpTFactory(), quicTFactory()}

	out := newCSV(t, "results/throughput.csv",
		[]string{"benchmark", "transport", "scenario", "sample", "batch_size", "batch_duration_ns", "ops_per_sec"})
	defer out.close()

	ids := makeValidators(4)

	for _, sn := range scenarios {
		sc := scenarioByName(sn)
		for _, fac := range factories {
			t.Logf("throughput: %s / %s", fac.name, sn)

			transports, cleanup := fac.setup(t, ids)
			for _, tr := range transports {
				tr.WaitForReady()
			}
			for _, tr := range transports {
				ch := tr.Subscribe()
				go func(c <-chan core.Message) { for range c {} }(ch)
			}
			applyScenario(t, sc)

			msg := &ibft.Message{
				MessageType: ibft.MessageTypePrepare,
				From:        ids[0],
				Instance:    "throughput",
				Round:       1,
				Value:       ibft.NewValue([]byte("bench")),
			}
			ctx := context.Background()

			for s := 1; s <= samples; s++ {
				start := time.Now()
				for j := 0; j < batchSize; j++ {
					transports[0].Broadcast(ctx, msg)
				}
				elapsed := time.Since(start)
				opsPerSec := float64(batchSize) / elapsed.Seconds()

				out.write([]string{
					"throughput", fac.name, sn,
					fmt.Sprint(s), fmt.Sprint(batchSize),
					fmt.Sprint(elapsed.Nanoseconds()),
					fmt.Sprintf("%.2f", opsPerSec),
				})
			}

			clearScenario()
			cleanup()
		}
	}
}

// ─── 6. HOL Blocking ──────────────────────────────────────────────────────
// Control-plane latency under data-plane flood.
// Outputs both total iteration time and control-message latency.

func TestCSV_HOLBlocking(t *testing.T) {
	const samples = 30
	const dataFlood = 20
	scenarios := []string{"localhost", "1pct_loss", "5pct_loss", "wan", "harsh"}
	// TCP uses default factory, QUIC uses multistream classifier
	type namedFactory struct {
		name string
		fac  tFactory
	}
	factories := []namedFactory{
		{"TCP", tcpTFactory()},
		{"QUIC", quicMultistreamTFactory()},
	}

	out := newCSV(t, "results/hol_blocking.csv",
		[]string{"benchmark", "transport", "scenario", "sample", "control_latency_ns", "total_iteration_ns"})
	defer out.close()

	ids := makeValidators(2)

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

	for _, sn := range scenarios {
		sc := scenarioByName(sn)
		for _, nf := range factories {
			t.Logf("hol: %s / %s", nf.name, sn)

			transports, cleanup := nf.fac.setup(t, ids)
			for _, tr := range transports {
				tr.WaitForReady()
			}
			senderCh := transports[0].Subscribe()
			go func() { for range senderCh {} }()
			ch := transports[1].Subscribe()
			applyScenario(t, sc)

			ctx := context.Background()

			for s := 1; s <= samples; s++ {
				iterStart := time.Now()

				var wg sync.WaitGroup
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < dataFlood; j++ {
						transports[0].Broadcast(ctx, dataMsg)
					}
				}()

				time.Sleep(500 * time.Microsecond)
				sendStart := time.Now()
				transports[0].Send(ctx, ids[1], controlMsg)

				dataReceived := 0
				controlReceived := false
				var ctrlElapsed time.Duration
				for dataReceived < dataFlood || !controlReceived {
					select {
					case m := <-ch:
						if msg, ok := m.(*ibft.Message); ok {
							if msg.Instance == "control" && !controlReceived {
								ctrlElapsed = time.Since(sendStart)
								controlReceived = true
							} else if msg.Instance == "data" {
								dataReceived++
							}
						}
					case <-time.After(60 * time.Second):
						t.Fatalf("HOL %s/%s sample %d: timed out (data=%d/%d, ctrl=%v)",
							nf.name, sn, s, dataReceived, dataFlood, controlReceived)
					}
				}
				wg.Wait()
				iterElapsed := time.Since(iterStart)

				out.write([]string{
					"hol_blocking", nf.name, sn,
					fmt.Sprint(s),
					fmt.Sprint(ctrlElapsed.Nanoseconds()),
					fmt.Sprint(iterElapsed.Nanoseconds()),
				})
			}

			clearScenario()
			cleanup()
		}
	}
}

// ─── 7. Burst Recovery ─────────────────────────────────────────────────────
// Time to deliver a message under bursty loss conditions.

func TestCSV_BurstRecovery(t *testing.T) {
	const samples = 50
	factories := []tFactory{tcpTFactory(), quicTFactory()}

	out := newCSV(t, "results/burst_recovery.csv",
		[]string{"benchmark", "transport", "scenario", "sample", "value_ns"})
	defer out.close()

	ids := makeValidators(2)

	for _, fac := range factories {
		t.Logf("burst: %s", fac.name)

		transports, cleanup := fac.setup(t, ids)
		for _, tr := range transports {
			tr.WaitForReady()
		}

		senderCh := transports[0].Subscribe()
		go func() { for range senderCh {} }()
		ch := transports[1].Subscribe()
		ctx := context.Background()

		// Warmup
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

		sc := scenarioByName("burst_loss")
		applyScenario(t, sc)

		msg := &ibft.Message{
			MessageType: ibft.MessageTypePrepare,
			From:        ids[0],
			Instance:    "recovery",
			Round:       1,
			Value:       ibft.NewValue([]byte("post-burst")),
		}

		for s := 1; s <= samples; s++ {
			start := time.Now()
			transports[0].Send(ctx, ids[1], msg)
			select {
			case <-ch:
			case <-time.After(30 * time.Second):
				t.Fatalf("burst recovery %s sample %d: timed out", fac.name, s)
			}
			elapsed := time.Since(start)

			out.write([]string{
				"burst_recovery", fac.name, "burst_loss",
				fmt.Sprint(s), fmt.Sprint(elapsed.Nanoseconds()),
			})
		}

		clearScenario()
		cleanup()
	}
}

// ─── 8. Payload Scaling ────────────────────────────────────────────────────
// Throughput at different payload sizes.

func TestCSV_PayloadScaling(t *testing.T) {
	const samples = 50
	payloads := []struct {
		name string
		size int
	}{
		{"128B", 128},
		{"1KB", 1024},
		{"16KB", 16 * 1024},
		{"64KB", 64 * 1024},
	}
	scenarios := []string{"localhost", "10ms"}
	factories := []tFactory{tcpTFactory(), quicTFactory()}

	out := newCSV(t, "results/payload_scaling.csv",
		[]string{"benchmark", "transport", "scenario", "payload_size", "sample", "duration_ns", "throughput_mbps"})
	defer out.close()

	ids := makeValidators(4)

	for _, sn := range scenarios {
		sc := scenarioByName(sn)
		for _, fac := range factories {
			transports, cleanup := fac.setup(t, ids)
			for _, tr := range transports {
				tr.WaitForReady()
			}
			for _, tr := range transports {
				ch := tr.Subscribe()
				go func(c <-chan core.Message) { for range c {} }(ch)
			}
			applyScenario(t, sc)

			ctx := context.Background()

			for _, pl := range payloads {
				t.Logf("payload: %s / %s / %s", fac.name, sn, pl.name)

				payload := make([]byte, pl.size)
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

				for s := 1; s <= samples; s++ {
					start := time.Now()
					transports[0].Broadcast(ctx, msg)
					elapsed := time.Since(start)
					mbps := float64(pl.size) / elapsed.Seconds() / 1e6

					out.write([]string{
						"payload_scaling", fac.name, sn, pl.name,
						fmt.Sprint(s), fmt.Sprint(elapsed.Nanoseconds()),
						fmt.Sprintf("%.4f", mbps),
					})
				}
			}

			clearScenario()
			cleanup()
		}
	}
}

// ─── 9. Connection Setup Time ──────────────────────────────────────────────
// Time to establish full mesh.

func TestCSV_ConnectionSetup(t *testing.T) {
	const samples = 50
	nodeCounts := []int{4, 7}
	factories := []tFactory{tcpTFactory(), quicTFactory()}

	out := newCSV(t, "results/connection_setup.csv",
		[]string{"benchmark", "transport", "nodes", "sample", "value_ns"})
	defer out.close()

	for _, n := range nodeCounts {
		ids := makeValidators(n)
		for _, fac := range factories {
			t.Logf("conn_setup: %s / %d nodes", fac.name, n)

			for s := 1; s <= samples; s++ {
				start := time.Now()
				transports, cleanup := fac.setup(t, ids)
				for _, tr := range transports {
					tr.WaitForReady()
				}
				elapsed := time.Since(start)
				cleanup()

				out.write([]string{
					"connection_setup", fac.name,
					fmt.Sprint(n), fmt.Sprint(s), fmt.Sprint(elapsed.Nanoseconds()),
				})
			}
		}
	}
}

// ─── 10. Round Change ──────────────────────────────────────────────────────
// Leader absent, measures time for round change + decision.

func TestCSV_RoundChange(t *testing.T) {
	const samples = 20
	scenarios := []string{"localhost", "10ms", "wan"}
	factories := []tFactory{tcpTFactory(), quicTFactory()}
	validators := makeValidators(4)
	activeIDs := validators[1:]

	out := newCSV(t, "results/round_change.csv",
		[]string{"benchmark", "transport", "scenario", "sample", "value_ns"})
	defer out.close()

	for _, sn := range scenarios {
		sc := scenarioByName(sn)
		for _, fac := range factories {
			t.Logf("round_change: %s / %s", fac.name, sn)

			transports, cleanup := fac.setup(t, activeIDs)
			for _, tr := range transports {
				tr.WaitForReady()
			}
			applyScenario(t, sc)

			for s := 1; s <= samples; s++ {
				config := ibft.NewConfig(validators, func(r core.Round) time.Duration { return 200 * time.Millisecond })
				value := ibft.NewValue([]byte(fmt.Sprintf("rc-%s-%s-%d", fac.name, sn, s)))
				inst := core.Instance(fmt.Sprintf("rc-%s-%s-%d", fac.name, sn, s))
				ns := make([]*ibft.Ibft, len(activeIDs))
				for j, v := range activeIDs {
					ns[j] = ibft.NewIbft(v, config, transports[j], ibft.NewStore(), silentLogger)
				}

				start := time.Now()
				for _, nd := range ns {
					go nd.Start(context.Background(), inst, value)
				}
				for _, nd := range ns {
					<-nd.Done()
				}
				elapsed := time.Since(start)
				for _, nd := range ns {
					nd.Stop()
				}

				out.write([]string{
					"round_change", fac.name, sn,
					fmt.Sprint(s), fmt.Sprint(elapsed.Nanoseconds()),
				})
			}

			clearScenario()
			cleanup()
		}
	}
}
