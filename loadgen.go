package benchmark

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The load generator drives each endpoint two ways, because the two answer
// different questions and neither one alone is a usable regression signal.
//
// Closed-loop (bounded worker pool, unlimited rate) answers "how much can it
// serve?". Its latency numbers are not comparable between two builds: at a fixed
// concurrency, a build that serves 60% more requests per second is doing 60%
// more work at every instant, so its tail rises even though it is strictly
// faster. Only the throughput column from this pass is worth diffing.
//
// Open-loop (fixed request rate, workers spawned as needed) answers "how fast is
// a request when the offered load is X?". Both builds are handed the identical
// arrival process, so p95 compares directly with no throughput coupling. This is
// the pass that catches a latency regression, and the reason the rates must be
// pinned across an A/B rather than recomputed per half.

// Fractions of measured capacity the open-loop ladder walks.
//
// Weighted low on purpose. Closed-loop capacity is a saturation throughput,
// measured with every worker already queued behind another; the latency knee
// sits well under it. Measured on limit=1000, 25% of capacity already ran at 20x
// the median of 10%, so a ladder starting at a quarter can land entirely past
// the knee and leave nothing comparable.
//
// The top rung sits at capacity itself and is expected to saturate: it is the
// control proving the ladder spans the interesting range rather than idling
// under it. The bottom rungs are the ones that carry the latency signal.
var ladderFractions = []float64{0.10, 0.25, 0.50, 0.75, 1.00}

const (
	// A rung that fails to reach this fraction of its target rate is saturated:
	// the server (or the generator) could not keep up, so its latencies describe
	// queue depth rather than request cost.
	rateTolerance = 0.95
	// Below this success fraction the run is dropping or erroring requests, and
	// the surviving latencies are a biased sample of the fast ones.
	successFloor = 0.995
	// Enough observations for a p95 that does not swing on one slow request.
	// Low rungs at limit=1000 would otherwise finish with a few dozen samples.
	minSamples = 500
	// How much the median may climb from the first half of a rung to the second
	// before the rung is called unstable. See latencyDrifting.
	latencyDriftFactor = 1.5
	// Below this many observations in a half, the two medians are too noisy to
	// read a trend from and the drift check abstains.
	minDriftSamples = 50
)

// loadConfig collects the knobs, all overridable so a noisy shared runner can
// trade resolution for wall time without a rebuild.
type loadConfig struct {
	concurrencyLevels []int
	capacityDuration  time.Duration
	rateDuration      time.Duration
	maxRateDuration   time.Duration
	requestTimeout    time.Duration
	pinned            map[string][]int
}

func loadConfigFromEnv() (loadConfig, error) {
	cfg := loadConfig{
		concurrencyLevels: []int{1, 10, 50, 100, 200},
		capacityDuration:  3 * time.Second,
		rateDuration:      4 * time.Second,
		maxRateDuration:   15 * time.Second,
		requestTimeout:    10 * time.Second,
	}

	var err error
	if cfg.capacityDuration, err = durationEnv("BENCH_CAPACITY_DURATION", cfg.capacityDuration); err != nil {
		return cfg, err
	}
	if cfg.rateDuration, err = durationEnv("BENCH_RATE_DURATION", cfg.rateDuration); err != nil {
		return cfg, err
	}
	if cfg.maxRateDuration, err = durationEnv("BENCH_MAX_RATE_DURATION", cfg.maxRateDuration); err != nil {
		return cfg, err
	}
	if cfg.requestTimeout, err = durationEnv("BENCH_REQUEST_TIMEOUT", cfg.requestTimeout); err != nil {
		return cfg, err
	}
	if cfg.pinned, err = parsePinnedRates(os.Getenv("BENCH_RATES")); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback, fmt.Errorf("%s: %w", name, err)
	}
	if d <= 0 {
		return fallback, fmt.Errorf("%s: must be positive, got %s", name, raw)
	}
	return d, nil
}

// cellKey names one (scenario, page size) pair. It is the unit a ladder is
// pinned against, since capacity differs by an order of magnitude between
// limit=10 and limit=1000.
func cellKey(scenario string, limit int) string {
	return scenario + "|" + strconv.Itoa(limit)
}

// ladderFor turns a measured capacity into the rates to offer. Rates are whole
// requests per second, deduplicated, and never zero: at capacities below 4 rps
// the lower fractions all collapse onto 1.
func ladderFor(capacity float64) []int {
	var rates []int
	seen := make(map[int]bool)
	for _, f := range ladderFractions {
		r := max(int(capacity*f+0.5), 1)
		if !seen[r] {
			seen[r] = true
			rates = append(rates, r)
		}
	}
	sort.Ints(rates)
	return rates
}

// formatPinnedRates renders one cell's ladder in the form BENCH_RATES accepts,
// so a transcript can be fed straight back into the candidate half.
func formatPinnedRates(key string, rates []int) string {
	parts := make([]string, len(rates))
	for i, r := range rates {
		parts[i] = strconv.Itoa(r)
	}
	return key + "=" + strings.Join(parts, ",")
}

// parsePinnedRates reads "list|100=300,600;list+count|10=2000" into per-cell
// ladders. An empty string means "derive from measured capacity".
func parsePinnedRates(raw string) (map[string][]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	out := make(map[string][]int)
	for entry := range strings.SplitSeq(raw, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, list, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("BENCH_RATES: entry %q is not key=rates", entry)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("BENCH_RATES: entry %q has an empty key", entry)
		}
		var rates []int
		for f := range strings.SplitSeq(list, ",") {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			r, err := strconv.Atoi(f)
			if err != nil {
				return nil, fmt.Errorf("BENCH_RATES: rate %q in %q: %w", f, key, err)
			}
			if r < 1 {
				return nil, fmt.Errorf("BENCH_RATES: rate %d in %q must be >= 1", r, key)
			}
			rates = append(rates, r)
		}
		if len(rates) == 0 {
			return nil, fmt.Errorf("BENCH_RATES: %q lists no rates", key)
		}
		sort.Ints(rates)
		out[key] = rates
	}
	return out, nil
}

// durationFor stretches a low rung until it has collected enough samples for a
// meaningful p95, then stops at the cap so a 12 rps rung does not run for a
// minute.
func durationFor(rate int, base, maxDuration time.Duration, samples int) time.Duration {
	needed := time.Duration(float64(samples) / float64(max(rate, 1)) * float64(time.Second))
	return min(max(needed, base), maxDuration)
}

// maxWorkersFor bounds the goroutines the open-loop pass may spawn. Vegeta grows
// its pool to hold the requested rate, so the pool has to be allowed to reach
// rate x latency; the timeout is the worst latency a request can reach, which
// makes rate x timeout the ceiling that never throttles below saturation.
//
// The cap matters: if it binds, the pass silently degrades into a closed-loop
// one and the numbers stop meaning what the column header says. It only binds
// past saturation, which the achieved-rate column already exposes.
func maxWorkersFor(rate int, timeout time.Duration) uint64 {
	const (
		floor   = 64
		ceiling = 8192
	)
	return uint64(min(max(int(float64(rate)*timeout.Seconds()), floor), ceiling))
}

// latencyDrifting reports whether the median climbed materially between the
// first and second half of a rung, which means requests were arriving faster
// than they were being retired and a queue was still growing when the run
// stopped.
//
// This is the condition that "did it hold the rate?" alone misses, and it is the
// common case: a server at or past its knee does not refuse the load, it absorbs
// it. The offered rate is met and nothing errors, while latency climbs for as
// long as you keep going. A p95 measured there is a function of how long the
// rung ran, so it is not a property of the build and must not be compared. It
// showed up on the first real run as a 2.24s p95 that no other check flagged.
func latencyDrifting(first, second time.Duration, firstN, secondN uint64) bool {
	if firstN < minDriftSamples || secondN < minDriftSamples || first <= 0 {
		return false
	}
	return float64(second) > float64(first)*latencyDriftFactor
}

// saturated reports whether a rung failed to hold its offered rate, either by
// falling behind it or by not answering cleanly. Callers combine this with
// latencyDrifting: both mean the rung's latencies describe queue depth rather
// than request cost, and both are reported as the same SATURATED flag.
func saturated(target int, achieved, success float64) bool {
	return achieved < float64(target)*rateTolerance || success < successFloor
}

// formatRateRow renders one open-loop rung. The column layout is a contract with
// scripts/bench-compare.sh in the blog repo, which reads the row positionally:
// seven whitespace-separated fields, plus the flag as an eighth when present.
//
// The success column is preformatted into a single string on purpose. Padding it
// as "%-10.2f%%" would put the padding between the number and the sign, which
// splits one column into two fields and pushes the flag out of position.
func formatRateRow(rate int, achieved float64, p50, p95, p99 time.Duration, requests uint64, success float64, flag string) string {
	return fmt.Sprintf("  %-12d  %-10.0f  %-10s  %-10s  %-10s  %-10d  %-10s  %s",
		rate,
		achieved,
		fmtDuration(p50),
		fmtDuration(p95),
		fmtDuration(p99),
		requests,
		fmt.Sprintf("%.2f%%", success*100),
		flag,
	)
}
