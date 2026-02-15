# GoREST Benchmark Plugin

[![CI](https://github.com/nicolasbonnici/gorest-benchmark/actions/workflows/ci.yml/badge.svg)](https://github.com/nicolasbonnici/gorest-benchmark/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/nicolasbonnici/gorest-benchmark)](https://goreportcard.com/report/github.com/nicolasbonnici/gorest-benchmark)
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
- Multiple concurrency levels (1, 10, 50)
- Load testing with different data sizes (10, 100, 1000 records)
- Latency percentiles (p50, p95, p99)
- Request per second (RPS) metrics
- Error rate tracking

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
