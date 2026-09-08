package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danicat/selene/internal/mutator"
)


func TestTimebox_Reporting(t *testing.T) {
	rep := &Report{
		Total:           5,
		Killed:          4,
		Survived:        1,
		TimeboxExpired:  true,
		TimeboxDuration: 5 * time.Minute,
		RemainingTasks:  15,
		TotalDiscovered: 20,
	}

	stats := TestStats{
		TotalTests: 3,
		GoodTests:  []string{"TestA"},
	}

	jr := NewJSONReport(rep, stats, false)
	if jr.Timebox != "5m0s" {
		t.Errorf("expected timebox '5m0s', got %q", jr.Timebox)
	}
	if !jr.TimeboxExpired {
		t.Errorf("expected TimeboxExpired true")
	}
	if jr.RemainingTasks != 15 {
		t.Errorf("expected RemainingTasks 15, got %d", jr.RemainingTasks)
	}
	if jr.TotalDiscovered != 20 {
		t.Errorf("expected TotalDiscovered 20, got %d", jr.TotalDiscovered)
	}

	var buf bytes.Buffer
	PrintHumanReport(&buf, rep, stats, false)
	out := buf.String()
	if !strings.Contains(out, "⏰ Timebox reached (5m0s)") {
		t.Errorf("human report missing timebox banner: %s", out)
	}
	if !strings.Contains(out, "Evaluated 5 mutations (15 remaining out of 20 total in codebase)") {
		t.Errorf("human report missing remaining tasks text: %s", out)
	}
}

func TestRun_TimeboxExpiration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "selene-timebox-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/tb\n\ngo 1.20\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	src := `package main
func Compute(a, b int) int {
	x := a + b
	y := x * 2
	z := y - 1
	return z
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(src), 0644)
	if err != nil {
		t.Fatal(err)
	}

	testSrc := `package main
import "testing"
func TestCompute(t *testing.T) {
	if Compute(1, 2) != 5 {
		t.Fail()
	}
}
`
	err = os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte(testSrc), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Run with a very tight timebox (1ms) to force early expiration
	config := Config{
		MutationDir: tmpDir,
		Mutators:    []mutator.Mutator{&mutator.ArithmeticMutator{}},
		Workers:     1,
		Timebox:     1 * time.Millisecond,
		Timeout:     5 * time.Second,
	}

	// We sleep slightly to ensure context deadline is exceeded before or during tasks
	time.Sleep(5 * time.Millisecond)

	rep, err := Run([]string{filepath.Join(tmpDir, "main.go")}, config)
	// It's possible baseline coverage generation expires or mutation loop expires
	if err != nil {
		if !strings.Contains(err.Error(), "timebox") {
			t.Fatalf("unexpected error: %v", err)
		}
		// Baseline coverage expired within 1ms, which is also a valid timebox cutoff
		return
	}

	if !rep.TimeboxExpired {
		t.Fatalf("expected TimeboxExpired to be true, got false")
	}
	if rep.TimeboxDuration != 1*time.Millisecond {
		t.Errorf("expected TimeboxDuration 1ms, got %v", rep.TimeboxDuration)
	}
}
