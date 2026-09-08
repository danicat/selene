package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestBuildRunFilter(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{
			name:     "empty",
			input:    []string{},
			expected: "",
		},
		{
			name:     "single test",
			input:    []string{"TestAdd"},
			expected: "^TestAdd$",
		},
		{
			name:     "single subtest exact match",
			input:    []string{"TestAdd/subtest_1"},
			expected: "^TestAdd/subtest_1$",
		},
		{
			name:     "multiple tests",
			input:    []string{"TestAdd", "TestSub"},
			expected: "^(TestAdd|TestSub)$",
		},
		{
			name:     "multiple subtests same parent",
			input:    []string{"TestAdd/a", "TestAdd/b"},
			expected: "^TestAdd/(a|b)$",
		},
		{
			name:     "multiple subtests across different parents",
			input:    []string{"TestAdd/a", "TestAdd/b", "TestSub/1"},
			expected: "^(TestAdd|TestSub)$",
		},
		{
			name:     "regex brackets",
			input:    []string{"TestAdd[1]"},
			expected: `^TestAdd\[1\]$`,
		},
		{
			name:     "regex plus sign",
			input:    []string{"TestAdd+Sub"},
			expected: `^TestAdd\+Sub$`,
		},
		{
			name:     "regex parentheses and dots",
			input:    []string{"TestFoo(Bar).Case"},
			expected: `^TestFoo\(Bar\)\.Case$`,
		},
		{
			name:     "regex asterisks and question marks",
			input:    []string{"TestA*B?C"},
			expected: `^TestA\*B\?C$`,
		},
		{
			name:     "multiple tests with regex chars sorted",
			input:    []string{"TestAdd[1]", "TestAdd+Sub"},
			expected: `^(TestAdd\+Sub|TestAdd\[1\])$`,
		},
		{
			name:     "regex special characters in subtest name",
			input:    []string{"TestAdd[1]/sub_case"},
			expected: `^TestAdd\[1\]/sub_case$`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildRunFilter(tc.input)
			if got != tc.expected {
				t.Errorf("BuildRunFilter(%v) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestMemoryTestIndex(t *testing.T) {
	idx := NewMemoryTestIndex()
	idx.AddCoverage("/app/main.go", 10, 20, "TestFoo")
	idx.AddCoverage("/app/main.go", 15, 25, "TestBar/subtest")
	idx.AddCoverage("/app/other.go", 1, 5, "TestOther")

	// Line 12 in main.go -> TestFoo
	cov12 := idx.GetCoveringTests("/app/main.go", 12)
	if len(cov12) != 1 || cov12[0] != "TestFoo" {
		t.Errorf("expected [TestFoo] for line 12, got %v", cov12)
	}

	// Line 18 in main.go -> TestBar/subtest, TestFoo
	cov18 := idx.GetCoveringTests("/app/main.go", 18)
	if len(cov18) != 2 || cov18[0] != "TestBar/subtest" || cov18[1] != "TestFoo" {
		t.Errorf("expected [TestBar/subtest, TestFoo] for line 18, got %v", cov18)
	}

	// Line 30 in main.go -> empty
	cov30 := idx.GetCoveringTests("/app/main.go", 30)
	if len(cov30) != 0 {
		t.Errorf("expected [] for line 30, got %v", cov30)
	}

	// Suffix / relative path lookup
	covRel := idx.GetCoveringTests("main.go", 12)
	if len(covRel) != 1 || covRel[0] != "TestFoo" {
		t.Errorf("expected [TestFoo] for relative path line 12, got %v", covRel)
	}
}

func TestMemoryTestIndex_Concurrent(t *testing.T) {
	idx := NewMemoryTestIndex()
	const numGoroutines = 20
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Writers
	for g := range numGoroutines {
		go func(gid int) {
			defer wg.Done()
			file := fmt.Sprintf("/path/to/file_%d.go", gid%5)
			for i := range opsPerGoroutine {
				start := i * 10
				end := start + 5
				testName := fmt.Sprintf("TestFunc_%d_%d", gid, i)
				idx.AddCoverage(file, start, end, testName)
			}
		}(g)
	}

	// Readers
	for g := range numGoroutines {
		go func(gid int) {
			defer wg.Done()
			file := fmt.Sprintf("/path/to/file_%d.go", gid%5)
			for i := range opsPerGoroutine {
				line := i * 10
				_ = idx.GetCoveringTests(file, line)
				_ = idx.GetCoveringTests(filepath.Base(file), line)
			}
		}(g)
	}

	wg.Wait()

	// Verify reader works correctly after all writes complete
	cov := idx.GetCoveringTests("/path/to/file_0.go", 0)
	if len(cov) == 0 {
		t.Errorf("expected coverage for /path/to/file_0.go at line 0, got none")
	}
}

func TestBuildCoverageIndex(t *testing.T) {
	ctx := context.Background()
	absTestdata, err := filepath.Abs("../../testdata")
	if err != nil {
		t.Fatalf("failed to resolve testdata path: %v", err)
	}

	targets := []PackageTarget{
		{
			Dir:        absTestdata,
			ImportPath: "github.com/danicat/selene/testdata",
			GoFiles:    []string{filepath.Join(absTestdata, "cond.go")},
		},
	}

	tmpDir := t.TempDir()
	index, err := BuildCoverageIndex(ctx, targets, 2, tmpDir, false)
	if err != nil {
		t.Fatalf("BuildCoverageIndex failed: %v", err)
	}

	condFile := filepath.Join(absTestdata, "cond.go")
	if !index.IsCovered(condFile, 6) {
		t.Errorf("expected line 6 in cond.go to be covered")
	}

	covTests := index.GetCoveringTests(condFile, 6)
	if len(covTests) == 0 {
		t.Errorf("expected covering tests for cond.go:6, got none")
	}

	// Line 7 is executed by TestFake (cond(100))
	if !index.IsCovered(condFile, 7) {
		t.Errorf("expected line 7 in cond.go to be covered")
	}

	// Line 1 (package header) has no executable statements
	if index.IsCovered(condFile, 1) {
		t.Errorf("expected line 1 not to be covered")
	}
}
