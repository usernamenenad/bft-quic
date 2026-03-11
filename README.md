# BFT-QUIC

A Byzantine Fault Tolerant consensus engine with pluggable transport layers — **QUIC** and **TCP** — for comparing their performance characteristics in BFT protocols.

## Overview

This project implements the **Istanbul BFT (IBFT)** consensus algorithm in Go with a clean separation between the consensus logic and the network transport. Two transport backends are provided:

- **QUIC** — UDP-based, with multi-stream multiplexing and datagram heartbeats (via [quic-go](https://github.com/quic-go/quic-go))
- **TCP** — traditional full-mesh TCP connections

The primary goal is to **measure and compare** how QUIC and TCP behave as transport layers for BFT consensus under various network conditions (latency, packet loss, jitter, bandwidth limits).

## Architecture

```
┌───────────────────────────────────────────┐
│          IBFT Consensus Engine            │
│   (impl/ibft/ — state machine, timers,    │
│    message handlers, round-robin leader)  │
└─────────────────┬─────────────────────────┘
                  │ implements core.Transport
        ┌─────────┴──────────┐
        │                    │
┌───────────────┐    ┌───────────────┐
│     QUIC      │    │      TCP      │
│  Multi-stream │    │  Full-mesh    │
│  UDP + TLS1.3 │    │  Single-conn  │
└───────────────┘    └───────────────┘
```

### Consensus Protocol

IBFT uses four message phases:

1. **PRE-PREPARE** — Leader proposes a value
2. **PREPARE** — Validators acknowledge the proposal
3. **COMMIT** — Validators commit; quorum decides
4. **ROUND-CHANGE** — Recovery when the leader is absent or faulty

Fault tolerance: **f = ⌊(N−1)/3⌋** Byzantine faults, quorum size **2f + 1**.

### QUIC Multi-Stream Design

QUIC separates traffic into independent streams per connection:

| Stream | Type Byte | Purpose |
|--------|-----------|---------|
| Control | `0x00` | Consensus messages (PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE) |
| Data | `0x01` | Bulk transfers (state sync, large payloads) |

This mitigates **Head-of-Line (HOL) blocking** — a large data transfer on the data stream does not delay small control messages.

## Project Structure

```
bft-quic/
├── core/                       # Interfaces: Transport, Store, Node, Value
├── impl/ibft/                  # IBFT consensus implementation
│   ├── ibft.go                 # Main consensus engine
│   ├── message.go              # Message types and certificates
│   ├── config.go               # BFT parameters (N, F, quorum)
│   ├── validator.go            # Justification rules
│   ├── codec.go                # JSON wire format
│   ├── state.go, store.go      # State machine and message storage
│   ├── timer.go, node.go       # Round timer and node identity
│   └── *_test.go               # Unit tests
├── transport/
│   ├── bft-quic/               # QUIC transport (multi-stream, heartbeat)
│   └── bft-tcp/                # TCP transport (full-mesh)
├── bench/                      # Benchmarks
│   ├── bench_test.go           # Go benchmarks (localhost)
│   ├── csv_test.go             # CSV data collection (netem scenarios)
│   └── netem_test.go           # tc netem network simulation benchmarks
├── run_benchmarks.sh           # Benchmark runner script
├── docker-compose.yaml         # Redis (infrastructure for future use)
└── go.mod
```

## Prerequisites

- **Go 1.25+**
- **Linux** (for network simulation benchmarks)
- `iproute2` with `sch_netem` kernel module (for `tc netem`)
- Passwordless `sudo` access for `tc` (for netem benchmarks)

## Quick Start

```bash
# Clone and build
git clone https://github.com/usernamenenad/bft-quic.git
cd bft-quic
go mod download

# Run all tests
go test ./...
```

## Running Benchmarks

### Using the Script

```bash
# Localhost-only benchmarks (no sudo required)
./run_benchmarks.sh

# Include tc netem network simulation
./run_benchmarks.sh --netem

# CSV data collection with all netem scenarios
./run_benchmarks.sh --csv

# Everything
./run_benchmarks.sh --all
```

### Manually

```bash
# Localhost Go benchmarks
go test -run='^$' -bench=. -benchtime=5x -count=3 -timeout=300s ./bench/

# CSV data collection (per-sample data for statistical analysis)
go test -v -run='TestCSV' -timeout=3600s ./bench/

# Netem benchmarks (requires sudo + tc)
go test -run='^$' -bench='BenchmarkNetem' -benchtime=5x -count=5 -timeout=600s -tags netem ./bench/
```

### Benchmark Categories

| Benchmark | Measures |
|-----------|----------|
| Consensus latency | Time for all honest nodes to decide (4N, 7N) |
| Round-change latency | Recovery time when the leader is absent |
| Connection setup | Time to establish a full-mesh network |
| Message throughput | Sustained broadcast operations per second |
| Message latency | Point-to-point Send → Subscribe round-trip time |
| Payload scaling | Throughput vs. message size (128 B – 64 KB) |
| HOL-blocking resistance | Control-plane latency under data-plane load |
| Burst recovery | Delivery time under bursty packet loss |

### Network Scenarios (netem)

| Scenario | Parameters |
|----------|------------|
| `localhost` | No emulation |
| `10ms` | 10 ms delay |
| `50ms` | 50 ms delay |
| `100ms_jitter` | 100 ms ± 10 ms delay |
| `jitter_30ms` | 20 ms ± 30 ms (25% correlation) |
| `1pct_loss` | 10 ms delay + 1% packet loss |
| `5pct_loss` | 10 ms delay + 5% packet loss |
| `10pct_loss` | 10 ms delay + 10% packet loss |
| `wan` | 30 ms ± 10 ms + 1% loss + 10 Mbit/s |
| `harsh` | 50 ms ± 20 ms + 5% loss + 5 Mbit/s |
| `burst_loss` | 10 ms delay + 25% correlated loss |

### CSV Output

CSV files are written to `bench/results/` with per-sample (non-aggregated) measurements suitable for statistical analysis and plotting.

### Configuring the Benchmark Runner

| Variable | Default | Description |
|----------|---------|-------------|
| `BENCH_TIME` | `5x` | Iterations per localhost benchmark |
| `BENCH_COUNT` | `3` | Repetitions per localhost benchmark |
| `NETEM_BENCH_TIME` | `5x` | Iterations per netem benchmark |
| `NETEM_BENCH_COUNT` | `5` | Repetitions per netem benchmark |
| `CSV_TIMEOUT` | `1800s` | Timeout per CSV test function |

Setting up passwordless `sudo` for `tc`:

```bash
echo "$(whoami) ALL=(ALL) NOPASSWD: /usr/sbin/tc" | sudo tee /etc/sudoers.d/tc-netem
```

## License

See [LICENSE](LICENSE) for details.

---

# BFT-QUIC (Srpski)

Vizantijski tolerantni (BFT) konsenzus mehanizam sa zamenljivim transportnim slojevima — **QUIC** i **TCP** — za poređenje njihovih performansi u BFT protokolima.

## Pregled

Projekat implementira **Istanbul BFT (IBFT)** konsenzus algoritam u programskom jeziku Go sa jasnim razdvajanjem konsenzus logike od mrežnog transporta. Ponuđena su dva transportna sloja:

- **QUIC** — baziran na UDP-u, sa multipleksiranjem više tokova podataka i heartbeat datagramima (koristi [quic-go](https://github.com/quic-go/quic-go))
- **TCP** — tradicionalne TCP konekcije u full-mesh topologiji

Primarni cilj je **merenje i poređenje** ponašanja QUIC-a i TCP-a kao transportnih slojeva za BFT konsenzus pod različitim mrežnim uslovima (kašnjenje, gubitak paketa, džiter, ograničenje propusnog opsega).

## Arhitektura

```
┌───────────────────────────────────────────┐
│       IBFT konsenzus mehanizam            │
│  (impl/ibft/ — mašina stanja, tajmeri,   │
│   obrađivači poruka, round-robin lider)   │
└─────────────────┬─────────────────────────┘
                  │ implementira core.Transport
        ┌─────────┴──────────┐
        │                    │
┌───────────────┐    ┌───────────────┐
│     QUIC      │    │      TCP      │
│  Više tokova  │    │   Full-mesh   │
│  UDP + TLS1.3 │    │  Pojedinačna  │
│               │    │   konekcija   │
└───────────────┘    └───────────────┘
```

### Konsenzus protokol

IBFT koristi četiri faze poruka:

1. **PRE-PREPARE** — Lider predlaže vrednost
2. **PREPARE** — Validatori potvrđuju predlog
3. **COMMIT** — Validatori urezuju; kvorum odlučuje
4. **ROUND-CHANGE** — Oporavak kada lider nedostaje ili je neispravan

Tolerancija grešaka: **f = ⌊(N−1)/3⌋** vizantijskih grešaka, veličina kvoruma **2f + 1**.

### QUIC dizajn sa više tokova

QUIC razdvaja saobraćaj u nezavisne tokove po konekciji:

| Tok | Bajt tipa | Namena |
|-----|-----------|--------|
| Kontrolni | `0x00` | Konsenzus poruke (PRE-PREPARE, PREPARE, COMMIT, ROUND-CHANGE) |
| Podatkovni | `0x01` | Masovni transferi (sinhronizacija stanja, veliki paketi) |

Ovo ublažava **blokiranje čela reda (HOL blocking)** — veliki transfer podataka na podatkovnom toku ne usporava male kontrolne poruke.

## Struktura projekta

```
bft-quic/
├── core/                       # Interfejsi: Transport, Store, Node, Value
├── impl/ibft/                  # IBFT implementacija konsenzusa
│   ├── ibft.go                 # Glavni konsenzus mehanizam
│   ├── message.go              # Tipovi poruka i sertifikati
│   ├── config.go               # BFT parametri (N, F, kvorum)
│   ├── validator.go            # Pravila opravdanja
│   ├── codec.go                # JSON žičani format
│   ├── state.go, store.go      # Mašina stanja i skladište poruka
│   ├── timer.go, node.go       # Tajmer runde i identitet čvora
│   └── *_test.go               # Jedinični testovi
├── transport/
│   ├── bft-quic/               # QUIC transport (više tokova, heartbeat)
│   └── bft-tcp/                # TCP transport (full-mesh)
├── bench/                      # Merenja performansi
│   ├── bench_test.go           # Go benchmark-ovi (localhost)
│   ├── csv_test.go             # CSV prikupljanje podataka (netem scenariji)
│   └── netem_test.go           # tc netem mrežna simulacija
├── run_benchmarks.sh           # Skripta za pokretanje benchmark-ova
├── docker-compose.yaml         # Redis (infrastruktura za buduću upotrebu)
└── go.mod
```

## Preduslovi

- **Go 1.25+**
- **Linux** (za benchmark-ove sa mrežnom simulacijom)
- `iproute2` sa `sch_netem` kernel modulom (za `tc netem`)
- `sudo` pristup bez lozinke za `tc` (za netem benchmark-ove)

## Brzi početak

```bash
# Kloniranje i kompajliranje
git clone https://github.com/usernamenenad/bft-quic.git
cd bft-quic
go mod download

# Pokretanje svih testova
go test ./...
```

## Pokretanje benchmark-ova

### Korišćenjem skripte

```bash
# Samo localhost benchmark-ovi (bez sudo-a)
./run_benchmarks.sh

# Sa tc netem mrežnom simulacijom
./run_benchmarks.sh --netem

# CSV prikupljanje podataka sa svim netem scenarijima
./run_benchmarks.sh --csv

# Sve zajedno
./run_benchmarks.sh --all
```

### Ručno

```bash
# Localhost Go benchmark-ovi
go test -run='^$' -bench=. -benchtime=5x -count=3 -timeout=300s ./bench/

# CSV prikupljanje podataka (po-uzorku, za statističku analizu)
go test -v -run='TestCSV' -timeout=3600s ./bench/

# Netem benchmark-ovi (zahteva sudo + tc)
go test -run='^$' -bench='BenchmarkNetem' -benchtime=5x -count=5 -timeout=600s -tags netem ./bench/
```

### Kategorije benchmark-ova

| Benchmark | Šta meri |
|-----------|----------|
| Kašnjenje konsenzusa | Vreme do odluke svih ispravnih čvorova (4N, 7N) |
| Kašnjenje promene runde | Vreme oporavka kada lider nedostaje |
| Uspostavljanje konekcije | Vreme za uspostavljanje full-mesh mreže |
| Propusnost poruka | Održive broadcast operacije u sekundi |
| Kašnjenje pojedinačne poruke | Povratno vreme slanja i primanja poruke |
| Skaliranje veličine korisnog tereta | Propusnost u zavisnosti od veličine poruke (128 B – 64 KB) |
| Otpornost na HOL blokiranje | Kašnjenje kontrolne ravni pod opterećenjem podatkovne ravni |
| Oporavak od nalet gubitaka | Vreme isporuke pod uslovima naletnog gubitka paketa |

### Mrežni scenariji (netem)

| Scenario | Parametri |
|----------|-----------|
| `localhost` | Bez emulacije |
| `10ms` | Kašnjenje od 10 ms |
| `50ms` | Kašnjenje od 50 ms |
| `100ms_jitter` | 100 ms ± 10 ms kašnjenje |
| `jitter_30ms` | 20 ms ± 30 ms (25% korelacija) |
| `1pct_loss` | 10 ms kašnjenje + 1% gubitak paketa |
| `5pct_loss` | 10 ms kašnjenje + 5% gubitak paketa |
| `10pct_loss` | 10 ms kašnjenje + 10% gubitak paketa |
| `wan` | 30 ms ± 10 ms + 1% gubitak + 10 Mbit/s |
| `harsh` | 50 ms ± 20 ms + 5% gubitak + 5 Mbit/s |
| `burst_loss` | 10 ms kašnjenje + 25% korelisani gubitak |

### CSV izlaz

CSV fajlovi se upisuju u `bench/results/` sa merenjima po uzorku (neagregiranim), pogodnim za statističku analizu i crtanje grafika.

### Konfiguracija skripte za benchmark-ove

| Promenljiva | Podrazumevano | Opis |
|-------------|---------------|------|
| `BENCH_TIME` | `5x` | Iteracije po localhost benchmark-u |
| `BENCH_COUNT` | `3` | Ponavljanja po localhost benchmark-u |
| `NETEM_BENCH_TIME` | `5x` | Iteracije po netem benchmark-u |
| `NETEM_BENCH_COUNT` | `5` | Ponavljanja po netem benchmark-u |
| `CSV_TIMEOUT` | `1800s` | Tajmaut po CSV test funkciji |

Podešavanje `sudo` pristupa bez lozinke za `tc`:

```bash
echo "$(whoami) ALL=(ALL) NOPASSWD: /usr/sbin/tc" | sudo tee /etc/sudoers.d/tc-netem
```

## Licenca

Pogledajte [LICENSE](LICENSE) za detalje.
