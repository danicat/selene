package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
}

type Report struct {
	Total         int
	Killed        int
	Survived      int
	Uncovered     int
	BuildFailures int
}

func (r Report) Score() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Killed) / float64(r.Total) * 100
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

// Run executes the mutation testing process.
func Run(patterns []string, config Config) (*Report, error) {

	if len(patterns) == 0 {
		return nil, fmt.Errorf("no patterns provided")
	}

	if !config.Verbose {
		log.SetOutput(io.Discard)
	} else {
		log.SetOutput(os.Stderr)
	}

	// 1. Path Expansion (Faithfully ported from legacy run.go)
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

	// 2. Generate Coverage
	log.Println("generating coverage profile...")
	coverFile := filepath.Join(config.MutationDir, "coverage.out")

	// Determine the module root to run tests correctly
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

	// 3. Process Files
	report := &Report{}
	overlayPath := filepath.Join(config.MutationDir, "overlay.json")

	for _, filename := range filenames {
		log.Printf("processing file: %s", filename)
		pf, err := parseFile(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file %s: %w", filename, err)
		}

		ast.Inspect(pf.File, func(n ast.Node) bool {
			for _, m := range config.Mutators {
				muts := m.Check(n)
				for _, mut := range muts {
					report.Total++

					// Check coverage
					pos := pf.Fset.Position(mut.Pos)
					mutID := fmt.Sprintf("%s-%s:%d:%d", mut.ID, filename, pos.Line, pos.Column)
					if !coverage.IsCovered(filename, pos.Line) {
						report.Uncovered++
						fmt.Printf("%s: uncovered\n", mutID)
						continue
					}

					// Apply and Test
					mut.Apply()

					mutatedFile := filepath.Join(config.MutationDir, filepath.Base(filename))
					if err := writeAST(mutatedFile, pf.Fset, pf.File); err != nil {
						log.Printf("failed to write mutated file: %s", err)
						mut.Revert()
						continue
					}

					absOrig, _ := filepath.Abs(filename)
					overlays := map[string]string{absOrig: mutatedFile}
					if err := createOverlayFile(overlayPath, overlays); err != nil {
						log.Printf("failed to create overlay: %s", err)
						mut.Revert()
						continue
					}

					pkgDir := filepath.Dir(absOrig)
					events, err := runGoTest(pkgDir, overlayPath)

					status := "survived"
					if err != nil {
						status = "killed"
						report.Killed++
						report.BuildFailures++ // treating execution error as build failure for now
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
							report.Killed++
						} else {
							report.Survived++
						}
					}

					fmt.Printf("%s: %s\n", mutID, status)
					mut.Revert()

				}
			}
			return true
		})
	}

	return report, nil
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

func runGoTest(pkgDir, overlay string) ([]TestEvent, error) {
	cmd := exec.Command("go", "test", "--json", "--overlay", overlay, ".")
	cmd.Dir = pkgDir
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, err
	}
	return parseGoTestOutput(out)
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
