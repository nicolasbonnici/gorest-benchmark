package benchmark

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"

	"github.com/gofiber/fiber/v3"
	"github.com/nicolasbonnici/gorest-codegen/codegen"
	"github.com/nicolasbonnici/gorest/database"
	"github.com/nicolasbonnici/gorest/plugin"
	"github.com/nicolasbonnici/gorest/pluginloader"
)

func init() {
	pluginloader.RegisterPluginFactory("benchmark", NewPlugin)
}

// BenchmarkPlugin provides API performance benchmarking
type BenchmarkPlugin struct {
	db database.Database
}

func NewPlugin() plugin.Plugin {
	return &BenchmarkPlugin{}
}

func (p *BenchmarkPlugin) Name() string {
	return "benchmark"
}

func (p *BenchmarkPlugin) Initialize(cfg map[string]interface{}) error {
	if db, ok := cfg["database"].(database.Database); ok {
		p.db = db
	}
	return nil
}

// Handler returns a no-op middleware since benchmark is a CLI tool
func (p *BenchmarkPlugin) Handler() fiber.Handler {
	return func(c fiber.Ctx) error {
		return c.Next()
	}
}

// Commands implements the CommandProvider interface
func (p *BenchmarkPlugin) Commands() []plugin.Command {
	return []plugin.Command{
		&BenchmarkCommand{plugin: p},
	}
}

// BenchmarkCommand runs API performance benchmarks
type BenchmarkCommand struct {
	plugin *BenchmarkPlugin
}

func (c *BenchmarkCommand) Name() string {
	return "benchmark"
}

func (c *BenchmarkCommand) Description() string {
	return "Run API performance benchmarks with different load levels"
}

func (c *BenchmarkCommand) Run(ctx *plugin.CommandContext) *plugin.CommandResult {
	db := c.plugin.db
	if db == nil {
		return &plugin.CommandResult{
			Success: false,
			Error:   fmt.Errorf("database not configured"),
			Message: "Database connection required for benchmarks",
		}
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return &plugin.CommandResult{
			Success: false,
			Error:   fmt.Errorf("DATABASE_URL not set"),
			Message: "DATABASE_URL environment variable required",
		}
	}

	// Read before any setup work: a typo in BENCH_RATES should fail in the first
	// second, not after the seeding and codegen that precede the first request.
	loadCfg, err := loadConfigFromEnv()
	if err != nil {
		return &plugin.CommandResult{
			Success: false,
			Error:   err,
			Message: "Invalid benchmark load configuration",
		}
	}

	started := time.Now()

	// Setup benchmark table
	if result := c.setupBenchmarkTable(ctx, db); result != nil {
		return result
	}

	// Generate test data
	counts := []int{10, 100, 1000}
	if result := c.generateTestData(ctx, db, counts); result != nil {
		return result
	}

	// Generate models and resources
	c.generateModelsAndResources(ctx)

	// Build and start server
	serverCmd, result := c.buildAndStartServer(ctx, dbURL)
	if result != nil {
		return result
	}
	defer func() {
		_ = serverCmd.Process.Kill()
		_ = serverCmd.Wait()
	}()

	// Wait for server and verify endpoint
	if result := c.waitForServerReady(ctx); result != nil {
		return result
	}
	c.verifyEndpoint()

	// Print environment info just before results
	printHeader(started)

	// Run benchmarks
	c.runBenchmarkTests(counts, loadCfg)

	// Cleanup
	c.cleanup(ctx, db)

	elapsed := time.Since(started).Round(time.Millisecond)
	fmt.Printf("\n  Completed in %s\n", elapsed)
	printDivider()
	fmt.Println()

	return &plugin.CommandResult{
		Success: true,
		Message: "Benchmark completed successfully",
	}
}

func (c *BenchmarkCommand) setupBenchmarkTable(ctx *plugin.CommandContext, db database.Database) *plugin.CommandResult {
	if ctx.ProgressCallback != nil {
		ctx.ProgressCallback("Setting up benchmark table...")
	}

	dbCtx := context.Background()
	_, _ = db.Exec(dbCtx, "DROP TABLE IF EXISTS benchmark_items CASCADE")
	_, err := db.Exec(dbCtx, `
		CREATE TABLE benchmark_items (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name TEXT,
			value INTEGER,
			description TEXT,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`)
	if err != nil {
		return &plugin.CommandResult{
			Success: false,
			Error:   err,
			Message: "Failed to create benchmark table",
		}
	}
	return nil
}

func (c *BenchmarkCommand) generateTestData(ctx *plugin.CommandContext, db database.Database, counts []int) *plugin.CommandResult {
	if ctx.ProgressCallback != nil {
		ctx.ProgressCallback("Generating test data...")
	}

	dbCtx := context.Background()
	maxCount := counts[len(counts)-1]

	for i := 1; i <= maxCount; i++ {
		_, err := db.Exec(dbCtx,
			"INSERT INTO benchmark_items (name, value, description) VALUES ($1, $2, $3)",
			fmt.Sprintf("Item %d", i),
			i,
			fmt.Sprintf("Description for item %d with some additional text to make it realistic", i),
		)
		if err != nil {
			return &plugin.CommandResult{
				Success: false,
				Error:   err,
				Message: "Failed to insert test data",
			}
		}
	}
	return nil
}

func (c *BenchmarkCommand) generateModelsAndResources(ctx *plugin.CommandContext) {
	if ctx.ProgressCallback != nil {
		ctx.ProgressCallback("Generating models and resources...")
	}

	tables := codegen.LoadSchema(c.plugin.db)
	codegen.GenerateStructs(tables)

	authCfg := codegen.DefaultAuthConfig()
	codegen.GenerateAPI(authCfg)
}

func (c *BenchmarkCommand) buildAndStartServer(ctx *plugin.CommandContext, dbURL string) (*exec.Cmd, *plugin.CommandResult) {
	if ctx.ProgressCallback != nil {
		ctx.ProgressCallback("Building API server...")
	}

	cmd := exec.Command("go", "build", "-o", "./bin/benchmark-server", "./testserver/main.go")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return nil, &plugin.CommandResult{
			Success: false,
			Error:   err,
			Message: "Failed to build benchmark server",
		}
	}

	if ctx.ProgressCallback != nil {
		ctx.ProgressCallback("Starting API server...")
	}

	serverCmd := exec.Command("./bin/benchmark-server")
	serverCmd.Env = append(os.Environ(),
		"DATABASE_URL="+dbURL,
		"PORT=3001",
		"JWT_SECRET=bmk-jwt-k3y-f0r-4p1-p3rf0rm4nc3-m34sur3m3nts-0nly",
		"JWT_TTL=3600",
		"PAGINATION_LIMIT=50",
		"PAGINATION_MAX_LIMIT=10000",
		"CORS_ORIGINS=*",
		"ENVIRONMENT=test",
	)
	serverCmd.Stdout = nil
	serverCmd.Stderr = nil

	if err := serverCmd.Start(); err != nil {
		return nil, &plugin.CommandResult{
			Success: false,
			Error:   err,
			Message: "Failed to start server",
		}
	}

	return serverCmd, nil
}

func (c *BenchmarkCommand) waitForServerReady(ctx *plugin.CommandContext) *plugin.CommandResult {
	if ctx.ProgressCallback != nil {
		ctx.ProgressCallback("Waiting for server to be ready...")
	}

	serverReady := false
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)

		target := vegeta.Target{
			Method: "GET",
			URL:    "http://localhost:3001/status",
		}
		attacker := vegeta.NewAttacker()

		for res := range attacker.Attack(vegeta.NewStaticTargeter(target), vegeta.Rate{Freq: 1, Per: time.Second}, 1*time.Second, "Status Check") {
			if res.Code == 200 {
				serverReady = true
				break
			}
		}

		if serverReady {
			break
		}
	}

	if !serverReady {
		return &plugin.CommandResult{
			Success: false,
			Error:   fmt.Errorf("server startup timeout"),
			Message: "Server failed to start within 15 seconds",
		}
	}

	time.Sleep(2 * time.Second)
	return nil
}

func (c *BenchmarkCommand) verifyEndpoint() {
	target := vegeta.Target{
		Method: "GET",
		URL:    "http://localhost:3001/benchmarkitems?limit=1",
	}
	attacker := vegeta.NewAttacker()
	resChan := attacker.Attack(vegeta.NewStaticTargeter(target), vegeta.Rate{Freq: 1, Per: time.Second}, 1*time.Second, "Endpoint Check")
	if res := <-resChan; res.Code != 200 {
		fmt.Printf("  WARNING: endpoint check returned HTTP %d\n\n", res.Code)
		time.Sleep(2 * time.Second)
	}
	for range resChan {
	}
}

const benchmarkBaseURL = "http://localhost:3001"

// queryScenario isolates a distinct query shape the list endpoint executes for
// a given page size. Splitting them lets the v0.6 query-path work in the other
// plugins be attributed rather than averaged together: "list" fetches and
// serialises rows only (count=false), while "list+count" adds the pagination
// COUNT(*) that GoREST runs as a separate second query.
type queryScenario struct {
	label string
	query func(limit int) string
}

func benchmarkScenarios() []queryScenario {
	return []queryScenario{
		{label: "list", query: func(limit int) string {
			return fmt.Sprintf("limit=%d&count=false", limit)
		}},
		{label: "list+count", query: func(limit int) string {
			return fmt.Sprintf("limit=%d", limit)
		}},
	}
}

func benchmarkURL(s queryScenario, limit int) string {
	return benchmarkBaseURL + "/benchmarkitems?" + s.query(limit)
}

func (c *BenchmarkCommand) runBenchmarkTests(counts []int, cfg loadConfig) {
	seeded := counts[len(counts)-1]

	for _, scenario := range benchmarkScenarios() {
		for _, limit := range counts {
			fmt.Printf("\n  GET /benchmarkitems  [%s]  limit=%-4d   (%d rows seeded)\n", scenario.label, limit, seeded)

			url := benchmarkURL(scenario, limit)
			key := cellKey(scenario.label, limit)

			capacity := c.runCapacitySweep(url, cfg)

			// A pinned ladder is what makes an A/B comparable: the candidate has
			// to answer the same arrival process as the baseline, not one scaled
			// to its own capacity, or every row compares two different questions.
			rates, pinned := cfg.pinned[key]
			if !pinned {
				rates = ladderFor(capacity)
			}
			c.runRateLadder(url, key, rates, pinned, cfg)
		}
	}
}

// runCapacitySweep drives the endpoint closed-loop: `concurrency` workers each
// issue the next request as soon as the previous returns (unlimited rate), so the
// server is actually saturated and metrics.Rate reports the *measured* throughput
// rather than a preset request rate. Peak throughput across the sweep is both the
// capacity number worth diffing and the scale the open-loop ladder is built on.
//
// The latency columns here are for reading, not for diffing: see the note at the
// top of loadgen.go.
//
// Backported from the v0.6 line so that a v0.5 baseline and a v0.6 candidate are
// measured the same way.
func (c *BenchmarkCommand) runCapacitySweep(url string, cfg loadConfig) float64 {
	fmt.Printf("\n  capacity (closed-loop, %s per level)\n", cfg.capacityDuration)
	fmt.Printf("  %-12s  %-10s  %-10s  %-10s  %-10s  %-10s  %s\n",
		"concurrency", "rps", "p50", "p95", "p99", "requests", "success")
	fmt.Printf("  %s\n", strings.Repeat("─", 78))

	var capacity float64
	var peakAt int
	for _, concurrency := range cfg.concurrencyLevels {
		target := vegeta.Target{Method: "GET", URL: url}
		// Freq 0 = send as fast as the bounded worker pool allows.
		attacker := vegeta.NewAttacker(
			vegeta.Workers(uint64(concurrency)),
			vegeta.MaxWorkers(uint64(concurrency)),
			vegeta.Timeout(cfg.requestTimeout),
		)

		var metrics vegeta.Metrics
		for res := range attacker.Attack(vegeta.NewStaticTargeter(target), vegeta.Rate{Freq: 0}, cfg.capacityDuration, "") {
			metrics.Add(res)
		}
		metrics.Close()

		fmt.Printf("  %-12d  %-10.0f  %-10s  %-10s  %-10s  %-10d  %.2f%%\n",
			concurrency,
			metrics.Rate,
			fmtDuration(metrics.Latencies.P50),
			fmtDuration(metrics.Latencies.P95),
			fmtDuration(metrics.Latencies.P99),
			metrics.Requests,
			metrics.Success*100,
		)

		if metrics.Rate > capacity {
			capacity = metrics.Rate
			peakAt = concurrency
		}
	}

	fmt.Printf("  capacity %.0f rps at concurrency %d\n", capacity, peakAt)
	return capacity
}

// runRateLadder drives the endpoint open-loop: requests leave on a fixed
// schedule and workers are spawned as needed to hold it, so a slow response
// delays no subsequent request. Latency measured this way is a property of the
// build at a stated offered load, which is what makes it comparable across an
// A/B; see the note at the top of loadgen.go.
func (c *BenchmarkCommand) runRateLadder(url, key string, rates []int, pinned bool, cfg loadConfig) {
	origin := "ladder from measured capacity"
	if pinned {
		origin = "ladder pinned via BENCH_RATES"
	}
	fmt.Printf("\n  latency at fixed rate (open-loop, %s)\n", origin)
	fmt.Printf("  %-12s  %-10s  %-10s  %-10s  %-10s  %-10s  %-10s  %s\n",
		"rate", "achieved", "p50", "p95", "p99", "requests", "success", "")
	fmt.Printf("  %s\n", strings.Repeat("─", 78))

	for _, rate := range rates {
		duration := durationFor(rate, cfg.rateDuration, cfg.maxRateDuration, minSamples)
		target := vegeta.Target{Method: "GET", URL: url}
		attacker := vegeta.NewAttacker(
			vegeta.MaxWorkers(maxWorkersFor(rate, cfg.requestTimeout)),
			vegeta.Timeout(cfg.requestTimeout),
		)

		// Split by arrival time as well as aggregating: comparing the two halves
		// is what exposes a queue that was still growing when the rung ended,
		// which the aggregate numbers hide completely.
		var metrics, firstHalf, secondHalf vegeta.Metrics
		pacer := vegeta.Rate{Freq: rate, Per: time.Second}
		results := attacker.Attack(vegeta.NewStaticTargeter(target), pacer, duration, "")

		var midpoint time.Time
		for res := range results {
			if midpoint.IsZero() {
				midpoint = res.Timestamp.Add(duration / 2)
			}
			metrics.Add(res)
			if res.Timestamp.Before(midpoint) {
				firstHalf.Add(res)
			} else {
				secondHalf.Add(res)
			}
		}
		metrics.Close()
		firstHalf.Close()
		secondHalf.Close()

		flag := ""
		if saturated(rate, metrics.Rate, metrics.Success) ||
			latencyDrifting(firstHalf.Latencies.P50, secondHalf.Latencies.P50,
				firstHalf.Requests, secondHalf.Requests) {
			flag = "SATURATED"
		}
		fmt.Println(formatRateRow(rate, metrics.Rate,
			metrics.Latencies.P50, metrics.Latencies.P95, metrics.Latencies.P99,
			metrics.Requests, metrics.Success, flag))
	}

	// Emitted in the env format on purpose: scripts/bench-ab.sh scrapes these
	// lines off the baseline transcript and hands them to the candidate half.
	fmt.Printf("  BENCH_RATES %s\n", formatPinnedRates(key, rates))
}

func (c *BenchmarkCommand) cleanup(ctx *plugin.CommandContext, db database.Database) {
	if ctx.ProgressCallback != nil {
		ctx.ProgressCallback("Cleaning up...")
	}

	dbCtx := context.Background()
	_, _ = db.Exec(dbCtx, "DROP TABLE IF EXISTS benchmark_items CASCADE")

	if ctx.ProgressCallback != nil {
		ctx.ProgressCallback("Restoring original schema...")
	}

	cmd := exec.Command("make", "test-schema")
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()

	cmd = exec.Command("make", "test-generate")
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()
}

// ── helpers ──────────────────────────────────────────────────────────────────

func printHeader(started time.Time) {
	printDivider()
	fmt.Println()
	fmt.Println("  GoREST API Performance Benchmark")
	fmt.Println()
	fmt.Printf("  goos:      %s\n", runtime.GOOS)
	fmt.Printf("  goarch:    %s\n", runtime.GOARCH)
	fmt.Printf("  cpu:       %s\n", cpuModel())
	memTotal, memAvail := memInfo()
	fmt.Printf("  ram:       %s total, %s available\n", fmtBytes(memTotal), fmtBytes(memAvail))
	fmt.Printf("  load avg:  %s\n", loadAvg())
	fmt.Printf("  date:      %s\n", started.Format("2006-01-02 15:04:05"))
	fmt.Println()
	printDivider()
}

func printDivider() {
	fmt.Println("  " + strings.Repeat("─", 66))
}

// fmtDuration rounds to 3 significant digits and picks the right unit.
func fmtDuration(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.2fµs", float64(d.Nanoseconds())/1e3)
	case d < time.Second:
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1e6)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

// memInfo returns total and available RAM in bytes from /proc/meminfo.
func memInfo() (total, avail uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		var kb uint64
		if strings.HasPrefix(line, "MemTotal:") {
			if _, err := fmt.Sscanf(strings.TrimPrefix(line, "MemTotal:"), "%d", &kb); err == nil {
				total = kb * 1024
			}
		} else if strings.HasPrefix(line, "MemAvailable:") {
			if _, err := fmt.Sscanf(strings.TrimPrefix(line, "MemAvailable:"), "%d", &kb); err == nil {
				avail = kb * 1024
			}
		}
		if total > 0 && avail > 0 {
			break
		}
	}
	return total, avail
}

// loadAvg returns the 1/5/15-minute load averages from /proc/loadavg.
func loadAvg() string {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "n/a"
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return "n/a"
	}
	return fmt.Sprintf("%s  %s  %s  (1m  5m  15m)", fields[0], fields[1], fields[2])
}

// fmtBytes formats a byte count as GiB or MiB.
func fmtBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// cpuModel reads the CPU brand string from the OS.
func cpuModel() string {
	if runtime.GOOS == "linux" {
		if f, err := os.Open("/proc/cpuinfo"); err == nil {
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				line := sc.Text()
				if strings.HasPrefix(line, "model name") {
					if _, after, ok := strings.Cut(line, ":"); ok {
						return strings.TrimSpace(after)
					}
				}
			}
		}
	}
	return runtime.GOARCH
}
