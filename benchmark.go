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

	started := time.Now()
	printHeader()

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

	// Run benchmarks
	c.runBenchmarkTests(counts)

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

func (c *BenchmarkCommand) runBenchmarkTests(counts []int) {
	concurrencyLevels := []int{1, 10, 50}
	testDuration := 5 * time.Second

	for _, limit := range counts {
		fmt.Printf("\n  GET /benchmarkitems?limit=%-4d   (%d rows seeded)\n", limit, counts[len(counts)-1])
		fmt.Printf("  %-12s  %-8s  %-10s  %-10s  %-10s  %s\n",
			"concurrency", "rps", "p50", "p95", "p99", "success")
		fmt.Printf("  %s\n", strings.Repeat("─", 62))

		for _, concurrency := range concurrencyLevels {
			c.runSingleBenchmark(limit, concurrency, testDuration)
		}
	}
}

func (c *BenchmarkCommand) runSingleBenchmark(limit, concurrency int, testDuration time.Duration) {
	url := fmt.Sprintf("http://localhost:3001/benchmarkitems?limit=%d", limit)

	target := vegeta.Target{Method: "GET", URL: url}
	rate := vegeta.Rate{Freq: concurrency, Per: time.Second}
	attacker := vegeta.NewAttacker()

	var metrics vegeta.Metrics
	for res := range attacker.Attack(vegeta.NewStaticTargeter(target), rate, testDuration, "") {
		metrics.Add(res)
	}
	metrics.Close()

	successPct := metrics.Success * 100
	fmt.Printf("  %-12d  %-8.0f  %-10s  %-10s  %-10s  %.2f%%\n",
		concurrency,
		metrics.Rate,
		fmtDuration(metrics.Latencies.P50),
		fmtDuration(metrics.Latencies.P95),
		fmtDuration(metrics.Latencies.P99),
		successPct,
	)
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

func printHeader() {
	printDivider()
	fmt.Println()
	fmt.Println("  GoREST API Performance Benchmark")
	fmt.Println()
	fmt.Printf("  goos:   %s\n", runtime.GOOS)
	fmt.Printf("  goarch: %s\n", runtime.GOARCH)
	fmt.Printf("  cpu:    %s\n", cpuModel())
	fmt.Printf("  date:   %s\n", time.Now().Format("2006-01-02 15:04:05"))
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
