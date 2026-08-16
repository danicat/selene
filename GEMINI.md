# Selene - Gemini CLI Development & TestQuery Notes

This document provides architectural guidance, workflow instructions, and database reference queries for developers and Gemini CLI agents working on [Selene](file:///Users/petruzalek/projects/selene/README.md) and its integration with [TestQuery (`tq`)](file:///Users/petruzalek/projects/selene/GEMINI.md).

---

## 🔄 TestQuery (`tq`) & Selene Synergy

TestQuery (`tq`) and Selene work hand-in-hand to provide ultra-fast, database-driven mutation testing:

```
+------------------+         +--------------------+         +--------------------+
|  Go Test Suite   |  tq     |   testquery.db     |  selene |  Targeted Mutants  |
|  go test ./...   | ------> |  (test_coverage)   | ------> |  -run '^(TestA)$'  |
+------------------+ build   +--------------------+  --db   +--------------------+
                                      ^                               |
                                      | Writes Results                |
                                      +-------------------------------+
                                        (selene, selene_tests, views)
```

1. **Coverage Profiling**: `tq build ./...` analyzes the codebase and populates `testquery.db` with detailed, statement-level test execution coverage (`test_coverage` table).
2. **Targeted Mutation Testing**: Selene reads `testquery.db` via `--db testquery.db` and indexes which tests cover each line in memory (`TestIndex`). When evaluating a mutant on a specific line, Selene executes **only the tests that cover that line** using `-run '^(TestA|TestB)$'`.
3. **Results Persistence**: Selene writes all mutant outcomes and test effectiveness rankings directly back into SQLite, creating tables (`selene`, `selene_tests`) and diagnostic views (`selene_survived`, `selene_bad_tests`, `selene_summary`).

---

## 🛠️ Typical Development Workflow

### 1. Build Coverage Database
Run `tq build` from the repository root:
```bash
tq build ./...
```
*(Or target a specific package: `tq build ./internal/mutator`)*

### 2. Run Selene in Targeted Fast-Mode
```bash
selene --db testquery.db -workers 8 -v ./...
```

### 3. Diagnose Surviving Mutants & Weak Tests
Use `tq query` or SQLite directly to inspect results:
```bash
tq query "SELECT * FROM selene_survived"
tq query "SELECT * FROM selene_zero_kill_tests"
```

---

## 📊 Practical SQL Query Examples

### 1. Identify Surviving Mutants (Test Coverage Gaps)
Find all code mutants that survived test execution:
```sql
SELECT id, mutator, file, line, col 
FROM selene 
WHERE status = 'survived'
ORDER BY file, line;
```
Or use the pre-built view:
```sql
SELECT * FROM selene_survived;
```

### 2. Identify Zero-Kill (Unproven) Tests
List all tests that executed during test runs but caught 0 mutations in this run:
```sql
SELECT test_name, package 
FROM selene_zero_kill_tests;
```
*(Note: `selene_bad_tests` is maintained as a backward-compatible alias view).*

### 3. Top Most Effective Tests
Rank tests by the number of mutants they successfully killed:
```sql
SELECT test_name, package, mutations_killed, killed_mutant_ids 
FROM selene_tests 
WHERE mutations_killed > 0
ORDER BY mutations_killed DESC 
LIMIT 10;
```

### 4. Overall Session Summary
Query aggregated mutation testing statistics:
```sql
SELECT * FROM selene_summary;
```

### 5. Check Line Coverage for a Function / File
Verify which tests execute a given line in the codebase:
```sql
SELECT test_name, count 
FROM test_coverage 
WHERE file LIKE '%expression.go' 
  AND start_line <= 42 
  AND end_line >= 42;
```

### 6. Correlate Surviving Mutants with Line Execution Counts
```sql
SELECT s.id, s.file, s.line, s.mutator, tc.test_name, tc.count
FROM selene s
LEFT JOIN test_coverage tc 
  ON s.file = tc.file 
 AND s.line >= tc.start_line 
 AND s.line <= tc.end_line
WHERE s.status = 'survived';
```

---

## 🏗️ Architecture & Internal Mechanisms

### In-Memory Test Indexing (`TestIndex`)
When `--db` is passed, [`internal/runner`](file:///Users/petruzalek/projects/selene/internal/runner) loads all `(file, start_line, end_line, test_name)` tuples into a thread-safe in-memory interval index:
* **Lookup Complexity**: $O(\log N)$ or fast hash table lookups per AST node.
* **Target Filter Builder**: Generates `-run '^(TestFoo|TestBar)$'` flags for `go test`. If no covering tests are recorded (e.g. uncovered line), test execution is skipped entirely.

### Overlay Compilation
Selene uses Go's native compilation overlay feature:
1. AST mutation is written to a worker scratch directory: `/tmp/mutation.../worker-1/file.go`.
2. An `overlay.json` manifest is written:
   ```json
   {
     "Replace": {
       "/path/to/original/file.go": "/tmp/mutation.../worker-1/file.go"
     }
   }
   ```
3. Test is run with `go test -overlay overlay.json .`.
4. **Benefit**: Instant compilation without touching working tree files on disk or invalidating build caches.

### Process Group Isolation & Timeout Management
To handle infinite loops caused by mutations (e.g., condition boundary flips in `for` loops):
* Tests are spawned with a dedicated process group (`Setpgid: true`).
* If the context expires (`-timeout`), a `SIGKILL` is sent to the negative PID (`syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)`), immediately terminating the entire process group and all child test runners.

---

## 💡 Strategic Tips for Gemini CLI Agents
* **Module Root Alignment**: Always execute commands from the directory containing `go.mod` or ensure package paths are resolved using `findModuleRoot`.
* **Database Verification**: If `tq query` returns empty results, run `PRAGMA table_info(selene)` or `PRAGMA table_info(test_coverage)` to verify table structures.
* **Path Matching**: SQLite paths in `test_coverage` may be relative to the module root or absolute. Selene automatically matches filename suffixes to handle path variations cleanly.
