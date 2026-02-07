package runner

import (
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
func (m *MockMutator) Check(n any) []mutator.Mutation {
	// In a real test we'd need valid AST node, but our runner parses files.
	// We'll create a simple valid file test.
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
	report, err := Run([]string{srcPath}, config)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if report.Total != 1 {
		t.Errorf("expected 1 mutation, got %d", report.Total)
	}
	if report.Caught != 1 {
		t.Errorf("expected 1 caught mutation, got %d", report.Caught)
	}
}

func TestReportScore(t *testing.T) {
	tests := []struct {
		report Report
		want   float64
	}{
		{Report{Total: 0, Caught: 0}, 0},
		{Report{Total: 10, Caught: 5}, 50.0},
		{Report{Total: 4, Caught: 4}, 100.0},
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

// Stub function to mock exec.Command if we wanted strictly unit tests,

// but for this integration test we actually want to run go test.
// NOTE: This test requires 'go' to be in PATH.
func TestRunGoTest(t *testing.T) {
	// This function is hard to unit test without mocking exec.Command.
	// Given the context, the integration test TestRun covers the happy path.
	// We can test error cases here if needed.

	// Create a dummy overlay file
	tmpDir := t.TempDir()
	overlayPath := filepath.Join(tmpDir, "overlay.json")

	// Write invalid JSON to force error or just check parsing of empty output
	if err := os.WriteFile(overlayPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// Calling runGoTest on a dir that doesn't have tests might return empty

	// or error depending on go test behavior.
	// Since we can't easily mock exec in Go without re-execing the test binary,
	// we'll rely on TestRun for the integration verification.
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
