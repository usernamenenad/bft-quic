#!/usr/bin/env bash
#
# run_benchmarks.sh — Run all BFT-QUIC benchmarks and collect CSV results.
#
# Usage:
#   ./run_benchmarks.sh              # localhost-only (no sudo required)
#   ./run_benchmarks.sh --netem      # include tc netem network simulation (requires sudo)
#   ./run_benchmarks.sh --csv        # CSV data collection with netem scenarios (requires sudo)
#   ./run_benchmarks.sh --all        # everything: localhost + netem + CSV
#
# Prerequisites:
#   - Go 1.25+
#   - Linux (for netem: iproute2, sch_netem kernel module, passwordless sudo for tc)

set -euo pipefail

# ─── Configuration ──────────────────────────────────────────────────────────

BENCH_TIME="${BENCH_TIME:-5x}"
BENCH_COUNT="${BENCH_COUNT:-3}"
NETEM_BENCH_TIME="${NETEM_BENCH_TIME:-5x}"
NETEM_BENCH_COUNT="${NETEM_BENCH_COUNT:-5}"
CSV_TIMEOUT="${CSV_TIMEOUT:-1800s}"
RESULTS_DIR="bench/results"

# ─── Color helpers ──────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()    { echo -e "${CYAN}[INFO]${NC}  $*"; }
ok()      { echo -e "${GREEN}[OK]${NC}    $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC}  $*"; }
fail()    { echo -e "${RED}[FAIL]${NC}  $*"; }
section() { echo -e "\n${CYAN}══════════════════════════════════════════════════${NC}"; echo -e "${CYAN}  $*${NC}"; echo -e "${CYAN}══════════════════════════════════════════════════${NC}\n"; }

# ─── Argument parsing ───────────────────────────────────────────────────────

RUN_LOCALHOST=true
RUN_NETEM=false
RUN_CSV=false

for arg in "$@"; do
    case "$arg" in
        --netem)    RUN_NETEM=true ;;
        --csv)      RUN_CSV=true ;;
        --all)      RUN_NETEM=true; RUN_CSV=true ;;
        --help|-h)
            echo "Usage: $0 [--netem] [--csv] [--all]"
            echo ""
            echo "Modes:"
            echo "  (default)   Localhost-only Go benchmarks (no sudo needed)"
            echo "  --netem     Include tc netem network simulation benchmarks (requires sudo)"
            echo "  --csv       Run CSV data collection tests with netem scenarios (requires sudo)"
            echo "  --all       Run everything"
            echo ""
            echo "Environment variables:"
            echo "  BENCH_TIME        Iterations for localhost benchmarks (default: 5x)"
            echo "  BENCH_COUNT       Repetitions for localhost benchmarks (default: 3)"
            echo "  NETEM_BENCH_TIME  Iterations for netem benchmarks (default: 5x)"
            echo "  NETEM_BENCH_COUNT Repetitions for netem benchmarks (default: 5)"
            echo "  CSV_TIMEOUT       Timeout per CSV test (default: 1800s)"
            exit 0
            ;;
        *) fail "Unknown argument: $arg"; exit 1 ;;
    esac
done

# ─── Preflight checks ──────────────────────────────────────────────────────

section "Preflight checks"

if ! command -v go &>/dev/null; then
    fail "Go is not installed"; exit 1
fi
ok "Go $(go version | awk '{print $3}')"

if ! go build ./... 2>/dev/null; then
    fail "Project does not compile"; exit 1
fi
ok "Project compiles"

if [[ "$RUN_NETEM" == true ]] || [[ "$RUN_CSV" == true ]]; then
    if ! command -v tc &>/dev/null; then
        fail "tc (iproute2) not found — required for netem benchmarks"; exit 1
    fi
    if ! sudo -n tc qdisc show dev lo &>/dev/null; then
        fail "Passwordless sudo for tc not available"
        echo "  Fix: echo '$(whoami) ALL=(ALL) NOPASSWD: /usr/sbin/tc' | sudo tee /etc/sudoers.d/tc-netem"
        exit 1
    fi
    ok "tc netem available with passwordless sudo"
fi

mkdir -p "$RESULTS_DIR"

# ─── Cleanup helper ────────────────────────────────────────────────────────

cleanup_netem() {
    sudo -n tc qdisc del dev lo root 2>/dev/null || true
}

trap cleanup_netem EXIT

# ─── 1. Localhost benchmarks ───────────────────────────────────────────────

if [[ "$RUN_LOCALHOST" == true ]]; then
    section "Localhost benchmarks (bench_test.go)"

    LOCALHOST_OUT="$RESULTS_DIR/go_bench_localhost.txt"

    # Run in groups to avoid port exhaustion on ConnSetup
    info "Running consensus, round-change, throughput, message latency..."
    go test -run='^$' \
        -bench='BenchmarkConsensus|BenchmarkRoundChange|BenchmarkThroughput|BenchmarkMsgLatency' \
        -benchtime="$BENCH_TIME" -count="$BENCH_COUNT" -timeout=300s \
        ./bench/ 2>&1 | tee "$LOCALHOST_OUT"
    ok "Core benchmarks done"

    info "Running connection setup..."
    go test -run='^$' \
        -bench='BenchmarkConnSetup' \
        -benchtime=3x -count="$BENCH_COUNT" -timeout=300s \
        ./bench/ 2>&1 | tee -a "$LOCALHOST_OUT"
    ok "Connection setup done"

    info "Running payload scaling and HOL blocking..."
    go test -run='^$' \
        -bench='BenchmarkPayload|BenchmarkHOL' \
        -benchtime="$BENCH_TIME" -count="$BENCH_COUNT" -timeout=300s \
        ./bench/ 2>&1 | tee -a "$LOCALHOST_OUT"
    ok "Payload + HOL done"

    ok "Localhost results saved to $LOCALHOST_OUT"
fi

# ─── 2. Netem benchmarks ───────────────────────────────────────────────────

if [[ "$RUN_NETEM" == true ]]; then
    section "Netem benchmarks (netem_test.go)"

    cleanup_netem

    NETEM_OUT="$RESULTS_DIR/go_bench_netem.txt"

    info "Running netem benchmarks (this may take several minutes)..."
    go test -run='^$' \
        -bench='BenchmarkNetem' \
        -benchtime="$NETEM_BENCH_TIME" -count="$NETEM_BENCH_COUNT" \
        -timeout=600s -tags netem \
        ./bench/ 2>&1 | tee "$NETEM_OUT"

    cleanup_netem
    ok "Netem results saved to $NETEM_OUT"
fi

# ─── 3. CSV data collection ────────────────────────────────────────────────

if [[ "$RUN_CSV" == true ]]; then
    section "CSV data collection (csv_test.go)"

    cleanup_netem

    CSV_TESTS=(
        TestCSV_SteadyStateConsensus
        TestCSV_SteadyStateConsensus7N
        TestCSV_ConsensusWithSetup
        TestCSV_MessageLatency
        TestCSV_Throughput
        TestCSV_HOLBlocking
        TestCSV_BurstRecovery
        TestCSV_PayloadScaling
        TestCSV_ConnectionSetup
        TestCSV_RoundChange
    )

    PASSED=0
    FAILED=0

    for test in "${CSV_TESTS[@]}"; do
        info "Running $test ..."
        cleanup_netem

        if go test -v -run="^${test}$" -timeout="$CSV_TIMEOUT" ./bench/ 2>&1 \
            | tee "$RESULTS_DIR/${test}.log" \
            | tail -3; then
            ok "$test passed"
            ((PASSED++))
        else
            fail "$test failed (see $RESULTS_DIR/${test}.log)"
            ((FAILED++))
        fi
        echo ""
    done

    cleanup_netem

    section "CSV collection summary"
    ok "$PASSED tests passed"
    if [[ $FAILED -gt 0 ]]; then
        fail "$FAILED tests failed"
    fi

    info "CSV files:"
    ls -lh "$RESULTS_DIR"/*.csv 2>/dev/null || warn "No CSV files found"
fi

# ─── Summary ────────────────────────────────────────────────────────────────

section "Done"
info "All results are in $RESULTS_DIR/"
ls -lh "$RESULTS_DIR"/ 2>/dev/null
