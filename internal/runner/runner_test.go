package runner

import (
	"go/ast"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danicat/selene/internal/mutator"
)

// MockMutator always returns a single mutation that does nothing but count apply calls.
type MockMutator struct {
	NameVal string
}

func (m *MockMutator) Name() string { return m.NameVal }
func (m *MockMutator) Check(n ast.Node) []mutator.Mutation {
	return nil
}

// TestRun is an integration test for the Runner.
// It requires creating real files because `go test` and parsing rely on FS.
func TestRun(t *testing.T) {
	// Create a temp dir for the project
	tmpDir, err := os.MkdirTemp("", "selene-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a Go module

	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.20\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create a source file
	src := `package main
func Add(a, b int) int {
	return a + b
}
`
	srcPath := filepath.Join(tmpDir, "main.go")
	err = os.WriteFile(srcPath, []byte(src), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create a test file
	testSrc := `package main
import "testing"
func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fail()
	}
}
`
	testPath := filepath.Join(tmpDir, "main_test.go")
	err = os.WriteFile(testPath, []byte(testSrc), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create mutation output dir
	mutDir, err := os.MkdirTemp("", "selene-mut")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(mutDir) }()

	// Config with ArithmeticMutator (should flip + to -)

	config := Config{
		Verbose:     true,
		MutationDir: mutDir,
		Mutators:    []mutator.Mutator{&mutator.ArithmeticMutator{}},
	}

	// Run
	// We use ./... pattern and set the command to run in the tmpDir
	// To do this faithfully to how Run works, we just pass the file path.
	report, err := Run([]string{srcPath}, config)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if report.Total != 1 {
		t.Errorf("expected 1 mutation, got %d", report.Total)
	}
	if report.Killed+report.Timeouts != 1 {
		t.Errorf("expected 1 killed/timeout mutation, got %d", report.Killed+report.Timeouts)
	}
}

func TestReportScore(t *testing.T) {
	tests := []struct {
		report Report
		want   float64
	}{
		{Report{Total: 0, Killed: 0, Timeouts: 0, Survived: 0}, 0},
		{Report{Total: 10, Killed: 5, Timeouts: 0, Survived: 5}, 50.0},
		{Report{Total: 10, Killed: 3, Timeouts: 2, Survived: 5}, 50.0},
		{Report{Total: 4, Killed: 4, Timeouts: 0, Survived: 0}, 100.0},
	}
	for _, tt := range tests {
		if got := tt.report.Score(); got != tt.want {
			t.Errorf("%+v.Score() = %v, want %v", tt.report, got, tt.want)
		}
	}
}

func TestRunErrors(t *testing.T) {
	// Test parse error
	tmpDir := t.TempDir()
	invalidFile := filepath.Join(tmpDir, "invalid.go")
	if err := os.WriteFile(invalidFile, []byte("package main\nfunc {"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Run([]string{invalidFile}, Config{MutationDir: tmpDir})
	if err == nil {
		t.Error("expected error for invalid go file")
	}

	// Test absolute path error (empty filenames)
	_, err = Run([]string{}, Config{})
	if err == nil {
		t.Error("expected error for empty filenames")
	}
}

func TestParseGoTestOutput(t *testing.T) {

	input := []byte(`{"Time":"2023-10-26T10:00:00.000000Z","Action":"run","Package":"github.com/danicat/selene","Test":"TestExample"}
{"Time":"2023-10-26T10:00:00.100000Z","Action":"pass","Package":"github.com/danicat/selene","Test":"TestExample","Elapsed":0.1}
`)

	events, err := parseGoTestOutput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}

	if events[0].Action != "run" {
		t.Errorf("expected first event action to be 'run', got '%s'", events[0].Action)
	}

	if events[1].Action != "pass" {
		t.Errorf("expected second event action to be 'pass', got '%s'", events[1].Action)
	}

	expectedTime, _ := time.Parse(time.RFC3339, "2023-10-26T10:00:00.000000Z")
	if !events[0].Time.Equal(expectedTime) {
		t.Errorf("expected time %v, got %v", expectedTime, events[0].Time)
	}
}

func TestParseGoTestOutputError(t *testing.T) {
	_, err := parseGoTestOutput([]byte(`{"Action": "run"} invalid`))
	if err == nil {
		t.Error("expected error for invalid json")
	}
}

func TestRun_ParallelWorkers(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/parallel\n\ngo 1.20\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	src := `package parallel

func Add(a, b int) int {
	return a + b
}

func Sub(a, b int) int {
	return a - b
}

func Mul(a, b int) int {
	return a * b
}
`
	srcPath := filepath.Join(tmpDir, "math.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	testSrc := `package parallel

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fail()
	}
}

func TestSub(t *testing.T) {
	if Sub(5, 3) != 2 {
		t.Fail()
	}
}

func TestMul(t *testing.T) {
	if Mul(2, 3) != 6 {
		t.Fail()
	}
}
`
	testPath := filepath.Join(tmpDir, "math_test.go")
	if err := os.WriteFile(testPath, []byte(testSrc), 0644); err != nil {
		t.Fatal(err)
	}

	mutDir := filepath.Join(tmpDir, "mut")
	config := Config{
		MutationDir: mutDir,
		Mutators:    []mutator.Mutator{&mutator.ArithmeticMutator{}},
		Workers:     4,
		Timeout:     5 * time.Second,
	}

	report, err := Run([]string{srcPath}, config)
	if err != nil {
		t.Fatalf("Run with 4 workers failed: %v", err)
	}

	if report.Total != 3 {
		t.Errorf("expected 3 mutations, got %d", report.Total)
	}
	if report.Killed != 3 {
		t.Errorf("expected 3 killed mutations, got %d", report.Killed)
	}
}

func TestRun_TimeoutHandling(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/spin\n\ngo 1.20\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	src := `package spin

import "time"

func Spin(loop bool) int {
	if loop {
		for {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return 42
}
`
	srcPath := filepath.Join(tmpDir, "spin.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	testSrc := `package spin

import "testing"

func TestSpin(t *testing.T) {
	if Spin(false) != 42 {
		t.Fail()
	}
}
`
	testPath := filepath.Join(tmpDir, "spin_test.go")
	if err := os.WriteFile(testPath, []byte(testSrc), 0644); err != nil {
		t.Fatal(err)
	}

	mutDir := filepath.Join(tmpDir, "mut")
	config := Config{
		MutationDir: mutDir,
		Mutators:    []mutator.Mutator{&mutator.ReverseIfCond{}},
		Workers:     1,
		Timeout:     500 * time.Millisecond,
	}

	report, err := Run([]string{srcPath}, config)
	if err != nil {
		t.Fatalf("Run timeout test failed: %v", err)
	}

	if report.Timeouts != 1 {
		t.Errorf("expected 1 timeout mutation, got %d (killed: %d)", report.Timeouts, report.Killed)
	}
}

func TestRun_TargetedExecution(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/targeted\n\ngo 1.20\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	src := `package targeted

func FastAdd(a, b int) int {
	return a + b
}

func SlowMultiply(a, b int) int {
	return a * b
}
`
	srcPath := filepath.Join(tmpDir, "math.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	testSrc := `package targeted

import (
	"testing"
	"time"
)

func TestFastAdd(t *testing.T) {
	if FastAdd(1, 2) != 3 {
		t.Fail()
	}
}

func TestSlowMultiply(t *testing.T) {
	time.Sleep(50 * time.Millisecond)
	if SlowMultiply(2, 3) != 6 {
		t.Fail()
	}
}
`
	testPath := filepath.Join(tmpDir, "math_test.go")
	if err := os.WriteFile(testPath, []byte(testSrc), 0644); err != nil {
		t.Fatal(err)
	}

	idx := NewMemoryTestIndex()
	idx.AddCoverage(srcPath, 3, 5, "TestFastAdd")
	idx.AddCoverage(srcPath, 7, 9, "TestSlowMultiply")

	mutDir := filepath.Join(tmpDir, "mut")
	config := Config{
		MutationDir: mutDir,
		Mutators:    []mutator.Mutator{&mutator.ArithmeticMutator{}},
		Workers:   2,
		Timeout:   5 * time.Second,
		TestIndex: idx,
	}

	report, err := Run([]string{srcPath}, config)
	if err != nil {
		t.Fatalf("Run targeted failed: %v", err)
	}

	if report.Total != 2 {
		t.Errorf("expected 2 mutations, got %d", report.Total)
	}
	if report.Killed != 2 {
		t.Errorf("expected 2 killed mutations, got %d", report.Killed)
	}
}

func TestRun_DBPersistence(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/dbtest\n\ngo 1.20\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	src := `package dbtest

func Incr(x int) int {
	return x + 1
}
`
	srcPath := filepath.Join(tmpDir, "calc.go")
	if err := os.WriteFile(srcPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	testSrc := `package dbtest

import "testing"

func TestIncr(t *testing.T) {
	if Incr(2) != 3 {
		t.Fail()
	}
}
`
	testPath := filepath.Join(tmpDir, "calc_test.go")
	if err := os.WriteFile(testPath, []byte(testSrc), 0644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(tmpDir, "testquery.db")
	mutDir := filepath.Join(tmpDir, "mut")

	config := Config{
		MutationDir: mutDir,
		Mutators:    []mutator.Mutator{&mutator.ArithmeticMutator{}},
		Workers:     1,
		Timeout:     5 * time.Second,
		DBPath:      dbPath,
	}

	report, err := Run([]string{srcPath}, config)
	if err != nil {
		t.Fatalf("Run with DB persistence failed: %v", err)
	}

	db := NewDatabase(dbPath)
	summary, err := db.QuerySummary()
	if err != nil {
		t.Fatalf("QuerySummary failed: %v", err)
	}

	if summary.TotalMutations != report.Total {
		t.Errorf("DB total (%d) != report total (%d)", summary.TotalMutations, report.Total)
	}
	if summary.Killed != report.Killed {
		t.Errorf("DB killed (%d) != report killed (%d)", summary.Killed, report.Killed)
	}
}
