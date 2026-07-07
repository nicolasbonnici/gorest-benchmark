package benchmark

import (
	"strings"
	"testing"
	"time"
)

func TestFmtDuration(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"zero", 0, "0ns"},
		{"sub-microsecond", 500 * time.Nanosecond, "500ns"},
		{"microseconds", 1500 * time.Nanosecond, "1.50µs"},
		{"milliseconds", 1500 * time.Microsecond, "1.50ms"},
		{"seconds", 2500 * time.Millisecond, "2.50s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmtDuration(tc.in); got != tc.want {
				t.Errorf("fmtDuration(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFmtBytes(t *testing.T) {
	cases := []struct {
		name string
		in   uint64
		want string
	}{
		{"bytes", 1023, "1023 B"},
		{"mebibytes", 512 << 20, "512 MiB"},
		{"gibibytes", 1 << 30, "1.0 GiB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmtBytes(tc.in); got != tc.want {
				t.Errorf("fmtBytes(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestBenchmarkScenarios guards the contract the v0.6 perf comparison relies on:
// the two shapes must differ only in whether the pagination COUNT(*) runs, so
// that "list" vs "list+count" attributes the count-query cost and nothing else.
func TestBenchmarkScenarios(t *testing.T) {
	scenarios := benchmarkScenarios()
	if len(scenarios) < 2 {
		t.Fatalf("expected at least list and list+count scenarios, got %d", len(scenarios))
	}

	seen := make(map[string]bool)
	var list, listCount *queryScenario
	for i := range scenarios {
		s := scenarios[i]
		if seen[s.label] {
			t.Errorf("duplicate scenario label %q", s.label)
		}
		seen[s.label] = true
		switch s.label {
		case "list":
			list = &scenarios[i]
		case "list+count":
			listCount = &scenarios[i]
		}
	}

	if list == nil || listCount == nil {
		t.Fatal("both list and list+count scenarios are required")
	}
	if q := list.query(100); !strings.Contains(q, "count=false") {
		t.Errorf("list scenario must disable the count query, got %q", q)
	}
	if q := listCount.query(100); strings.Contains(q, "count=false") {
		t.Errorf("list+count scenario must keep the default count query, got %q", q)
	}
}

func TestBenchmarkURL(t *testing.T) {
	s := queryScenario{label: "list", query: func(limit int) string {
		return "limit=" + itoa(limit) + "&count=false"
	}}
	got := benchmarkURL(s, 1000)
	want := "http://localhost:3001/benchmarkitems?limit=1000&count=false"
	if got != want {
		t.Errorf("benchmarkURL = %q, want %q", got, want)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
