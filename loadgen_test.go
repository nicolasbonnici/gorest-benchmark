package benchmark

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLadderFor(t *testing.T) {
	cases := []struct {
		name     string
		capacity float64
		want     []int
	}{
		{"typical", 1200, []int{120, 300, 600, 900, 1200}},
		{"rounds to nearest", 1204, []int{120, 301, 602, 903, 1204}},
		// limit=1000 on a loaded runner lands here; the low fractions collapse
		// and must not produce a rate of 0, which vegeta reads as "unlimited"
		// and would silently turn the rung closed-loop.
		{"tiny capacity", 3, []int{1, 2, 3}},
		{"zero capacity", 0, []int{1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ladderFor(tc.capacity)
			if !slices.Equal(got, tc.want) {
				t.Errorf("ladderFor(%v) = %v, want %v", tc.capacity, got, tc.want)
			}
			if slices.Contains(got, 0) {
				t.Error("ladder contains rate 0")
			}
		})
	}
}

func TestParsePinnedRates(t *testing.T) {
	got, err := parsePinnedRates("list|100=301,602,903 ; list+count|10=2000,4000")
	if err != nil {
		t.Fatalf("parsePinnedRates: %v", err)
	}
	if want := []int{301, 602, 903}; !slices.Equal(got["list|100"], want) {
		t.Errorf("list|100 = %v, want %v", got["list|100"], want)
	}
	if want := []int{2000, 4000}; !slices.Equal(got["list+count|10"], want) {
		t.Errorf("list+count|10 = %v, want %v", got["list+count|10"], want)
	}

	if m, err := parsePinnedRates(""); err != nil || m != nil {
		t.Errorf("empty input = (%v, %v), want (nil, nil)", m, err)
	}

	for _, bad := range []string{
		"list|100",     // no '='
		"list|100=",    // no rates
		"list|100=abc", // not a number
		"list|100=0",   // 0 means "unlimited" to vegeta
		"list|100=-5",  // negative
		"=300",         // no key
	} {
		if _, err := parsePinnedRates(bad); err == nil {
			t.Errorf("parsePinnedRates(%q) accepted invalid input", bad)
		}
	}
}

// TestPinnedRatesRoundTrip is the contract scripts/bench-ab.sh depends on: the
// ladder a baseline transcript prints must parse back to the same rates when the
// candidate half receives it as BENCH_RATES.
func TestPinnedRatesRoundTrip(t *testing.T) {
	key := cellKey("list+count", 1000)
	rates := ladderFor(188)

	parsed, err := parsePinnedRates(formatPinnedRates(key, rates))
	if err != nil {
		t.Fatalf("parsePinnedRates: %v", err)
	}
	if !slices.Equal(parsed[key], rates) {
		t.Errorf("round trip = %v, want %v", parsed[key], rates)
	}
}

func TestDurationFor(t *testing.T) {
	base, maxDuration := 4*time.Second, 15*time.Second

	cases := []struct {
		name string
		rate int
		want time.Duration
	}{
		// 2000 rps clears 500 samples well inside the base duration.
		{"fast rung uses base", 2000, 4 * time.Second},
		// 50 rps needs 10s to reach 500 samples.
		{"slow rung stretches", 50, 10 * time.Second},
		// 12 rps would need 42s; the cap wins so one rung cannot eat the budget.
		{"very slow rung is capped", 12, 15 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := durationFor(tc.rate, base, maxDuration, minSamples); got != tc.want {
				t.Errorf("durationFor(%d) = %s, want %s", tc.rate, got, tc.want)
			}
		})
	}

	if got := durationFor(0, base, maxDuration, minSamples); got != maxDuration {
		t.Errorf("durationFor(0) = %s, want %s (must not divide by zero)", got, maxDuration)
	}
}

func TestMaxWorkersFor(t *testing.T) {
	timeout := 10 * time.Second

	// The pool has to be allowed to reach rate x latency or the pass throttles
	// itself into a closed-loop one.
	if got := maxWorkersFor(500, timeout); got != 5000 {
		t.Errorf("maxWorkersFor(500) = %d, want 5000", got)
	}
	if got := maxWorkersFor(1, timeout); got != 64 {
		t.Errorf("maxWorkersFor(1) = %d, want the floor 64", got)
	}
	if got := maxWorkersFor(100000, timeout); got != 8192 {
		t.Errorf("maxWorkersFor(100000) = %d, want the ceiling 8192", got)
	}
}

func TestSaturated(t *testing.T) {
	cases := []struct {
		name     string
		target   int
		achieved float64
		success  float64
		want     bool
	}{
		{"holding the rate", 1000, 1000, 1.0, false},
		{"just inside tolerance", 1000, 960, 1.0, false},
		{"fell behind the rate", 1000, 900, 1.0, true},
		{"holding but erroring", 1000, 1000, 0.98, true},
		{"slightly over target", 1000, 1004, 1.0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := saturated(tc.target, tc.achieved, tc.success); got != tc.want {
				t.Errorf("saturated(%d, %v, %v) = %v, want %v",
					tc.target, tc.achieved, tc.success, got, tc.want)
			}
		})
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("BENCH_RATE_DURATION", "7s")
	t.Setenv("BENCH_RATES", "list|10=100,200")

	cfg, err := loadConfigFromEnv()
	if err != nil {
		t.Fatalf("loadConfigFromEnv: %v", err)
	}
	if cfg.rateDuration != 7*time.Second {
		t.Errorf("rateDuration = %s, want 7s", cfg.rateDuration)
	}
	if cfg.capacityDuration != 3*time.Second {
		t.Errorf("capacityDuration = %s, want the 3s default", cfg.capacityDuration)
	}
	if !slices.Equal(cfg.pinned["list|10"], []int{100, 200}) {
		t.Errorf("pinned = %v, want [100 200]", cfg.pinned["list|10"])
	}

	t.Setenv("BENCH_RATE_DURATION", "banana")
	if _, err := loadConfigFromEnv(); err == nil {
		t.Error("expected an error for an unparseable duration")
	}
	t.Setenv("BENCH_RATE_DURATION", "-3s")
	if _, err := loadConfigFromEnv(); err == nil {
		t.Error("expected an error for a negative duration")
	}
}

// TestFormatRateRowFields locks the layout scripts/bench-compare.sh parses
// positionally: seven fields, or eight when a rung saturated. A padded "%%"
// silently split the success column into two fields once already.
func TestFormatRateRowFields(t *testing.T) {
	cases := []struct {
		name   string
		flag   string
		fields int
	}{
		{"unflagged row", "", 7},
		{"saturated row", "SATURATED", 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := formatRateRow(1311, 1152, 18*time.Millisecond, 95*time.Millisecond,
				200*time.Millisecond, 5760, 0.9983, tc.flag)

			f := strings.Fields(row)
			if len(f) != tc.fields {
				t.Fatalf("got %d fields %q, want %d", len(f), f, tc.fields)
			}
			if f[0] != "1311" {
				t.Errorf("field 1 (rate) = %q, want the offered rate", f[0])
			}
			if f[3] != "95.00ms" {
				t.Errorf("field 4 (p95) = %q, want 95.00ms", f[3])
			}
			if f[6] != "99.83%" {
				t.Errorf("field 7 (success) = %q, want a single 99.83%% field", f[6])
			}
			if tc.flag != "" && f[7] != "SATURATED" {
				t.Errorf("field 8 (flag) = %q, want SATURATED", f[7])
			}
		})
	}
}

func TestLatencyDrifting(t *testing.T) {
	const n = 1000
	ms := func(x int) time.Duration { return time.Duration(x) * time.Millisecond }

	cases := []struct {
		name            string
		first, second   time.Duration
		firstN, secondN uint64
		want            bool
	}{
		{"steady", ms(20), ms(21), n, n, false},
		{"noise below the factor", ms(20), ms(28), n, n, false},
		// The case a rate/success check cannot see: the rate was held and
		// nothing errored, but the queue was still growing at the end.
		{"queue still growing", ms(20), ms(90), n, n, true},
		{"recovering", ms(90), ms(20), n, n, false},
		// Too few samples per half to read a trend; abstain rather than guess.
		{"sparse first half", ms(20), ms(90), 10, n, false},
		{"sparse second half", ms(20), ms(90), n, 10, false},
		{"no first-half latency", 0, ms(90), n, n, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := latencyDrifting(tc.first, tc.second, tc.firstN, tc.secondN)
			if got != tc.want {
				t.Errorf("latencyDrifting(%s, %s, %d, %d) = %v, want %v",
					tc.first, tc.second, tc.firstN, tc.secondN, got, tc.want)
			}
		})
	}
}
