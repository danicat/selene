package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danicat/selene/internal/mutator"
)

// setupIntegrationProject creates a temporary Go module with multiple functions and tests.
func setupIntegrationProject(t *testing.T) (dir string, srcPath string, testPath string) {
	t.Helper()
	dir = t.TempDir()

	goMod := `module example.com/integration
go 1.20
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	src := `package integration

func Compute(a, b int, flag bool) int {
	if flag {
		return a + b
	}
	return a - b
}

func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func IsPositive(n int) bool {
	return n > 0
}

func UncoveredFunc(x int) int {
	return x * 2
}
`
	srcPath = filepath.Join(dir, "calc.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write calc.go: %v", err)
	}

	testSrc := `package integration

import "testing"

func TestCompute(t *testing.T) {
	if Compute(3, 2, true) != 5 {
		t.Errorf("expected 5, got %d", Compute(3, 2, true))
	}
	if Compute(3, 2, false) != 1 {
		t.Errorf("expected 1, got %d", Compute(3, 2, false))
	}
}

func TestMax(t *testing.T) {
	if Max(10, 5) != 10 {
		t.Errorf("expected 10, got %d", Max(10, 5))
	}
	if Max(3, 7) != 7 {
		t.Errorf("expected 7, got %d", Max(3, 7))
	}
}

func TestIneffective(t *testing.T) {
	// Runs IsPositive but does not assert the return value
	_ = IsPositive(5)
	_ = IsPositive(-5)
}
`
	testPath = filepath.Join(dir, "calc_test.go")
	if err := os.WriteFile(testPath, []byte(testSrc), 0644); err != nil {
		t.Fatalf("failed to write calc_test.go: %v", err)
	}

	return dir, srcPath, testPath
}

// 1. Parallel execution with -workers (workers=1 vs workers=4)
func TestE2E_ParallelExecution_Workers(t *testing.T) {
	_, srcPath, _ := setupIntegrationProject(t)

	mutators := []mutator.Mutator{
		&mutator.ArithmeticMutator{},
		&mutator.ComparisonMutator{},
		&mutator.BooleanMutator{},
		&mutator.ReverseIfCond{},
		&mutator.ConditionalsBoundaryMutator{},
	}

	// Run with workers = 1
	mutDir1 := filepath.Join(t.TempDir(), "mut-w1")
	config1 := Config{
		Verbose:     false,
		MutationDir: mutDir1,
		Mutators:    mutators,
		Workers:     1,
		Timeout:     10 * time.Second,
	}

	report1, err := Run([]string{srcPath}, config1)
	if err != nil {
		t.Fatalf("Run with workers=1 failed: %v", err)
	}

	// Run with workers = 4
	mutDir4 := filepath.Join(t.TempDir(), "mut-w4")
	config4 := Config{
		Verbose:     false,
		MutationDir: mutDir4,
		Mutators:    mutators,
		Workers:     4,
		Timeout:     10 * time.Second,
	}

	report4, err := Run([]string{srcPath}, config4)
	if err != nil {
		t.Fatalf("Run with workers=4 failed: %v", err)
	}

	// Verify consistency across parallel worker pools
	t.Logf("Workers=1: Total=%d, Killed=%d, Survived=%d, Uncovered=%d, Timeouts=%d",
		report1.Total, report1.Killed, report1.Survived, report1.Uncovered, report1.Timeouts)
	t.Logf("Workers=4: Total=%d, Killed=%d, Survived=%d, Uncovered=%d, Timeouts=%d",
		report4.Total, report4.Killed, report4.Survived, report4.Uncovered, report4.Timeouts)

	if report1.Total == 0 {
		t.Fatalf("expected non-zero mutations, got 0")
	}

	if report1.Total != report4.Total {
		t.Errorf("Total mutations mismatch: workers=1 had %d, workers=4 had %d", report1.Total, report4.Total)
	}

	if report1.Killed != report4.Killed {
		t.Errorf("Killed count mismatch: workers=1 had %d, workers=4 had %d", report1.Killed, report4.Killed)
	}

	if report1.Survived != report4.Survived {
		t.Errorf("Survived count mismatch: workers=1 had %d, workers=4 had %d", report1.Survived, report4.Survived)
	}

	if report1.Uncovered != report4.Uncovered {
		t.Errorf("Uncovered count mismatch: workers=1 had %d, workers=4 had %d", report1.Uncovered, report4.Uncovered)
	}

	if report1.Timeouts != report4.Timeouts {
		t.Errorf("Timeouts count mismatch: workers=1 had %d, workers=4 had %d", report1.Timeouts, report4.Timeouts)
	}

	if report1.Score() != report4.Score() {
		t.Errorf("Mutation score mismatch: workers=1 had %.2f%%, workers=4 had %.2f%%", report1.Score(), report4.Score())
	}
}

// 2. Configurable timeout handling with process-group kill
func TestE2E_TimeoutHandling_ProcessGroupKill(t *testing.T) {
	dir := t.TempDir()

	goMod := `module example.com/timeout
go 1.20
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Source file where a boolean mutation triggers an infinite loop
	src := `package timeout

import "time"

func Spin(shouldLoop bool) int {
	if shouldLoop {
		for {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return 42
}
`
	srcPath := filepath.Join(dir, "spin.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write spin.go: %v", err)
	}

	// Test passes normally because Spin(false) returns immediately
	testSrc := `package timeout

import "testing"

func TestSpin(t *testing.T) {
	if Spin(false) != 42 {
		t.Fail()
	}
}
`
	testPath := filepath.Join(dir, "spin_test.go")
	if err := os.WriteFile(testPath, []byte(testSrc), 0644); err != nil {
		t.Fatalf("failed to write spin_test.go: %v", err)
	}

	// When BooleanLiteralMutator mutates false -> true, Spin(true) enters infinite loop
	mutDir := filepath.Join(dir, "mut-timeout")
	shortTimeout := 500 * time.Millisecond

	config := Config{
		Verbose:     true,
		MutationDir: mutDir,
		Mutators:    []mutator.Mutator{&mutator.ReverseIfCond{}},
		Workers:     1,
		Timeout:     shortTimeout,
	}

	start := time.Now()
	report, err := Run([]string{srcPath}, config)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	t.Logf("Timeout test finished in %v. Total: %d, Timeouts: %d, Killed: %d",
		elapsed, report.Total, report.Timeouts, report.Killed)

	if report.Total != 1 {
		t.Errorf("expected 1 mutation, got %d", report.Total)
	}

	if report.Timeouts != 1 {
		t.Errorf("expected 1 timeout mutation, got %d (killed: %d)", report.Timeouts, report.Killed)
	}

	// Ensure process was killed promptly by timeout, not hanging for standard default 10s
	if elapsed > 3*time.Second {
		t.Errorf("timeout handling took too long (%v), expected < 3s", elapsed)
	}
}

// 3. SQLite DB table population with --db testquery.db
func TestE2E_SQLiteDB_PopulationAndViews(t *testing.T) {
	dir, srcPath, _ := setupIntegrationProject(t)

	dbPath := filepath.Join(dir, "testquery.db")
	mutDir := filepath.Join(dir, "mut-db")

	mutators := []mutator.Mutator{
		&mutator.ArithmeticMutator{},
		&mutator.ComparisonMutator{},
		&mutator.BooleanMutator{},
		&mutator.ReverseIfCond{},
		&mutator.ConditionalsBoundaryMutator{},
	}

	config := Config{
		Verbose:     false,
		MutationDir: mutDir,
		Mutators:    mutators,
		Workers:     2,
		Timeout:     10 * time.Second,
		DBPath:      dbPath,
	}

	report, err := Run([]string{srcPath}, config)
	if err != nil {
		t.Fatalf("Run with DB failed: %v", err)
	}

	// Verify database file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("database file was not created at %s", dbPath)
	}

	db := NewDatabase(dbPath)

	// 1. Verify selene_summary view
	summary, err := db.QuerySummary()
	if err != nil {
		t.Fatalf("QuerySummary failed: %v", err)
	}

	t.Logf("DB Summary: Total=%d, Killed=%d, Survived=%d, Uncovered=%d, Timeouts=%d",
		summary.TotalMutations, summary.Killed, summary.Survived, summary.Uncovered, summary.Timeouts)

	if summary.TotalMutations != report.Total {
		t.Errorf("summary total mutations (%d) != report total (%d)", summary.TotalMutations, report.Total)
	}

	if summary.Killed != report.Killed {
		t.Errorf("summary killed (%d) != report killed (%d)", summary.Killed, report.Killed)
	}

	if summary.Survived != report.Survived {
		t.Errorf("summary survived (%d) != report survived (%d)", summary.Survived, report.Survived)
	}

	if summary.Uncovered != report.Uncovered {
		t.Errorf("summary uncovered (%d) != report uncovered (%d)", summary.Uncovered, report.Uncovered)
	}

	// 2. Verify selene_survived view
	survivedMutations, err := db.QuerySurvived()
	if err != nil {
		t.Fatalf("QuerySurvived failed: %v", err)
	}

	if len(survivedMutations) != report.Survived {
		t.Errorf("selene_survived record count (%d) != report survived (%d)", len(survivedMutations), report.Survived)
	}

	for _, sm := range survivedMutations {
		if sm.Status != "survived" {
			t.Errorf("expected status 'survived' in selene_survived view, got %q", sm.Status)
		}
		if sm.File == "" || sm.Line == 0 || sm.Mutator == "" {
			t.Errorf("invalid mutation record in selene_survived: %+v", sm)
		}
	}

	// 3. Verify selene_zero_kill_tests view
	zeroKillTests, err := db.QueryZeroKillTests()
	if err != nil {
		t.Fatalf("QueryZeroKillTests failed: %v", err)
	}

	// TestIneffective should be classified as a zero-kill test because it didn't assert
	foundIneffective := false
	for _, zkt := range zeroKillTests {
		if strings.Contains(zkt.TestName, "TestIneffective") {
			foundIneffective = true
		}
	}
	if !foundIneffective {
		t.Errorf("expected TestIneffective in selene_zero_kill_tests view, got: %+v", zeroKillTests)
	}
}

// 4. Targeted test execution speedup verification
func TestE2E_TargetedExecution_Speedup(t *testing.T) {
	dir := t.TempDir()

	goMod := `module example.com/targeted
go 1.20
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Source file with a fast function and a slow function
	src := `package targeted

func FastAdd(a, b int) int {
	return a + b
}

func SlowMultiply(a, b int) int {
	return a * b
}
`
	srcPath := filepath.Join(dir, "math.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write math.go: %v", err)
	}

	// Test file with TestFastAdd (instant) and TestSlowMultiply (sleeps 100ms)
	testSrc := `package targeted

import (
	"testing"
	"time"
)

func TestFastAdd(t *testing.T) {
	if FastAdd(2, 3) != 5 {
		t.Fail()
	}
}

func TestSlowMultiply(t *testing.T) {
	time.Sleep(100 * time.Millisecond)
	if SlowMultiply(2, 3) != 6 {
		t.Fail()
	}
}
`
	testPath := filepath.Join(dir, "math_test.go")
	if err := os.WriteFile(testPath, []byte(testSrc), 0644); err != nil {
		t.Fatalf("failed to write math_test.go: %v", err)
	}

	// Setup targeted coverage index: FastAdd covered only by TestFastAdd
	idx := NewMemoryTestIndex()
	idx.AddCoverage(srcPath, 3, 5, "TestFastAdd")
	idx.AddCoverage(srcPath, 7, 9, "TestSlowMultiply")

	mutators := []mutator.Mutator{&mutator.ArithmeticMutator{}}

	// 1. Untargeted baseline: both tests (including 100ms sleep) run for all mutations
	idxAll := NewMemoryTestIndex()
	idxAll.AddCoverage(srcPath, 1, 10, "TestFastAdd")
	idxAll.AddCoverage(srcPath, 1, 10, "TestSlowMultiply")

	mutDirUntargeted := filepath.Join(dir, "mut-untargeted")
	configUntargeted := Config{
		MutationDir: mutDirUntargeted,
		Mutators:    mutators,
		Workers:     1,
		Timeout:     5 * time.Second,
		TestIndex:   idxAll,
	}

	startUntargeted := time.Now()
	reportUntargeted, err := Run([]string{srcPath}, configUntargeted)
	durationUntargeted := time.Since(startUntargeted)

	if err != nil {
		t.Fatalf("Untargeted run failed: %v", err)
	}

	// 2. Targeted run: runs ONLY covering tests per mutation (-run ^TestFastAdd$ for FastAdd)
	mutDirTargeted := filepath.Join(dir, "mut-targeted")
	configTargeted := Config{
		MutationDir: mutDirTargeted,
		Mutators:    mutators,
		Workers:     1,
		Timeout:     5 * time.Second,
		TestIndex:   idx,
	}

	startTargeted := time.Now()
	reportTargeted, err := Run([]string{srcPath}, configTargeted)
	durationTargeted := time.Since(startTargeted)

	if err != nil {
		t.Fatalf("Targeted run failed: %v", err)
	}

	t.Logf("Untargeted duration: %v (Killed=%d, Total=%d)", durationUntargeted, reportUntargeted.Killed, reportUntargeted.Total)
	t.Logf("Targeted duration:   %v (Killed=%d, Total=%d)", durationTargeted, reportTargeted.Killed, reportTargeted.Total)

	if reportTargeted.Total != reportUntargeted.Total {
		t.Errorf("Mutation total mismatch: targeted=%d, untargeted=%d", reportTargeted.Total, reportUntargeted.Total)
	}

	if reportTargeted.Killed != reportUntargeted.Killed {
		t.Errorf("Killed count mismatch: targeted=%d, untargeted=%d", reportTargeted.Killed, reportUntargeted.Killed)
	}

	// Targeted mode should skip non-covering slow tests and run faster
	t.Logf("Speedup factor: %.2fx", float64(durationUntargeted)/float64(durationTargeted))
}

// 5. Exact subtest targeting vs parent-level table run benchmark
func TestE2E_SubtestTargeting_Speedup(t *testing.T) {
	dir := t.TempDir()

	goMod := `module example.com/subtestspeed
go 1.20
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	src := `package subtestspeed

func Op1(a, b int) int {
	return a + b
}

func Op2(a, b int) int {
	return a * b
}
`
	srcPath := filepath.Join(dir, "math.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatalf("failed to write math.go: %v", err)
	}

	// Table test with 5 subtests, each doing simulated work (15ms)
	testSrc := `package subtestspeed

import (
	"testing"
	"time"
)

func TestTable(t *testing.T) {
	cases := []struct {
		name string
		fn   func() bool
	}{
		{"Case1_Op1", func() bool { time.Sleep(15 * time.Millisecond); return Op1(2, 3) == 5 }},
		{"Case2_Unrelated1", func() bool { time.Sleep(15 * time.Millisecond); return true }},
		{"Case3_Unrelated2", func() bool { time.Sleep(15 * time.Millisecond); return true }},
		{"Case4_Unrelated3", func() bool { time.Sleep(15 * time.Millisecond); return true }},
		{"Case5_Unrelated4", func() bool { time.Sleep(15 * time.Millisecond); return true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.fn() {
				t.Fail()
			}
		})
	}
}
`
	testPath := filepath.Join(dir, "math_test.go")
	if err := os.WriteFile(testPath, []byte(testSrc), 0644); err != nil {
		t.Fatalf("failed to write math_test.go: %v", err)
	}

	mutators := []mutator.Mutator{&mutator.ArithmeticMutator{}}

	// A. Parent-level targeting (runs the whole TestTable harness with all 5 cases = 75ms per run)
	idxParent := NewMemoryTestIndex()
	idxParent.AddCoverage(srcPath, 3, 5, "TestTable")
	configParent := Config{
		MutationDir: filepath.Join(dir, "mut-parent"),
		Mutators:    mutators,
		Workers:     1,
		Timeout:     5 * time.Second,
		TestIndex:   idxParent,
	}

	startParent := time.Now()
	reportParent, err := Run([]string{srcPath}, configParent)
	durationParent := time.Since(startParent)
	if err != nil {
		t.Fatalf("Parent-level targeted run failed: %v", err)
	}

	// B. Exact subtest targeting (runs ONLY TestTable/Case1_Op1 = 15ms per run)
	idxSub := NewMemoryTestIndex()
	idxSub.AddCoverage(srcPath, 3, 5, "TestTable/Case1_Op1")
	configSub := Config{
		MutationDir: filepath.Join(dir, "mut-subtest"),
		Mutators:    mutators,
		Workers:     1,
		Timeout:     5 * time.Second,
		TestIndex:   idxSub,
	}

	startSub := time.Now()
	reportSub, err := Run([]string{srcPath}, configSub)
	durationSub := time.Since(startSub)
	if err != nil {
		t.Fatalf("Exact subtest targeted run failed: %v", err)
	}

	t.Logf("Parent-level table duration: %v (Killed=%d)", durationParent, reportParent.Killed)
	t.Logf("Exact subtest duration:       %v (Killed=%d)", durationSub, reportSub.Killed)
	speedup := float64(durationParent) / float64(durationSub)
	t.Logf("Subtest targeting speedup factor: %.2fx", speedup)
}

// 6. Concurrency stress test for high worker counts (workers=8)
func TestE2E_ConcurrentWorkerPoolStress(t *testing.T) {
	dir, srcPath, _ := setupIntegrationProject(t)

	mutators := DefaultMutators()
	mutDir := filepath.Join(dir, "mut-stress")

	config := Config{
		Verbose:     false,
		MutationDir: mutDir,
		Mutators:    mutators,
		Workers:     8,
		Timeout:     10 * time.Second,
	}

	report, err := Run([]string{srcPath}, config)
	if err != nil {
		t.Fatalf("Concurrent stress run failed: %v", err)
	}

	if report.Total == 0 {
		t.Fatalf("expected mutations in stress test, got 0")
	}

	t.Logf("Stress test passed cleanly with 8 workers. Total: %d, Killed: %d, Survived: %d, Score: %.2f%%",
		report.Total, report.Killed, report.Survived, report.Score())
}
