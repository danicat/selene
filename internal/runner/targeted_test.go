package runner

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
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
			name:     "subtest normalized to parent",
			input:    []string{"TestAdd/subtest_1"},
			expected: "^TestAdd$",
		},
		{
			name:     "multiple tests",
			input:    []string{"TestAdd", "TestSub"},
			expected: "^(TestAdd|TestSub)$",
		},
		{
			name:     "multiple subtests deduplicated",
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
			expected: `^TestAdd\[1\]$`,
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

	// Line 18 in main.go -> TestBar, TestFoo
	cov18 := idx.GetCoveringTests("/app/main.go", 18)
	if len(cov18) != 2 || cov18[0] != "TestBar" || cov18[1] != "TestFoo" {
		t.Errorf("expected [TestBar, TestFoo] for line 18, got %v", cov18)
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

func TestDatabase_LoadTestCoverage_StartEndLineSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "testquery.db")

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite DB: %v", err)
	}

	// Create test_coverage table with (file, start_line, end_line, test_name, count)
	createTableSQL := `
	CREATE TABLE test_coverage (
		file TEXT NOT NULL,
		start_line INTEGER NOT NULL,
		end_line INTEGER NOT NULL,
		test_name TEXT NOT NULL,
		count INTEGER NOT NULL
	);
	`
	if _, err := dbConn.Exec(createTableSQL); err != nil {
		t.Fatalf("failed to create test_coverage table: %v", err)
	}

	// Insert test data
	insertSQL := `
	INSERT INTO test_coverage (file, start_line, end_line, test_name, count) VALUES
	('calc.go', 10, 20, 'TestCompute', 5),
	('calc.go', 15, 25, 'TestCompute/Sub', 3),
	('calc.go', 30, 40, 'TestMax', 1),
	('math.go', 5, 15, 'TestAdd[1]', 2);
	`
	if _, err := dbConn.Exec(insertSQL); err != nil {
		t.Fatalf("failed to insert coverage rows: %v", err)
	}
	dbConn.Close()

	db := NewDatabase(dbPath)
	index, err := db.LoadTestCoverage()
	if err != nil {
		t.Fatalf("LoadTestCoverage failed: %v", err)
	}

	// Check calc.go line 12 -> TestCompute
	cov12 := index.GetCoveringTests("calc.go", 12)
	if len(cov12) != 1 || cov12[0] != "TestCompute" {
		t.Errorf("expected [TestCompute] for calc.go:12, got %v", cov12)
	}

	// Check calc.go line 18 -> TestCompute (subtest normalized and deduplicated)
	cov18 := index.GetCoveringTests("calc.go", 18)
	if len(cov18) != 1 || cov18[0] != "TestCompute" {
		t.Errorf("expected [TestCompute] for calc.go:18, got %v", cov18)
	}

	// Check calc.go line 35 -> TestMax
	cov35 := index.GetCoveringTests("calc.go", 35)
	if len(cov35) != 1 || cov35[0] != "TestMax" {
		t.Errorf("expected [TestMax] for calc.go:35, got %v", cov35)
	}

	// Check math.go line 10 -> TestAdd[1]
	covMath := index.GetCoveringTests("math.go", 10)
	if len(covMath) != 1 || covMath[0] != "TestAdd[1]" {
		t.Errorf("expected [TestAdd[1]] for math.go:10, got %v", covMath)
	}

	// Check BuildRunFilter with loaded tests
	filter := BuildRunFilter(covMath)
	expectedFilter := `^TestAdd\[1\]$`
	if filter != expectedFilter {
		t.Errorf("BuildRunFilter(%v) = %q, want %q", covMath, filter, expectedFilter)
	}
}

func TestDatabase_LoadTestCoverage_SingleLineSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "testquery_single.db")

	dbConn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite DB: %v", err)
	}

	// Create test_coverage table with (file, line, test_name, count)
	createTableSQL := `
	CREATE TABLE test_coverage (
		file TEXT NOT NULL,
		line INTEGER NOT NULL,
		test_name TEXT NOT NULL,
		count INTEGER NOT NULL
	);
	`
	if _, err := dbConn.Exec(createTableSQL); err != nil {
		t.Fatalf("failed to create single-line test_coverage table: %v", err)
	}

	insertSQL := `
	INSERT INTO test_coverage (file, line, test_name, count) VALUES
	('calc.go', 10, 'TestCompute', 1),
	('calc.go', 11, 'TestCompute', 1),
	('calc.go', 20, 'TestMax', 1);
	`
	if _, err := dbConn.Exec(insertSQL); err != nil {
		t.Fatalf("failed to insert single-line coverage rows: %v", err)
	}
	dbConn.Close()

	index, err := LoadTestIndex(dbPath)
	if err != nil {
		t.Fatalf("LoadTestIndex failed: %v", err)
	}

	cov10 := index.GetCoveringTests("calc.go", 10)
	if len(cov10) != 1 || cov10[0] != "TestCompute" {
		t.Errorf("expected [TestCompute] for calc.go:10, got %v", cov10)
	}

	cov15 := index.GetCoveringTests("calc.go", 15)
	if len(cov15) != 0 {
		t.Errorf("expected [] for calc.go:15, got %v", cov15)
	}

	cov20 := index.GetCoveringTests("calc.go", 20)
	if len(cov20) != 1 || cov20[0] != "TestMax" {
		t.Errorf("expected [TestMax] for calc.go:20, got %v", cov20)
	}
}

func TestDatabase_LoadTestCoverage_MissingTable(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.db")

	// InitDB creates selene tables but not test_coverage
	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	db.Close()

	index, err := LoadTestIndex(dbPath)
	if err != nil {
		t.Fatalf("LoadTestIndex failed on missing table: %v", err)
	}
	if index == nil {
		t.Fatalf("expected non-nil TestIndex for missing table")
	}

	cov := index.GetCoveringTests("anything.go", 1)
	if len(cov) != 0 {
		t.Errorf("expected empty coverage from empty index, got %v", cov)
	}
}
