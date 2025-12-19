package runner

import (
	"os"
	"path/filepath"

	"testing"

	"github.com/danicat/selene/internal/mutator"
)

func TestRunMutations(t *testing.T) {
	// Create a temporary directory for mutations
	tmpDir, err := os.MkdirTemp("", "mutation_test")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create go.mod
	err = os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test\n\ngo 1.24"), 0644)
	if err != nil {
		t.Fatalf("WriteFile go.mod failed: %v", err)
	}

	// Create a dummy source file
	srcFile := filepath.Join(tmpDir, "main_test.go")
	err = os.WriteFile(srcFile, []byte(`package main
import "testing"
func TestMain(t *testing.T) {
	if 1 == 1 {
		return
	}
	t.Fail()
}`), 0644)
	if err != nil {
		t.Fatalf("WriteFile main_test.go failed: %v", err)
	}

		// Run mutations
		results, err := RunIterative([]string{srcFile}, tmpDir, []mutator.Mutator{&mutator.Comparison{}}, nil)
		if err != nil {
			t.Fatalf("RunIterative failed: %v", err)
		}
	
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
	
		// Check result
		// The mutation changes `if 1 == 1` to `if 1 != 1`.
		// So the condition becomes false, and `t.Fail()` is executed.
		// Thus, the test should fail (mutation killed).
		if results[0].Status != "killed" {
			t.Errorf("expected status 'killed', got '%s'", results[0].Status)
		}}
