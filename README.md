# Selene

Selene is a high-performance mutation testing tool for Go. It helps you verify the quality and rigor of your test suite by introducing targeted syntactic mutations (faults) into your Go source code and checking whether your tests fail (kill the mutant).

If a test suite passes despite code mutations, those surviving mutants indicate gaps in test assertions, edge-case handling, or missing test coverage.

---

## ⚡ How It Works

1. **AST Parsing & Mutation**: Selene parses target Go source files into Abstract Syntax Trees (AST) using Go's standard `go/parser` and `go/ast` packages.
2. **Coverage Filtering & Targeted Selection**: Selene analyzes code coverage (from `go test -coverprofile` or via [TestQuery](file:///Users/petruzalek/projects/selene/GEMINI.md) `--db`) and skips mutating uncovered lines, saving testing time.
3. **In-Memory & Overlay Compilation**: Mutated files are written to isolated worker scratch directories and mapped using Go's native `-overlay` compiler flag (`go test -overlay=overlay.json`). Source code on disk is never modified.
4. **Concurrent Execution & Process Group Isolation**: Worker goroutines execute test runs in parallel. If a mutant causes an infinite loop, Selene terminates the entire process group cleanly via `SIGKILL` upon reaching the per-mutation `-timeout`.
5. **Results & Test Quality Aggregation**: Selene records all mutation outcomes, maps kills to individual tests and subtests, and persists detailed results to an SQLite database when configured.

---

## 📦 Installation

Install the latest binary using `go install`:

```bash
go install github.com/danicat/selene/cmd/selene@latest
```

Or build directly from source:

```bash
git clone https://github.com/danicat/selene.git
cd selene
go build -o selene ./cmd/selene
```

---

## 🚀 Usage & Supported Path Syntaxes

Selene supports flexible target path resolution powered by `go list`:

```bash
# 1. Single file or multiple files
selene main.go utils.go

# 2. Package directory
selene ./internal/mutator

# 3. Current package
selene .

# 4. Recursive package expansion (all subpackages)
selene ./...

# 5. Fully-qualified Go module import path
selene github.com/danicat/selene/internal/mutator

# 6. Targeted fast-mode using TestQuery SQLite DB
selene --db testquery.db -workers 8 ./...
```

---

## ⚙️ CLI Flags Reference

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `-workers` | `int` | `runtime.NumCPU()` | Number of parallel worker goroutines executing mutations concurrently. |
| `-timeout` | `duration` | `10s` | Maximum time allowed for a single mutant test run (e.g. `5s`, `500ms`). Mutants exceeding this are classified as `timeout` (killed). |
| `--db` | `string` | `""` | Path to TestQuery SQLite database (e.g. `testquery.db`). Enables targeted `-run` test filtering and persists results. |
| `-v` | `bool` | `false` | Enable verbose logging (mutations generated, line positions, killed status, covering tests). |
| `-json` | `bool` | `false` | Output results in structured JSON format for CI/CD pipelines. |
| `-output`, `--mutation-dir` | `string` | OS Temp Dir | Directory to store temporary mutated overlay files (can also be set via `GOMUTATION` env). |
| `-seed` | `int64` | Timestamp | Deterministic seed for randomization. |
| `-shuffle` | `bool` | `false` | Enable randomization of file and mutant processing order. |

---

## 🧬 Supported Mutator Classes

Selene includes 8 AST mutator classes implemented in [`internal/mutator`](file:///Users/petruzalek/projects/selene/internal/mutator):

### 1. [`ReverseIfCond`](file:///Users/petruzalek/projects/selene/internal/mutator/reverse_if_cond.go)
Reverses boolean expressions in `if` statements.
```go
// Original
if x > 0 { ... }
// Mutated
if !(x > 0) { ... }
```

### 2. [`ArithmeticMutator` / `SwapArithmetic`](file:///Users/petruzalek/projects/selene/internal/mutator/expression.go)
Swaps binary arithmetic operators:
* `+` $\longleftrightarrow$ `-`
* `*` $\longleftrightarrow$ `/`
```go
// Original
total := price + tax
// Mutated
total := price - tax
```

### 3. [`ComparisonMutator`](file:///Users/petruzalek/projects/selene/internal/mutator/expression.go)
Inverts standard comparison operators:
* `==` $\longleftrightarrow$ `!=`
* `<` $\longleftrightarrow$ `>=`
* `>` $\longleftrightarrow$ `<=`
```go
// Original
if count == expected { ... }
// Mutated
if count != expected { ... }
```

### 4. [`BooleanMutator`](file:///Users/petruzalek/projects/selene/internal/mutator/expression.go)
Swaps logical operators:
* `&&` $\longleftrightarrow$ `||`
```go
// Original
if isValid && isAuthorized { ... }
// Mutated
if isValid || isAuthorized { ... }
```

### 5. [`ConditionalsBoundaryMutator`](file:///Users/petruzalek/projects/selene/internal/mutator/boundary_incdec.go)
Relaxes or tightens relational boundary checks:
* `<` $\longleftrightarrow$ `<=`
* `>` $\longleftrightarrow$ `>=`
```go
// Original
if index < len(items) { ... }
// Mutated
if index <= len(items) { ... }
```

### 6. [`IncrementDecrementMutator`](file:///Users/petruzalek/projects/selene/internal/mutator/boundary_incdec.go)
Swaps increment and decrement unary statements:
* `++` $\longleftrightarrow$ `--`
```go
// Original
counter++
// Mutated
counter--
```

### 7. `LiteralMutator`
Mutates boolean, numeric, and string literals:
* `true` $\longleftrightarrow$ `false`
* `0` $\longleftrightarrow$ `1` (or $n \pm 1$)
* `""` $\longleftrightarrow$ `"mutated"`
```go
// Original
const enabled = true
// Mutated
const enabled = false
```

### 8. `AssignmentMutator`
Mutates compound assignment operators:
* `+=` $\longleftrightarrow$ `-=`
* `*=` $\longleftrightarrow$ `/=`
```go
// Original
balance += deposit
// Mutated
balance -= deposit
```

---

## 🗄️ TestQuery SQLite Integration (`--db`)

When the `--db <path>` flag is supplied (pointing to a TestQuery database `testquery.db`), Selene performs two key optimizations:

### 1. Targeted Sub-Second Test Execution
Instead of executing the entire test suite for each mutant, Selene looks up the covering tests for the exact mutated file and line from `testquery.db`'s `test_coverage` table. It constructs a targeted `-run '^(TestFoo|TestBar)$'` regex, yielding massive speedups (10x-50x on larger codebases).

### 2. Database Schema & Views
Selene automatically creates and populates the following tables and views in SQLite:

#### Table `selene`
Stores individual mutation records:
```sql
CREATE TABLE IF NOT EXISTS selene (
    id TEXT PRIMARY KEY,
    mutator TEXT NOT NULL,
    file TEXT NOT NULL,
    line INTEGER NOT NULL,
    col INTEGER NOT NULL,
    status TEXT NOT NULL,       -- 'killed', 'survived', 'uncovered', 'timeout', 'build_failure'
    killed_by TEXT              -- JSON array or comma-separated test names
);
```

#### Table `selene_tests`
Aggregates test effectiveness and catches:
```sql
CREATE TABLE IF NOT EXISTS selene_tests (
    test_name TEXT PRIMARY KEY,
    package TEXT NOT NULL,
    status TEXT NOT NULL,       -- 'good', 'bad'
    mutations_killed INTEGER NOT NULL,
    killed_mutant_ids TEXT      -- Comma-separated mutation IDs
);
```

#### Diagnostic Views
* `selene_survived`: Instant view of all surviving mutations requiring test improvements.
  ```sql
  SELECT id, mutator, file, line, col FROM selene WHERE status = 'survived';
  ```
* `selene_zero_kill_tests`: List of tests that ran but caught 0 mutations in the current run (alias: `selene_bad_tests`).
  ```sql
  SELECT test_name, package FROM selene_zero_kill_tests;
  ```
* `selene_summary`: High-level summary of killed, survived, uncovered, and timeout counts.
  ```sql
  SELECT * FROM selene_summary;
  ```

---

## 📊 Output Formats & Examples

### Standard Terminal Output

```bash
$ selene -workers 8 ./...

Total mutations: 28
Killed:          22
Timeouts:        2
Survived:        3
Uncovered:       1

Total tests:     15
Good tests:      12
Zero-kill tests: 3

Zero-kill tests (caught 0 mutations):
- TestUnrelatedHelper
- TestTrivialGetter
- TestMockLogger

Mutation Score:     85.71% (killed / total mutations)
Test Quality Score: 80.00% (good tests / total tests)
```

### Verbose Mode (`-v`)

```bash
$ selene -v -workers 4 ./internal/mutator
Arithmetic_1042-internal/mutator/expression.go:42:12: killed (Killed by: TestArithmeticMutator)
Comparison_1580-internal/mutator/expression.go:75:10: killed (Killed by: TestComparisonMutator)
ConditionalsBoundary_2104-internal/mutator/boundary_incdec.go:28:15: survived
ReverseIfCond_3340-internal/mutator/reverse_if_cond.go:20:8: killed (timeout)

Good tests details:
- TestArithmeticMutator (caught 4 mutations): Arithmetic_1042, Arithmetic_1080, ...
- TestComparisonMutator (caught 6 mutations): Comparison_1580, Comparison_1620, ...
```

### JSON Output (`-json`)

```bash
$ selene -json ./...
```
```json
{
  "total_mutations": 28,
  "killed": 22,
  "survived": 3,
  "timeouts": 2,
  "uncovered": 1,
  "total_tests": 15,
  "good_tests": [
    "TestArithmeticMutator",
    "TestComparisonMutator",
    "TestBoundaryMutator"
  ],
  "bad_tests": [
    "TestUnrelatedHelper",
    "TestTrivialGetter",
    "TestMockLogger"
  ],
  "mutation_score": 85.71,
  "test_quality_score": 80.00
}
```

---

## 💡 Metrics Explained

* **Mutation Score**: $\frac{\text{Killed} + \text{Timeouts}}{\text{Total Mutations}} \times 100\%$
  * Represents how thoroughly your test suite prevents regressions.
  * Uncovered mutants are counted in the total to provide an honest, uninflated metric.
* **Test Quality Score**: $\frac{\text{Good Tests}}{\text{Total Tests}} \times 100\%$
  * A "good test" caught at least one code mutant.
  * A "bad test" caught 0 mutants, indicating assertions may be weak, redundant, or missing.

---

## 🦇 Why Selene?

Selene is the [oldest known human mutant](https://en.wikipedia.org/wiki/Selene_(comics)) in Marvel comics. It is also the name of the greatest vampire protagonist in cinematic history.
