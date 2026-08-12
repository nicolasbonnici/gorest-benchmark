# GoREST Benchmark Plugin

[![CI](https://github.com/nicolasbonnici/gorest-benchmark/actions/workflows/ci.yml/badge.svg)](https://github.com/nicolasbonnici/gorest-benchmark/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/nicolasbonnici/gorest-benchmark.svg)](https://pkg.go.dev/github.com/nicolasbonnici/gorest-benchmark)
[![Go Version](https://img.shields.io/github/go-mod/go-version/nicolasbonnici/gorest-benchmark)](https://github.com/nicolasbonnici/gorest-benchmark/blob/HEAD/go.mod)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

API performance benchmarking plugin for GoREST framework.

## Installation

```bash
go get github.com/nicolasbonnici/gorest-benchmark
```


## Development Environment

To set up your development environment:

```bash
make install
```

This will:
- Install Go dependencies
- Install development tools (golangci-lint)
- Set up git hooks (pre-commit linting and tests)

## Dependencies

This plugin requires the following dependencies:

- **GoREST Framework** (v0.4.8+): Core framework
- **gorest-codegen**: Code generation plugin for creating models and API resources during benchmarks

The codegen dependency is used internally to generate temporary models and resources for benchmark testing.

## Usage

### As a CLI Command

```go
import (
	_ "github.com/nicolasbonnici/gorest-benchmark"
)
```

Then run:
```bash
go run cmd/main.go
```

### Configuration

Add to your `gorest.yaml`:

```yaml
plugins:
  - name: benchmark
    enabled: true
```

## Features

- Automated API performance testing
- Load testing with different data sizes (10, 100, 1000 records)
- Two measurement passes per endpoint, closed-loop and open-loop
- Latency percentiles (p50, p95, p99)
- Request per second (RPS) metrics
- Error rate tracking

## How each endpoint is measured

Every (scenario, page size) pair runs two passes, because they answer different
questions and only one of them produces comparable latency.

**Capacity (closed-loop).** Concurrency levels 1, 10, 50, 100, 200, each worker
issuing its next request as soon as the previous returns. Reports measured
throughput; the peak across the sweep is the capacity number. The latency columns
in this pass are for reading, not for comparing between builds: at a fixed
concurrency a build serving 60% more requests per second is doing 60% more work
at every instant, so its tail rises even when it is strictly faster.

**Latency at fixed rate (open-loop).** A ladder at 10/25/50/75/100% of measured
capacity. Requests leave on a schedule and workers are spawned as needed to hold
it, so a slow response delays no subsequent request. Latency here is a property
of the build at a stated offered load, which is what makes it comparable across
an A/B. The ladder is weighted low because the latency knee sits well under
closed-loop capacity; the bottom rungs carry the signal and the top rung is the
saturation control.

A rung is marked `SATURATED` when any of three things happen: it could not hold
its offered rate, it stopped answering cleanly, or its median climbed by more
than half between the first and second half of the run. That last one is the
common case and the one a rate check alone misses: a server past its knee does
not refuse load, it absorbs it, so the rate is met and nothing errors while
latency grows for as long as you keep going. Past any of these the latency
describes queue depth rather than request cost, and `bench-compare.sh` skips the
row instead of diffing it.

Each ladder is printed back in `BENCH_RATES` form so a second run can be pinned
to it:

```
  BENCH_RATES list|100=328,656,983,1180,1311
```

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | (required) | Database to seed and benchmark against |
| `BENCH_RATES` | derived | Pin the open-loop ladders instead of sizing them from measured capacity: `list\|100=300,600;list+count\|10=2000,4000`. Required for a meaningful A/B, since a faster candidate left to size its own ladder gets measured at higher rates *because* it is faster |
| `BENCH_CAPACITY_DURATION` | `3s` | Time per closed-loop concurrency level |
| `BENCH_RATE_DURATION` | `4s` | Minimum time per open-loop rung; low rates run longer to collect enough samples for a stable p95 |
| `BENCH_MAX_RATE_DURATION` | `15s` | Cap on that stretch, so one slow rung cannot eat the budget |
| `BENCH_REQUEST_TIMEOUT` | `10s` | Per-request timeout; also bounds the open-loop worker pool |

---

## Git Hooks

This directory contains git hooks for the GoREST plugin to maintain code quality.

### Available Hooks

#### pre-commit

Runs before each commit to ensure code quality:
- **Linting**: Runs `make lint` to check code style and potential issues
- **Tests**: Runs `make test` to verify all tests pass

### Installation

#### Automatic Installation

Run the install script from the project root:

```bash
./.githooks/install.sh
```

#### Manual Installation

Copy the hooks to your `.git/hooks` directory:

```bash
cp .githooks/pre-commit .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

---


## License

MIT License - see LICENSE file for details
