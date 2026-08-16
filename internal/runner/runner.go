package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/danicat/selene/internal/mutator"
)

type TestEvent struct {
	Time    time.Time
	Action  string
	Package string
	Test    string
	Elapsed float64
	Output  string
}

type ParsedFile struct {
	Path string
	File *ast.File
	Fset *token.FileSet
}

type Config struct {
	Verbose         bool
	MutationDir     string
	Mutators        []mutator.Mutator
	Workers         int
	Seed            int64
	Shuffle         bool
	Timeout         time.Duration
	DBPath          string
	Targeted        bool
	TestIndex       TestIndex
	AdaptiveTimeout bool
}

type Report struct {
	Total         int
	Killed        int
	Timeouts      int
	Survived      int
	Uncovered     int
	Excluded      int
	BuildFailures int
	TestKills     map[string][]string // test name -> list of mutation IDs
	ExecutedTests map[string]bool     // all executed test/subtest names
	ExcludedList  []ExcludedMutant    // analytics on excluded mutations
}

func (r Report) Score() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Killed+r.Timeouts) / float64(r.Total) * 100
}

// DefaultMutators returns all registered mutators.
func DefaultMutators() []mutator.Mutator {
	return []mutator.Mutator{
		&mutator.ReverseIfCond{},
		&mutator.ArithmeticMutator{},
		&mutator.ComparisonMutator{},
		&mutator.BooleanMutator{},
		&mutator.ConditionalsBoundaryMutator{},
		&mutator.IncrementDecrementMutator{},
		&mutator.BooleanLiteralMutator{},
		&mutator.IntegerLiteralMutator{},
		&mutator.StringLiteralMutator{},
		&mutator.AssignmentMutator{},
		&mutator.BitwiseMutator{},
	}
}

// findModuleRoot looks for the directory containing go.mod starting from dir.
func findModuleRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break
		}
		abs = parent
	}
	return dir
}

type mutationResult struct {
	status        string // "killed", "survived", "uncovered", "killed (timeout)", "build_failure", "excluded"
	mutID         string
	mutator       string
	filename      string
	line          int
	col           int
	reason        string
	buildFailures int
	killedBy      []string
	executed      []string
}

// Run executes the mutation testing process.
func Run(patterns []string, config Config) (*Report, error) {
	if len(patterns) == 0 {
		return nil, fmt.Errorf("no patterns provided")
	}

	if config.Workers <= 0 {
		config.Workers = runtime.NumCPU()
	}

	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Second
	}

	if config.Seed == 0 {
		config.Seed = time.Now().UnixNano()
	}
	r := rand.New(rand.NewSource(config.Seed))

	if len(config.Mutators) == 0 {
		config.Mutators = DefaultMutators()
	}

	if config.Verbose {
		fmt.Printf("Seed: %d\n", config.Seed)
		if config.Shuffle {
			fmt.Println("Shuffle: enabled")
		}
	}

	// 1. Path Resolution via ResolveTargets
	targets, filenames, err := ResolveTargets(patterns)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve targets: %w", err)
	}

	if len(filenames) == 0 {
		return nil, fmt.Errorf("no files found to mutate")
	}

	if config.Shuffle {
		r.Shuffle(len(filenames), func(i, j int) {
			filenames[i], filenames[j] = filenames[j], filenames[i]
		})
	}

	// Setup mutation directory if empty
	if config.MutationDir == "" {
		tmpDir, err := os.MkdirTemp("", "selene-run")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp mutation dir: %w", err)
		}
		config.MutationDir = tmpDir
		defer func() { _ = os.RemoveAll(tmpDir) }()
	} else {
		if err := os.MkdirAll(config.MutationDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create mutation dir: %w", err)
		}
	}

	// Load DB coverage for targeted execution if requested and not provided
	if config.Targeted && config.TestIndex == nil && config.DBPath != "" {
		db := NewDatabase(config.DBPath)
		testIndex, err := db.LoadTestCoverage()
		if err == nil && testIndex != nil {
			config.TestIndex = testIndex
		}
	}

	// 2. Generate Coverage
	if config.Verbose {
		log.Println("generating coverage profile...")
	}

	coverFile := filepath.Join(config.MutationDir, "coverage.out")
	firstAbs, _ := filepath.Abs(filenames[0])
	moduleRoot := findModuleRoot(filepath.Dir(firstAbs))
	coverCmd := exec.Command("go", "test", "-coverprofile="+coverFile, "./...")
	coverCmd.Dir = moduleRoot

	if out, err := coverCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("coverage generation failed: %s\n%s", err, out)
	}
	defer func() { _ = os.Remove(coverFile) }()

	coverage, err := LoadCoverage(coverFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load coverage: %w", err)
	}

	// 3. Mutation Pipeline
	type task struct {
		pf       *ParsedFile
		filename string
		mut      mutator.Mutation
		mutator  string
	}

	tasks := make(chan task, config.Workers*2)
	results := make(chan mutationResult, config.Workers*2)

	// Collector (Reducer)
	finalReport := make(chan *Report)
	var allMutationResults []mutationResult
	var mutResultsMu sync.Mutex

	go func() {
		report := &Report{
			TestKills:     make(map[string][]string),
			ExecutedTests: make(map[string]bool),
		}
		for res := range results {
			mutResultsMu.Lock()
			allMutationResults = append(allMutationResults, res)
			mutResultsMu.Unlock()

			for _, t := range res.executed {
				report.ExecutedTests[t] = true
			}

			report.Total++
			displayStatus := res.status
			switch res.status {
			case "killed":
				report.Killed++
				for _, test := range res.killedBy {
					report.TestKills[test] = append(report.TestKills[test], res.mutID)
				}
			case "killed (timeout)":
				report.Timeouts++
			case "survived":
				report.Survived++
			case "uncovered":
				report.Uncovered++
				displayStatus = "survived (uncovered)"
			case "excluded":
				report.Excluded++
				report.ExcludedList = append(report.ExcludedList, ExcludedMutant{
					MutantID: res.mutID,
					Mutator:  res.mutator,
					File:     res.filename,
					Line:     res.line,
					Col:      res.col,
					Reason:   res.reason,
				})
				displayStatus = fmt.Sprintf("safety-excluded (%s)", res.reason)
			}

			report.BuildFailures += res.buildFailures

			if config.Verbose {
				fmt.Printf("%s-%s:%d:%d: %s", res.mutID, res.filename, res.line, res.col, displayStatus)
				if len(res.killedBy) > 0 {
					fmt.Printf(" (Killed by: %s)", strings.Join(res.killedBy, ", "))
				}
				fmt.Println()
			}
		}
		finalReport <- report
	}()

	var astMu sync.Mutex

	// Workers
	var wg sync.WaitGroup
	for i := 0; i < config.Workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			workerDir := filepath.Join(config.MutationDir, fmt.Sprintf("worker-%d", workerID))
			if err := os.MkdirAll(workerDir, 0755); err != nil {
				log.Printf("worker %d failed to create dir: %v", workerID, err)
				return
			}
			defer func() { _ = os.RemoveAll(workerDir) }()

			overlayPath := filepath.Join(workerDir, "overlay.json")

			for t := range tasks {
				mutatedFile := filepath.Join(workerDir, filepath.Base(t.filename))
				var pos token.Position
				var writeErr error

				astMu.Lock()
				t.mut.Apply()
				pos = t.pf.Fset.Position(t.mut.Pos)
				writeErr = writeAST(mutatedFile, t.pf.Fset, t.pf.File)
				t.mut.Revert()
				astMu.Unlock()

				if writeErr != nil {
					if config.Verbose {
						log.Printf("failed to write mutated file: %s", writeErr)
					}
					continue
				}

				status := "survived"
				bFailures := 0
				var killedBy []string

				absOrig, _ := filepath.Abs(t.filename)
				overlays := map[string]string{absOrig: mutatedFile}
				if err := createOverlayFile(overlayPath, overlays); err != nil {
					if config.Verbose {
						log.Printf("failed to create overlay: %s", err)
					}
					continue
				}

				pkgDir := filepath.Dir(absOrig)

				// Targeted test filtering
				runFilter := ""
				if config.Targeted && config.TestIndex != nil {
					coveringTests := config.TestIndex.GetCoveringTests(t.filename, pos.Line)
					if len(coveringTests) > 0 {
						runFilter = BuildRunFilter(coveringTests)
					}
				}

				testTimeout := config.Timeout
				if config.AdaptiveTimeout && config.TestIndex != nil {
					expectedDuration := config.TestIndex.GetExpectedDuration(t.filename, pos.Line)
					testTimeout = CalculateAdaptiveTimeout(expectedDuration, config.Timeout)
				}

				ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
				events, err := runGoTest(ctx, pkgDir, overlayPath, runFilter)
				cancel()

				var executed []string
				if err != nil {
					if ctx.Err() == context.DeadlineExceeded {
						status = "killed (timeout)"
					} else {
						status = "killed"
						bFailures = 1
					}
				} else {
					killed := false
					for _, e := range events {
						if e.Test != "" {
							executed = append(executed, e.Test)
						}
						if e.Action == "fail" && e.Test != "" {
							killed = true
							killedBy = append(killedBy, e.Test)
						}
					}
					if killed {
						status = "killed"
					}
				}

				results <- mutationResult{
					status:        status,
					mutID:         t.mut.ID,
					mutator:       t.mutator,
					filename:      t.filename,
					line:          pos.Line,
					col:           pos.Column,
					buildFailures: bFailures,
					killedBy:      killedBy,
					executed:      executed,
				}
			}
		}(i)
	}

	// Generator (Producer)
	for _, filename := range filenames {
		if config.Verbose {
			log.Printf("processing file: %s", filename)
		}
		pf, err := parseFile(filename)

		if err != nil {
			if config.Verbose {
				log.Printf("failed to parse file %s: %v", filename, err)
			}
			continue
		}

		exclusions := mutator.BuildDestructiveExclusionSet(pf.File, pf.Fset)

		ast.Inspect(pf.File, func(n ast.Node) bool {
			if n == nil {
				return true
			}
			for _, m := range config.Mutators {
				muts := m.Check(n)
				for _, mut := range muts {
					pos := pf.Fset.Position(mut.Pos)

					// Check if this AST node is excluded for safety
					if reason, isExcluded := exclusions[n]; isExcluded {
						results <- mutationResult{
							status:   "excluded",
							mutID:    mut.ID,
							mutator:  m.Name(),
							filename: filename,
							line:     pos.Line,
							col:      pos.Column,
							reason:   reason,
							killedBy: []string{reason},
						}
						continue
					}

					if !coverage.IsCovered(filename, pos.Line) {
						results <- mutationResult{
							status:   "uncovered",
							mutID:    mut.ID,
							mutator:  m.Name(),
							filename: filename,
							line:     pos.Line,
							col:      pos.Column,
						}
						continue
					}
					tasks <- task{pf: pf, filename: filename, mut: mut, mutator: m.Name()}
				}
			}
			return true
		})
	}

	close(tasks)
	wg.Wait()
	close(results)
	rep := <-finalReport

	// Save to SQLite database if requested
	if config.DBPath != "" {
		db := NewDatabase(config.DBPath)
		var mutRecords []MutationRecord
		for _, r := range allMutationResults {
			dbStatus := r.status
			if dbStatus == "killed (timeout)" {
				dbStatus = "timeout"
			}
			mutRecords = append(mutRecords, MutationRecord{
				ID:       r.mutID,
				Mutator:  r.mutator,
				File:     r.filename,
				Line:     r.line,
				Col:      r.col,
				Status:   dbStatus,
				KilledBy: r.killedBy,
			})
		}

		// Collect discovered tests from targets and executed tests
		var discoveredTests []string
		for _, target := range targets {
			tests, err := DiscoverTests(target.Dir)
			if err == nil {
				discoveredTests = append(discoveredTests, tests...)
			}
		}
		for t := range rep.ExecutedTests {
			discoveredTests = append(discoveredTests, t)
		}

		stats := CalculateTestStats(discoveredTests, rep.TestKills)
		var testRecords []TestRecord
		pkgName := ""
		if len(targets) > 0 {
			pkgName = targets[0].ImportPath
		}
		for _, gt := range stats.GoodTests {
			testRecords = append(testRecords, TestRecord{
				TestName:        gt,
				Package:         pkgName,
				Status:          "good",
				MutationsKilled: len(stats.AggregatedKills[gt]),
				KilledMutantIDs: stats.AggregatedKills[gt],
			})
		}
		for _, zkt := range stats.ZeroKillTests {
			testRecords = append(testRecords, TestRecord{
				TestName:        zkt,
				Package:         pkgName,
				Status:          "zero_kills",
				MutationsKilled: 0,
			})
		}

		if err := db.SaveResults(mutRecords, testRecords); err != nil && config.Verbose {
			log.Printf("failed to save results to database %s: %v", config.DBPath, err)
		}
	}

	return rep, nil
}

func parseFile(filename string) (*ParsedFile, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	return &ParsedFile{Path: filename, File: file, Fset: fset}, nil
}

func writeAST(path string, fset *token.FileSet, file *ast.File) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	return printer.Fprint(f, fset, file)
}

func createOverlayFile(overlayPath string, overlays map[string]string) (err error) {
	type ov struct{ Replace map[string]string }
	data, err := json.Marshal(ov{Replace: overlays})
	if err != nil {
		return err
	}
	f, err := os.Create(overlayPath)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	_, err = f.Write(data)
	return err
}

func runGoTest(ctx context.Context, pkgDir, overlay, runFilter string) ([]TestEvent, error) {
	args := []string{"test", "-count=1", "--json", "--overlay", overlay}
	if runFilter != "" {
		args = append(args, "-run", runFilter)
	}
	args = append(args, ".")

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = pkgDir
	configureProcessGroup(cmd)

	var out []byte
	var err error

	done := make(chan error, 1)
	go func() {
		out, err = cmd.CombinedOutput()
		done <- err
	}()

	select {
	case <-ctx.Done():
		_ = killProcessGroup(cmd)
		return nil, ctx.Err()
	case <-done:
		if err != nil && len(out) == 0 {
			return nil, err
		}
		return parseGoTestOutput(out)
	}
}

func parseGoTestOutput(testOutput []byte) ([]TestEvent, error) {
	var tests []TestEvent
	decoder := json.NewDecoder(bytes.NewReader(testOutput))
	for {
		var event TestEvent
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			return tests, fmt.Errorf("error decoding json event: %w", err)
		}
		tests = append(tests, event)
	}
	return tests, nil
}
