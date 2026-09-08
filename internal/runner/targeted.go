package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
)

// TestIndex provides thread-safe in-memory mapping from (file, line) to covering test names.
type TestIndex interface {
	GetCoveringTests(file string, line int) []string
	AddCoverage(file string, startLine, endLine int, testName string)
	IsCovered(file string, line int) bool
}

type coverageRange struct {
	startLine int
	endLine   int
	testName  string
}

// MemoryTestIndex is an in-memory thread-safe implementation of TestIndex.
type MemoryTestIndex struct {
	mu     sync.RWMutex
	ranges map[string][]coverageRange // keyed by normalized file path
}

// NewMemoryTestIndex creates a new in-memory test coverage index.
func NewMemoryTestIndex() *MemoryTestIndex {
	return &MemoryTestIndex{
		ranges: make(map[string][]coverageRange),
	}
}

// normalizePath cleans and standardizes file path for consistent matching.
func normalizePath(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// AddCoverage registers a test covering the specified line range in a file.
func (idx *MemoryTestIndex) AddCoverage(file string, startLine, endLine int, testName string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	normFile := normalizePath(file)
	idx.ranges[normFile] = append(idx.ranges[normFile], coverageRange{
		startLine: startLine,
		endLine:   endLine,
		testName:  testName,
	})

	// Also index under cleaned original path if different
	cleanFile := filepath.Clean(file)
	if cleanFile != normFile {
		idx.ranges[cleanFile] = append(idx.ranges[cleanFile], coverageRange{
			startLine: startLine,
			endLine:   endLine,
			testName:  testName,
		})
	}

	// Also index under base file name in case relative/partial paths are matched
	base := filepath.Base(file)
	if base != normFile && base != cleanFile {
		idx.ranges[base] = append(idx.ranges[base], coverageRange{
			startLine: startLine,
			endLine:   endLine,
			testName:  testName,
		})
	}
}

// IsCovered reports whether at least one test covers the specified line in the file.
func (idx *MemoryTestIndex) IsCovered(file string, line int) bool {
	return len(idx.GetCoveringTests(file, line)) > 0
}

// GetCoveringTests returns all unique test and subtest names that cover the given line.
func (idx *MemoryTestIndex) GetCoveringTests(file string, line int) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	testSet := make(map[string]bool)
	normFile := normalizePath(file)
	cleanFile := filepath.Clean(file)
	baseFile := filepath.Base(file)

	var candidateRanges [][]coverageRange

	// 1. Prioritize exact matches first
	if r, exists := idx.ranges[normFile]; exists {
		candidateRanges = append(candidateRanges, r)
	} else if cleanFile != normFile {
		if r, exists := idx.ranges[cleanFile]; exists {
			candidateRanges = append(candidateRanges, r)
		}
	}

	// 2. Only fallback to base filename or suffix matching if no exact matches exist
	if len(candidateRanges) == 0 {
		if r, exists := idx.ranges[baseFile]; exists {
			candidateRanges = append(candidateRanges, r)
		} else {
			for key, r := range idx.ranges {
				if strings.HasSuffix(normFile, key) || strings.HasSuffix(key, normFile) ||
					strings.HasSuffix(cleanFile, key) || strings.HasSuffix(key, cleanFile) {
					candidateRanges = append(candidateRanges, r)
				}
			}
		}
	}

	for _, list := range candidateRanges {
		for _, r := range list {
			if line >= r.startLine && line <= r.endLine {
				tName := strings.TrimSpace(r.testName)
				if tName != "" {
					testSet[tName] = true
				}
			}
		}
	}

	tests := make([]string, 0, len(testSet))
	for t := range testSet {
		tests = append(tests, t)
	}
	sort.Strings(tests)
	return tests
}

// BuildRunFilter generates the -run regex for go test matching exact tests and subtests.
// Correctly handles Go's hierarchical slash-splitting in testing/match.go.
func BuildRunFilter(tests []string) string {
	if len(tests) == 0 {
		return ""
	}

	// Deduplicate
	seen := make(map[string]bool)
	var unique []string
	for _, t := range tests {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			unique = append(unique, t)
		}
	}

	if len(unique) == 0 {
		return ""
	}
	if len(unique) == 1 {
		return "^" + regexp.QuoteMeta(unique[0]) + "$"
	}

	// Group tests by top-level parent function
	parentMap := make(map[string][]string)
	for _, t := range unique {
		if parent, sub, ok := strings.Cut(t, "/"); ok && parent != "" && sub != "" {
			parentMap[parent] = append(parentMap[parent], sub)
		} else {
			parentMap[t] = append(parentMap[t], "")
		}
	}

	// Case A: All subtests share the EXACT same parent test function
	if len(parentMap) == 1 {
		for parent, subs := range parentMap {
			hasEmpty := slices.Contains(subs, "")
			if hasEmpty || len(subs) == 0 {
				return "^" + regexp.QuoteMeta(parent) + "$"
			}
			quotedSubs := make([]string, len(subs))
			for i, s := range subs {
				quotedSubs[i] = regexp.QuoteMeta(s)
			}
			sort.Strings(quotedSubs)
			return "^" + regexp.QuoteMeta(parent) + "/(" + strings.Join(quotedSubs, "|") + ")$"
		}
	}

	// Case B: Tests span multiple parent functions
	var parents []string
	for parent := range parentMap {
		parents = append(parents, regexp.QuoteMeta(parent))
	}
	sort.Strings(parents)
	return "^(" + strings.Join(parents, "|") + ")$"
}

// BuildCoverageIndex profiles tests across the specified targets to map which tests cover which source lines.
// It compiles the test binary for each target package once, then runs individual tests with coverage profiling,
// indexing all statement-level coverage into a MemoryTestIndex.
func BuildCoverageIndex(ctx context.Context, targets []PackageTarget, workers int, tempDir string, verbose bool) (*MemoryTestIndex, error) {
	index := NewMemoryTestIndex()
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	for targetIdx, target := range targets {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		tests, err := DiscoverTests(target.Dir)
		if err != nil || len(tests) == 0 {
			continue
		}

		testBin := filepath.Join(tempDir, fmt.Sprintf("test_pkg_%d.bin", targetIdx))
		if runtime.GOOS == "windows" {
			testBin += ".exe"
		}

		// Compile test binary with coverage enabled
		buildCmd := exec.CommandContext(ctx, "go", "test", "-c", "-coverpkg=./...", "-o", testBin, ".")
		buildCmd.Dir = target.Dir
		if _, err := buildCmd.CombinedOutput(); err != nil {
			// Fall back to -cover if -coverpkg=./... fails
			fallbackCmd := exec.CommandContext(ctx, "go", "test", "-c", "-cover", "-o", testBin, ".")
			fallbackCmd.Dir = target.Dir
			if out2, err2 := fallbackCmd.CombinedOutput(); err2 != nil {
				return nil, fmt.Errorf("failed to compile tests in %s: %s\n%s", target.Dir, err2, out2)
			}
		}
		defer func(bin string) { _ = os.Remove(bin) }(testBin)

		type testJob struct {
			testName string
			index    int
		}
		jobChan := make(chan testJob, len(tests))
		for i, t := range tests {
			jobChan <- testJob{testName: t, index: i}
		}
		close(jobChan)

		numWorkers := min(workers, len(tests))
		var wg sync.WaitGroup
		var errMu sync.Mutex
		var firstErr error

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobChan {
					if ctx.Err() != nil {
						return
					}

					covFile := filepath.Join(tempDir, fmt.Sprintf("cov_%d_%d.out", targetIdx, job.index))
					runCmd := exec.CommandContext(ctx, testBin, "-test.run", "^"+regexp.QuoteMeta(job.testName)+"$", "-test.coverprofile="+covFile)
					runCmd.Dir = target.Dir
					out, err := runCmd.CombinedOutput()
					if err != nil {
						errMu.Lock()
						if firstErr == nil {
							firstErr = fmt.Errorf("baseline test %q failed in %s: %s\n%s", job.testName, target.Dir, err, out)
						}
						errMu.Unlock()
						_ = os.Remove(covFile)
						return
					}

					cov, err := LoadCoverage(covFile)
					_ = os.Remove(covFile)
					if err != nil {
						continue
					}

					for file, blocks := range cov.Blocks {
						for _, b := range blocks {
							index.AddCoverage(file, b.StartLine, b.EndLine, job.testName)
						}
					}
				}
			}()
		}
		wg.Wait()

		if firstErr != nil {
			return nil, firstErr
		}
	}

	return index, nil
}
