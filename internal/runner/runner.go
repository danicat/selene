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
	"time"

	"github.com/danicat/selene/internal/mutator"
)

type TestEvent struct {
	Time    time.Time // encodes as an RFC3339-format string
	Action  string
	Package string
	Test    string
	Elapsed float64 // seconds
	Output  string
}

// ParsedFile holds the AST and FileSet for a parsed Go file.
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
	Caught        int
	Uncaught      int
	BuildFailures int
}

func (r Report) Score() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Caught) / float64(r.Total) * 100
}

// Run executes the mutation testing process.
func Run(filenames []string, config Config) (*Report, error) {
	if len(filenames) == 0 {
		return nil, fmt.Errorf("no filenames provided")
	}

	if !config.Verbose {

		log.SetOutput(io.Discard)
	} else {
		log.SetOutput(os.Stderr)
	}

	absPath, err := filepath.Abs(filenames[0])
	if err != nil {
		return nil, err
	}
	pkgDir := filepath.Dir(absPath)

	log.Println("parsing files...")
	var parsedFiles []*ParsedFile
	for _, filename := range filenames {
		pf, err := parseFile(filename)
		if err != nil {
			return nil, fmt.Errorf("failed to parse file %s: %w", filename, err)
		}
		parsedFiles = append(parsedFiles, pf)
	}

	log.Println("discovering mutations...")
	type MutationTask struct {
		Mutation mutator.Mutation
		File     *ParsedFile
	}
	var tasks []MutationTask

	for _, pf := range parsedFiles {
		ast.Inspect(pf.File, func(n ast.Node) bool {
			for _, m := range config.Mutators {
				mutations := m.Check(n)
				for _, mutation := range mutations {
					tasks = append(tasks, MutationTask{
						Mutation: mutation,
						File:     pf,
					})
				}
			}
			return true
		})
	}

	report := &Report{Total: len(tasks)}
	if report.Total == 0 {
		return report, nil
	}

	fmt.Printf("Found %d mutation candidates.\n", report.Total)

	overlayPath := filepath.Join(config.MutationDir, "overlay.json")

	for i, task := range tasks {
		fmt.Printf("[%d/%d] Applying %s at %s... ", i+1, report.Total, task.Mutation.ID, task.File.Path)

		// Apply mutation
		task.Mutation.Apply()

		// Write mutated file to temp
		mutatedFilename := filepath.Base(task.File.Path)
		mutatedFilePath := filepath.Join(config.MutationDir, mutatedFilename)
		err := writeAST(mutatedFilePath, task.File.Fset, task.File.File)
		if err != nil {
			log.Printf("failed to write mutated file: %s", err)
			task.Mutation.Revert()
			continue
		}

		// Create overlay map
		overlays := map[string]string{
			task.File.Path: mutatedFilePath,
		}

		err = createOverlayFile(overlayPath, overlays)
		if err != nil {
			log.Printf("failed to create overlay: %s", err)
			task.Mutation.Revert()
			continue
		}

		// Run tests
		tests, err := runGoTest(pkgDir, overlayPath)
		if err != nil {
			// Build failure or similar
			report.BuildFailures++
			report.Caught++
			fmt.Println("CAUGHT (Build Failure)")
		} else {
			caught := false
			for _, t := range tests {
				if t.Action == "fail" {
					caught = true
					break
				}
			}

			if caught {
				report.Caught++
				fmt.Println("CAUGHT")
			} else {
				report.Uncaught++
				fmt.Println("NOT CAUGHT")
			}
		}

		// Revert mutation for next run
		task.Mutation.Revert()
	}

	return report, nil
}

// parseFile parses a single Go file.
func parseFile(filename string) (*ParsedFile, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	return &ParsedFile{
		Path: filename,
		File: file,
		Fset: fset,
	}, nil
}

// writeAST writes the AST to the specified file path.
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

// createOverlayFile creates the JSON overlay file required by 'go test -overlay'.
func createOverlayFile(overlayPath string, overlays map[string]string) (err error) {
	type ov struct {
		Replace map[string]string
	}
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
	args := []string{"test", "--json", "--overlay", overlay, pkgDir}
	cmd := exec.Command("go", args...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		// Only return error if we really can't parse output or something catastrophic happened
		// But checking if out is empty might be useful
		if len(out) == 0 {
			return nil, err
		}
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
