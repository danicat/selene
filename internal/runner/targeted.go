package runner

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// TestIndex provides thread-safe in-memory mapping from (file, line) to covering test names
// and tracks baseline test execution durations for adaptive timeout calculations.
type TestIndex interface {
	GetCoveringTests(file string, line int) []string
	AddCoverage(file string, startLine, endLine int, testName string)
	SetTestDuration(testName string, d time.Duration)
	GetTestDuration(testName string) time.Duration
	GetExpectedDuration(file string, line int) time.Duration
}

type coverageRange struct {
	startLine int
	endLine   int
	testName  string
}

// MemoryTestIndex is an in-memory thread-safe implementation of TestIndex.
type MemoryTestIndex struct {
	mu        sync.RWMutex
	ranges    map[string][]coverageRange // keyed by normalized file path
	durations map[string]time.Duration   // testName -> duration
}

// NewMemoryTestIndex creates a new in-memory test coverage index.
func NewMemoryTestIndex() *MemoryTestIndex {
	return &MemoryTestIndex{
		ranges:    make(map[string][]coverageRange),
		durations: make(map[string]time.Duration),
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

// SetTestDuration records the baseline duration of a specific test.
func (idx *MemoryTestIndex) SetTestDuration(testName string, d time.Duration) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.durations == nil {
		idx.durations = make(map[string]time.Duration)
	}
	idx.durations[testName] = d
}

// GetTestDuration retrieves the recorded duration of a specific test.
func (idx *MemoryTestIndex) GetTestDuration(testName string) time.Duration {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if idx.durations == nil {
		return 0
	}
	return idx.durations[testName]
}

// GetExpectedDuration returns the summed baseline duration of all tests covering the line.
func (idx *MemoryTestIndex) GetExpectedDuration(file string, line int) time.Duration {
	tests := idx.GetCoveringTests(file, line)
	if len(tests) == 0 {
		return 0
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var total time.Duration
	for _, t := range tests {
		total += idx.durations[t]
	}
	return total
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

// CalculateAdaptiveTimeout computes a dynamic timeout budget based on baseline test duration.
// Formula: max(1.5s, 3.0 * expectedDuration + 0.5s), capped by userTimeout.
func CalculateAdaptiveTimeout(expectedDuration time.Duration, userTimeout time.Duration) time.Duration {
	if expectedDuration <= 0 {
		return userTimeout
	}

	const (
		floorTime      = 1500 * time.Millisecond
		scaleFactor    = 3.0
		overheadMargin = 500 * time.Millisecond
	)

	dynamic := max(time.Duration(float64(expectedDuration)*scaleFactor)+overheadMargin, floorTime)
	if userTimeout > 0 && dynamic > userTimeout {
		return userTimeout
	}
	return dynamic
}

// LoadTestIndex loads coverage data from a SQLite database file at dbPath.
func LoadTestIndex(dbPath string) (TestIndex, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	return LoadTestIndexFromDB(db)
}

// LoadTestIndexFromDB loads coverage data and test durations from an open SQLite database connection.
func LoadTestIndexFromDB(db *sql.DB) (TestIndex, error) {
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='test_coverage'").Scan(&tableName)
	if err != nil {
		return NewMemoryTestIndex(), nil
	}

	// Inspect columns to support both (start_line, end_line) and (line) schemas
	rowsInfo, err := db.Query("PRAGMA table_info(test_coverage)")
	if err != nil {
		return nil, fmt.Errorf("failed to inspect test_coverage table: %w", err)
	}
	defer rowsInfo.Close()

	hasStartLine := false
	hasEndLine := false
	hasLine := false
	for rowsInfo.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue sql.NullString
		if err := rowsInfo.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("failed to scan pragma table_info: %w", err)
		}
		switch strings.ToLower(name) {
		case "start_line":
			hasStartLine = true
		case "end_line":
			hasEndLine = true
		case "line":
			hasLine = true
		}
	}

	var query string
	if hasStartLine && hasEndLine {
		query = "SELECT file, start_line, end_line, test_name FROM test_coverage"
	} else if hasLine {
		query = "SELECT file, line, line, test_name FROM test_coverage"
	} else {
		return NewMemoryTestIndex(), nil
	}

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query test_coverage: %w", err)
	}
	defer rows.Close()

	index := NewMemoryTestIndex()
	for rows.Next() {
		var file, testName string
		var startLine, endLine int
		if err := rows.Scan(&file, &startLine, &endLine, &testName); err != nil {
			return nil, fmt.Errorf("failed to scan test_coverage row: %w", err)
		}
		index.AddCoverage(file, startLine, endLine, testName)
	}

	// Load test durations from all_tests if table exists
	var allTestsTable string
	_ = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='all_tests'").Scan(&allTestsTable)
	if allTestsTable != "" {
		dRows, err := db.Query("SELECT test, elapsed FROM all_tests WHERE elapsed IS NOT NULL AND test != ''")
		if err == nil {
			defer dRows.Close()
			for dRows.Next() {
				var tName string
				var elapsed float64
				if err := dRows.Scan(&tName, &elapsed); err == nil {
					index.SetTestDuration(tName, time.Duration(elapsed*float64(time.Second)))
				}
			}
		}
	}

	return index, nil
}
