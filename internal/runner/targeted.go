package runner

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// TestIndex provides thread-safe in-memory mapping from (file, line) to covering test names.
type TestIndex interface {
	GetCoveringTests(file string, line int) []string
	AddCoverage(file string, startLine, endLine int, testName string)
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

// GetCoveringTests returns all unique root parent test names that cover the given line.
func (idx *MemoryTestIndex) GetCoveringTests(file string, line int) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	testSet := make(map[string]bool)
	normFile := normalizePath(file)
	cleanFile := filepath.Clean(file)
	baseFile := filepath.Base(file)

	var candidateRanges [][]coverageRange

	if r, exists := idx.ranges[normFile]; exists {
		candidateRanges = append(candidateRanges, r)
	}
	if cleanFile != normFile {
		if r, exists := idx.ranges[cleanFile]; exists {
			candidateRanges = append(candidateRanges, r)
		}
	}
	if r, exists := idx.ranges[baseFile]; exists {
		candidateRanges = append(candidateRanges, r)
	}

	// Suffix / partial path matching if not found directly
	if len(candidateRanges) == 0 {
		for key, r := range idx.ranges {
			if strings.HasSuffix(normFile, key) || strings.HasSuffix(key, normFile) ||
				strings.HasSuffix(cleanFile, key) || strings.HasSuffix(key, cleanFile) {
				candidateRanges = append(candidateRanges, r)
			}
		}
	}

	for _, list := range candidateRanges {
		for _, r := range list {
			if line >= r.startLine && line <= r.endLine {
				parent := ParentTestName(r.testName)
				if parent != "" {
					testSet[parent] = true
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

// BuildRunFilter generates the -run regex for go test.
// Returns empty string if tests is empty (meaning all tests should run or no filter).
func BuildRunFilter(tests []string) string {
	if len(tests) == 0 {
		return ""
	}

	// Deduplicate and quote special regex chars if any
	seen := make(map[string]bool)
	var filtered []string
	for _, t := range tests {
		parent := ParentTestName(t)
		if parent != "" && !seen[parent] {
			seen[parent] = true
			filtered = append(filtered, regexp.QuoteMeta(parent))
		}
	}

	if len(filtered) == 0 {
		return ""
	}
	if len(filtered) == 1 {
		return "^" + filtered[0] + "$"
	}
	sort.Strings(filtered)
	return "^(" + strings.Join(filtered, "|") + ")$"
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

// LoadTestIndexFromDB loads coverage data from an open SQLite database connection.
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
	return index, nil
}
