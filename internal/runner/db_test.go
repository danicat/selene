package runner

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInitDB(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB(:memory:) failed: %v", err)
	}
	defer db.Close()

	// Verify tables exist
	tables := []string{"selene", "selene_tests"}
	for _, table := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Fatalf("table %q not found in sqlite_master: %v", table, err)
		}
		if name != table {
			t.Errorf("expected table name %q, got %q", table, name)
		}
	}

	// Verify views exist
	views := []string{"selene_survived", "selene_zero_kill_tests", "selene_bad_tests", "selene_summary"}
	for _, view := range views {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='view' AND name=?", view).Scan(&name)
		if err != nil {
			t.Fatalf("view %q not found in sqlite_master: %v", view, err)
		}
		if name != view {
			t.Errorf("expected view name %q, got %q", view, name)
		}
	}
}

func TestWriteResultsAndViews(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	mutations := []MutationRecord{
		{
			ID:       "mut-1",
			Mutator:  "boundary_incdec",
			File:     "foo.go",
			Line:     10,
			Col:      5,
			Status:   "killed",
			KilledBy: []string{"TestFoo", "TestBar"},
		},
		{
			ID:       "mut-2",
			Mutator:  "reverse_if_cond",
			File:     "foo.go",
			Line:     20,
			Col:      8,
			Status:   "survived",
			KilledBy: nil,
		},
		{
			ID:       "mut-3",
			Mutator:  "expression",
			File:     "bar.go",
			Line:     15,
			Col:      3,
			Status:   "uncovered",
			KilledBy: nil,
		},
		{
			ID:       "mut-4",
			Mutator:  "expression",
			File:     "bar.go",
			Line:     30,
			Col:      4,
			Status:   "timeout",
			KilledBy: nil,
		},
		{
			ID:       "mut-5",
			Mutator:  "literals",
			File:     "baz.go",
			Line:     5,
			Col:      1,
			Status:   "build_failure",
			KilledBy: nil,
		},
	}

	tests := []TestRecord{
		{
			TestName:        "TestFoo",
			Package:         "example.com/pkg",
			Status:          "good",
			MutationsKilled: 1,
			KilledMutantIDs: []string{"mut-1"},
		},
		{
			TestName:        "TestUseless",
			Package:         "example.com/pkg",
			Status:          "bad",
			MutationsKilled: 0,
			KilledMutantIDs: nil,
		},
	}

	if err := WriteResults(db, mutations, tests); err != nil {
		t.Fatalf("WriteResults failed: %v", err)
	}

	// 1. Verify selene table
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM selene").Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 mutations, got %d", count)
	}

	var killedBy string
	err = db.QueryRow("SELECT killed_by FROM selene WHERE id = 'mut-1'").Scan(&killedBy)
	if err != nil {
		t.Fatalf("failed to query mut-1: %v", err)
	}
	if killedBy != "TestFoo,TestBar" {
		t.Errorf("expected killed_by 'TestFoo,TestBar', got %q", killedBy)
	}

	// 2. Verify selene_survived view
	rows, err := db.Query("SELECT id, mutator, file, line, col FROM selene_survived")
	if err != nil {
		t.Fatalf("failed to query selene_survived: %v", err)
	}
	defer rows.Close()

	var survivedIDs []string
	for rows.Next() {
		var id, mutatorName, file string
		var line, col int
		if err := rows.Scan(&id, &mutatorName, &file, &line, &col); err != nil {
			t.Fatalf("failed to scan selene_survived row: %v", err)
		}
		survivedIDs = append(survivedIDs, id)
		if id == "mut-2" {
			if mutatorName != "reverse_if_cond" || file != "foo.go" || line != 20 || col != 8 {
				t.Errorf("unexpected survived record fields: got %s, %s, %d, %d", mutatorName, file, line, col)
			}
		}
	}
	if !reflect.DeepEqual(survivedIDs, []string{"mut-2"}) {
		t.Errorf("expected survived IDs ['mut-2'], got %v", survivedIDs)
	}

	// 3. Verify selene_zero_kill_tests view and QueryZeroKillTests
	zeroKillRecords, err := QueryZeroKillTests(db)
	if err != nil {
		t.Fatalf("QueryZeroKillTests failed: %v", err)
	}
	if len(zeroKillRecords) != 1 || zeroKillRecords[0].TestName != "TestUseless" {
		t.Errorf("expected zero kill test 'TestUseless', got %+v", zeroKillRecords)
	}

	// Verify selene_bad_tests view (compatibility alias)
	badRows, err := db.Query("SELECT test_name, package FROM selene_bad_tests")
	if err != nil {
		t.Fatalf("failed to query selene_bad_tests: %v", err)
	}
	defer badRows.Close()

	var badTests []string
	for badRows.Next() {
		var name, pkg string
		if err := badRows.Scan(&name, &pkg); err != nil {
			t.Fatalf("failed to scan bad test: %v", err)
		}
		badTests = append(badTests, name)
		if pkg != "example.com/pkg" {
			t.Errorf("expected package example.com/pkg, got %s", pkg)
		}
	}
	if !reflect.DeepEqual(badTests, []string{"TestUseless"}) {
		t.Errorf("expected bad tests ['TestUseless'], got %v", badTests)
	}

	// 4. Verify selene_summary view
	summary, err := QuerySummary(db)
	if err != nil {
		t.Fatalf("QuerySummary failed: %v", err)
	}
	if summary.TotalMutations != 5 {
		t.Errorf("expected total 5, got %d", summary.TotalMutations)
	}
	if summary.Killed != 1 {
		t.Errorf("expected killed 1, got %d", summary.Killed)
	}
	if summary.Survived != 1 {
		t.Errorf("expected survived 1, got %d", summary.Survived)
	}
	if summary.Uncovered != 1 {
		t.Errorf("expected uncovered 1, got %d", summary.Uncovered)
	}
	if summary.Timeouts != 1 {
		t.Errorf("expected timeouts 1, got %d", summary.Timeouts)
	}
}

func TestWriteResults_ReplaceExisting(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	initialMut := []MutationRecord{
		{ID: "m1", Mutator: "mut1", File: "a.go", Line: 1, Col: 1, Status: "survived"},
	}
	if err := WriteResults(db, initialMut, nil); err != nil {
		t.Fatalf("initial write failed: %v", err)
	}

	updatedMut := []MutationRecord{
		{ID: "m1", Mutator: "mut1", File: "a.go", Line: 1, Col: 1, Status: "killed", KilledBy: []string{"TestA"}},
	}
	if err := WriteResults(db, updatedMut, nil); err != nil {
		t.Fatalf("update write failed: %v", err)
	}

	var status, killedBy string
	err = db.QueryRow("SELECT status, killed_by FROM selene WHERE id = 'm1'").Scan(&status, &killedBy)
	if err != nil {
		t.Fatalf("query updated record failed: %v", err)
	}
	if status != "killed" || killedBy != "TestA" {
		t.Errorf("expected status 'killed' and killed_by 'TestA', got status=%q killed_by=%q", status, killedBy)
	}
}

func TestWriteResults_TransactionRollback(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	// Initial data
	mutations := []MutationRecord{
		{ID: "m1", Mutator: "mut1", File: "a.go", Line: 1, Col: 1, Status: "killed"},
	}
	if err := WriteResults(db, mutations, nil); err != nil {
		t.Fatalf("WriteResults failed: %v", err)
	}

	// Drop the table to simulate error during WriteResults
	if _, err := db.Exec("DROP TABLE selene_tests"); err != nil {
		t.Fatalf("failed to drop table: %v", err)
	}

	// Attempting to write tests will fail and roll back the whole transaction
	err = WriteResults(db, []MutationRecord{
		{ID: "m2", Mutator: "mut2", File: "b.go", Line: 2, Col: 2, Status: "survived"},
	}, []TestRecord{
		{TestName: "TestB", Package: "pkg", Status: "good", MutationsKilled: 1},
	})
	if err == nil {
		t.Fatalf("expected WriteResults to fail when table missing, but it succeeded")
	}

	// Verify m2 was rolled back and only m1 remains
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM selene").Scan(&count); err != nil {
		t.Fatalf("query count failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1 after rollback, got %d", count)
	}

	var id string
	if err := db.QueryRow("SELECT id FROM selene").Scan(&id); err != nil {
		t.Fatalf("query id failed: %v", err)
	}
	if id != "m1" {
		t.Errorf("expected remaining ID 'm1', got %q", id)
	}
}

func TestSaveResultsToDB(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "selene_db_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "nested", "dir", "test_selene.db")

	mutations := []MutationRecord{
		{ID: "m1", Mutator: "mutator1", File: "file1.go", Line: 10, Col: 2, Status: "killed", KilledBy: []string{"Test1"}},
		{ID: "m2", Mutator: "mutator2", File: "file2.go", Line: 20, Col: 4, Status: "survived"},
	}
	tests := []TestRecord{
		{TestName: "Test1", Package: "main", Status: "good", MutationsKilled: 1, KilledMutantIDs: []string{"m1"}},
	}

	if err := SaveResultsToDB(dbPath, mutations, tests); err != nil {
		t.Fatalf("SaveResultsToDB failed: %v", err)
	}

	// Verify database file exists and contains the data
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("database file was not created: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to reopen saved db: %v", err)
	}
	defer db.Close()

	summary, err := QuerySummary(db)
	if err != nil {
		t.Fatalf("QuerySummary failed: %v", err)
	}
	if summary.TotalMutations != 2 || summary.Killed != 1 || summary.Survived != 1 {
		t.Errorf("unexpected summary: %+v", summary)
	}
}

func TestQuerySummary_Empty(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	summary, err := QuerySummary(db)
	if err != nil {
		t.Fatalf("QuerySummary on empty db failed: %v", err)
	}
	if summary.TotalMutations != 0 || summary.Killed != 0 || summary.Survived != 0 || summary.Uncovered != 0 || summary.Timeouts != 0 {
		t.Errorf("expected all zeros for empty db, got %+v", summary)
	}
}
