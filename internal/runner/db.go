package runner

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// MutationRecord represents a single mutation test outcome.
type MutationRecord struct {
	ID       string   `json:"id"`
	Mutator  string   `json:"mutator"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Col      int      `json:"col"`
	Status   string   `json:"status"` // 'killed', 'survived', 'uncovered', 'timeout', 'build_failure'
	KilledBy []string `json:"killed_by,omitempty"`
}

// TestRecord represents effectiveness metrics for a test.
type TestRecord struct {
	TestName        string   `json:"test_name"`
	Package         string   `json:"package"`
	Status          string   `json:"status"` // 'good', 'bad'
	MutationsKilled int      `json:"mutations_killed"`
	KilledMutantIDs []string `json:"killed_mutant_ids,omitempty"`
}

// SummaryRecord represents the aggregated mutation outcome metrics from selene_summary.
type SummaryRecord struct {
	TotalMutations int `json:"total_mutations"`
	Killed         int `json:"killed"`
	Survived       int `json:"survived"`
	Uncovered      int `json:"uncovered"`
	Timeouts       int `json:"timeouts"`
	SafetyExcluded int `json:"safety_excluded"`
}

// SchemaDDL defines the SQLite tables and diagnosis views for Selene.
const SchemaDDL = `
CREATE TABLE IF NOT EXISTS selene (
    id TEXT PRIMARY KEY,
    mutator TEXT NOT NULL,
    file TEXT NOT NULL,
    line INTEGER NOT NULL,
    col INTEGER NOT NULL,
    status TEXT NOT NULL,
    killed_by TEXT
);

CREATE TABLE IF NOT EXISTS selene_tests (
    test_name TEXT PRIMARY KEY,
    package TEXT NOT NULL,
    status TEXT NOT NULL,
    mutations_killed INTEGER NOT NULL,
    killed_mutant_ids TEXT
);

DROP VIEW IF EXISTS selene_survived;
CREATE VIEW selene_survived AS
    SELECT id, mutator, file, line, col FROM selene WHERE status = 'survived';

DROP VIEW IF EXISTS selene_excluded;
CREATE VIEW selene_excluded AS
    SELECT id, mutator, file, line, col, killed_by as reason FROM selene WHERE status = 'excluded';

DROP VIEW IF EXISTS selene_zero_kill_tests;
CREATE VIEW selene_zero_kill_tests AS
    SELECT test_name, package FROM selene_tests WHERE mutations_killed = 0;

DROP VIEW IF EXISTS selene_summary;
CREATE VIEW selene_summary AS
    SELECT 
        count(*) as total_mutations,
        sum(case when status = 'killed' then 1 else 0 end) as killed,
        sum(case when status = 'survived' then 1 else 0 end) as survived,
        sum(case when status = 'uncovered' then 1 else 0 end) as uncovered,
        sum(case when status = 'timeout' then 1 else 0 end) as timeouts,
        sum(case when status = 'excluded' then 1 else 0 end) as safety_excluded
    FROM selene;
`

// InitDB opens a SQLite database and initializes the Selene tables and views.
func InitDB(dbPath string) (*sql.DB, error) {
	if dbPath != ":memory:" && !strings.HasPrefix(dbPath, "file:") {
		dir := filepath.Dir(dbPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create directory for database: %w", err)
			}
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if _, err := db.Exec(SchemaDDL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return db, nil
}

// WriteResults inserts or updates mutation records and test records inside a single SQLite transaction.
func WriteResults(db *sql.DB, mutations []MutationRecord, tests []TestRecord) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if len(mutations) > 0 {
		stmtMut, err := tx.Prepare(`
			INSERT OR REPLACE INTO selene (id, mutator, file, line, col, status, killed_by)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return fmt.Errorf("failed to prepare mutation statement: %w", err)
		}
		defer stmtMut.Close()

		for _, m := range mutations {
			killedBy := strings.Join(m.KilledBy, ",")
			if _, err := stmtMut.Exec(m.ID, m.Mutator, m.File, m.Line, m.Col, m.Status, killedBy); err != nil {
				return fmt.Errorf("failed to insert mutation record %s: %w", m.ID, err)
			}
		}
	}

	if len(tests) > 0 {
		stmtTest, err := tx.Prepare(`
			INSERT OR REPLACE INTO selene_tests (test_name, package, status, mutations_killed, killed_mutant_ids)
			VALUES (?, ?, ?, ?, ?)
		`)
		if err != nil {
			return fmt.Errorf("failed to prepare test statement: %w", err)
		}
		defer stmtTest.Close()

		for _, t := range tests {
			killedIDs := strings.Join(t.KilledMutantIDs, ",")
			if _, err := stmtTest.Exec(t.TestName, t.Package, t.Status, t.MutationsKilled, killedIDs); err != nil {
				return fmt.Errorf("failed to insert test record %s: %w", t.TestName, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// SaveResultsToDB opens the SQLite database at dbPath, executes the batch insert, and closes the database.
func SaveResultsToDB(dbPath string, mutations []MutationRecord, tests []TestRecord) error {
	db, err := InitDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	return WriteResults(db, mutations, tests)
}

// QuerySummary executes a query on selene_summary and returns the aggregated metrics.
func QuerySummary(db *sql.DB) (*SummaryRecord, error) {
	row := db.QueryRow("SELECT total_mutations, killed, survived, uncovered, timeouts, safety_excluded FROM selene_summary")
	var s SummaryRecord
	var killed, survived, uncovered, timeouts, safetyExcluded sql.NullInt64
	if err := row.Scan(&s.TotalMutations, &killed, &survived, &uncovered, &timeouts, &safetyExcluded); err != nil {
		return nil, fmt.Errorf("failed to query selene_summary: %w", err)
	}
	if killed.Valid {
		s.Killed = int(killed.Int64)
	}
	if survived.Valid {
		s.Survived = int(survived.Int64)
	}
	if uncovered.Valid {
		s.Uncovered = int(uncovered.Int64)
	}
	if timeouts.Valid {
		s.Timeouts = int(timeouts.Int64)
	}
	if safetyExcluded.Valid {
		s.SafetyExcluded = int(safetyExcluded.Int64)
	}
	return &s, nil
}

// QuerySurvived queries the selene_survived view.
func QuerySurvived(db *sql.DB) ([]MutationRecord, error) {
	rows, err := db.Query("SELECT id, mutator, file, line, col FROM selene_survived")
	if err != nil {
		return nil, fmt.Errorf("failed to query selene_survived: %w", err)
	}
	defer rows.Close()

	var records []MutationRecord
	for rows.Next() {
		var r MutationRecord
		if err := rows.Scan(&r.ID, &r.Mutator, &r.File, &r.Line, &r.Col); err != nil {
			return nil, fmt.Errorf("failed to scan selene_survived row: %w", err)
		}
		r.Status = "survived"
		records = append(records, r)
	}
	return records, nil
}

// QueryExcluded queries the selene_excluded view.
func QueryExcluded(db *sql.DB) ([]MutationRecord, error) {
	rows, err := db.Query("SELECT id, mutator, file, line, col, reason FROM selene_excluded")
	if err != nil {
		return nil, fmt.Errorf("failed to query selene_excluded: %w", err)
	}
	defer rows.Close()

	var records []MutationRecord
	for rows.Next() {
		var r MutationRecord
		var reason sql.NullString
		if err := rows.Scan(&r.ID, &r.Mutator, &r.File, &r.Line, &r.Col, &reason); err != nil {
			return nil, fmt.Errorf("failed to scan selene_excluded row: %w", err)
		}
		r.Status = "excluded"
		if reason.Valid {
			r.KilledBy = []string{reason.String}
		}
		records = append(records, r)
	}
	return records, nil
}

// QueryZeroKillTests queries the selene_zero_kill_tests view.
func QueryZeroKillTests(db *sql.DB) ([]TestRecord, error) {
	rows, err := db.Query("SELECT test_name, package FROM selene_zero_kill_tests")
	if err != nil {
		return nil, fmt.Errorf("failed to query selene_zero_kill_tests: %w", err)
	}
	defer rows.Close()

	var records []TestRecord
	for rows.Next() {
		var r TestRecord
		if err := rows.Scan(&r.TestName, &r.Package); err != nil {
			return nil, fmt.Errorf("failed to scan selene_zero_kill_tests row: %w", err)
		}
		r.Status = "zero_kills"
		records = append(records, r)
	}
	return records, nil
}

// Database provides a high-level client for Selene's SQLite database operations.
type Database struct {
	path string
}

// NewDatabase constructs a Database handler for the given SQLite file path.
func NewDatabase(path string) *Database {
	return &Database{path: path}
}

// WriteResults writes mutation and test records to the database (alias for SaveResults).
func (d *Database) WriteResults(mutations []MutationRecord, tests []TestRecord) error {
	return d.SaveResults(mutations, tests)
}

// LoadTestCoverage loads statement-level coverage from the test_coverage table.
func (d *Database) LoadTestCoverage() (TestIndex, error) {
	return LoadTestIndex(d.path)
}

// SaveResults persists mutation outcomes and test records to the database.
func (d *Database) SaveResults(mutations []MutationRecord, tests []TestRecord) error {
	return SaveResultsToDB(d.path, mutations, tests)
}

// QuerySummary retrieves summary metrics from the selene_summary view.
func (d *Database) QuerySummary() (*SummaryRecord, error) {
	db, err := InitDB(d.path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return QuerySummary(db)
}

// QuerySurvived retrieves all surviving mutation records from the selene_survived view.
func (d *Database) QuerySurvived() ([]MutationRecord, error) {
	db, err := InitDB(d.path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return QuerySurvived(db)
}

// QueryZeroKillTests retrieves all non-assertive tests from the selene_zero_kill_tests view.
func (d *Database) QueryZeroKillTests() ([]TestRecord, error) {
	db, err := InitDB(d.path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return QueryZeroKillTests(db)
}
