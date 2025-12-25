# GoREST Benchmark Plugin

API performance benchmarking plugin for GoREST framework.

## Installation

```bash
go get github.com/nicolasbonnici/gorest-benchmark
```

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

## License

MIT License - see LICENSE file for details
