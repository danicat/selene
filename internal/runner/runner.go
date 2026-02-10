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
	"syscall"
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
	Verbose     bool
	MutationDir string
	Mutators    []mutator.Mutator
	Workers     int
	Seed        int64
	Shuffle     bool
	Timeout     time.Duration
}

type Report struct {
	Total         int
	Killed        int
	Timeouts      int
	Survived      int
	Uncovered     int
	BuildFailures int
}

func (r Report) Score() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Killed+r.Timeouts) / float64(r.Total) * 100
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
	status        string // "killed", "survived", "uncovered"
	mutID         string
	filename      string
	line          int
	col           int
	buildFailures int
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

	fmt.Printf("Seed: %d\n", config.Seed)
	if config.Shuffle {
		fmt.Println("Shuffle: enabled")
	}

	// 1. Path Expansion
	var filenames []string
	for _, arg := range patterns {
		if strings.Contains(arg, "...") {
			out, err := exec.Command("go", "list", "-f", "{{range .GoFiles}}{{.}} {{end}}", arg).Output()
			if err != nil {
				filenames = append(filenames, arg)
				continue
			}
			dirOut, err := exec.Command("go", "list", "-f", "{{.Dir}}", arg).Output()
			if err != nil {
				filenames = append(filenames, arg)
				continue
			}
			dirs := strings.Split(strings.TrimSpace(string(dirOut)), "\n")
			fileLists := strings.Split(strings.TrimSpace(string(out)), "\n")
			for i, fileList := range fileLists {
				if i >= len(dirs) {
					break
				}
				dir := dirs[i]
				for _, f := range strings.Fields(fileList) {
					filenames = append(filenames, filepath.Join(dir, f))
				}
			}
		} else {
			filenames = append(filenames, arg)
		}
	}

	if len(filenames) == 0 {
		return nil, fmt.Errorf("no files found to mutate")
	}

	if config.Shuffle {
		r.Shuffle(len(filenames), func(i, j int) {
			filenames[i], filenames[j] = filenames[j], filenames[i]
		})
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
	}

	tasks := make(chan task, config.Workers*2)
	results := make(chan mutationResult, config.Workers*2)

	// Collector (Reducer)
	finalReport := make(chan *Report)
	go func() {
		report := &Report{}
		for res := range results {
			report.Total++
			displayStatus := res.status
			switch res.status {
			case "killed":
				report.Killed++
			case "killed (timeout)":
				report.Timeouts++
			case "survived":
				report.Survived++
			case "uncovered":
				report.Uncovered++
				displayStatus = "survived (uncovered)"
			}
			report.BuildFailures += res.buildFailures
			fmt.Printf("%s-%s:%d:%d: %s\n", res.mutID, res.filename, res.line, res.col, displayStatus)
		}
		finalReport <- report
	}()

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
				t.mut.Apply()
				pos := t.pf.Fset.Position(t.mut.Pos)
				mutatedFile := filepath.Join(workerDir, filepath.Base(t.filename))

				status := "survived"
				bFailures := 0

				if err := writeAST(mutatedFile, t.pf.Fset, t.pf.File); err != nil {
					if config.Verbose {
						log.Printf("failed to write mutated file: %s", err)
					}
					t.mut.Revert()
					continue
				}

				absOrig, _ := filepath.Abs(t.filename)
				overlays := map[string]string{absOrig: mutatedFile}
				if err := createOverlayFile(overlayPath, overlays); err != nil {
					if config.Verbose {
						log.Printf("failed to create overlay: %s", err)
					}
					t.mut.Revert()
					continue
				}

				pkgDir := filepath.Dir(absOrig)
				ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
				events, err := runGoTest(ctx, pkgDir, overlayPath)
				cancel()

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
						if e.Action == "fail" {
							killed = true
							break
						}
					}
					if killed {
						status = "killed"
					}
				}

				results <- mutationResult{
					status:        status,
					mutID:         t.mut.ID,
					filename:      t.filename,
					line:          pos.Line,
					col:           pos.Column,
					buildFailures: bFailures,
				}
				t.mut.Revert()
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

		ast.Inspect(pf.File, func(n ast.Node) bool {
			for _, m := range config.Mutators {
				muts := m.Check(n)
				for _, mut := range muts {
					pos := pf.Fset.Position(mut.Pos)
					if !coverage.IsCovered(filename, pos.Line) {
						results <- mutationResult{
							status:   "uncovered",
							mutID:    mut.ID,
							filename: filename,
							line:     pos.Line,
							col:      pos.Column,
						}
						continue
					}
					tasks <- task{pf: pf, filename: filename, mut: mut}
				}
			}
			return true
		})
	}

	close(tasks)
	wg.Wait()
	close(results)
	return <-finalReport, nil
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

func runGoTest(ctx context.Context, pkgDir, overlay string) ([]TestEvent, error) {
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "--json", "--overlay", overlay, ".")
	cmd.Dir = pkgDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var out []byte
	var err error

	done := make(chan error, 1)
	go func() {
		out, err = cmd.CombinedOutput()
		done <- err
	}()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			// Kill the entire process group
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
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
