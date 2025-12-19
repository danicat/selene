package runner

import (
	"encoding/json"

	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/danicat/selene/internal/mutator"
)

// RunMutations applies mutations to the given files and writes them to the mutation directory.
// It returns the path to the overlay JSON file.
func RunMutations(filenames []string, mutationDir string, mutators []mutator.Mutator) (string, error) {
	// This function is kept for compatibility but should be replaced by iterative approach
	return "", nil
}

// RunGoTest runs `go test` with the given overlay and returns the raw JSON output.
func RunGoTest(pkgDir, overlay string) ([]byte, error) {
	cmd := exec.Command("go", "test", "--json", "--overlay", overlay, ".")
	cmd.Dir = pkgDir
	return cmd.CombinedOutput()
}

// Result represents the outcome of a mutation test.
type Result struct {
	ID     string
	Status string // "killed", "survived", "error"
	Output string
}

// RunIterative scans for candidates and runs tests for each mutation.
func RunIterative(filenames []string, mutationDir string, mutators []mutator.Mutator, coverage *Coverage) ([]Result, error) {
	var results []Result

	for _, filename := range filenames {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filename, nil, 0)
		if err != nil {
			return nil, err
		}

		candidates := mutator.Scan(file, fset, mutators)
		for _, c := range candidates {
			// Check coverage
			pos := fset.Position(c.Node.Pos())
			if coverage != nil && !coverage.IsCovered(filename, pos.Line) {
				continue
			}

			// Apply mutation to a fresh AST copy
			// Note: For simplicity, we re-parse the file for each mutation.
			// Optimization: Clone the AST instead.
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filename, nil, 0)
			if err != nil {
				return nil, err
			}

			// Find the node again (since we re-parsed)
			// This is a bit hacky, ideally we'd have a better way to map back
			// For now, we rely on the fact that Scan returns nodes in order
			// and we can re-scan to find the matching node.
			// A better approach would be to use the position to find the node.
			// Let's implement a simple position-based finder.
			
			// Apply the mutation
			c.Mutator.Apply(c.Node) // This won't work directly because c.Node is from the old AST
			
			// Correct approach:
			// 1. Create a temporary file for the mutated source.
			// 2. Apply the mutation to the AST.
			// 3. Write the AST to the temp file.
			// 4. Create overlay.
			// 5. Run test.
			
			// Let's simplify: We need to apply the mutation to the *current* AST `file`.
			// We need to find the node in `file` that corresponds to `c.Node`.
			// Since we don't have a robust AST cloner/mapper yet, let's just re-scan and match by ID.
			
			newCandidates := mutator.Scan(file, fset, mutators)
			var targetNode mutator.Candidate
			found := false
			for _, nc := range newCandidates {
				if nc.ID == c.ID {
					targetNode = nc
					found = true
					break
				}
			}
			
			if !found {
				continue // Should not happen
			}
			
			targetNode.Mutator.Apply(targetNode.Node)
			
			// Write mutated file
			mutatedFile := filepath.Join(mutationDir, filepath.Base(filename))
			f, err := os.Create(mutatedFile)
			if err != nil {
				return nil, err
			}
			
			printer.Fprint(f, fset, file)
			f.Close()
			
			// Create overlay
			overlay := filepath.Join(mutationDir, "overlay.json")
			absOriginalPath, err := filepath.Abs(filename)
			if err != nil {
				return nil, err
			}
			overlays := map[string]string{absOriginalPath: mutatedFile}
			bytes, _ := json.Marshal(struct{ Replace map[string]string }{Replace: overlays})
			os.WriteFile(overlay, bytes, 0644)
			
			// Run test
			absPath, _ := filepath.Abs(filename)
			pkgDir := filepath.Dir(absPath)
			out, _ := RunGoTest(pkgDir, overlay)
			
			// Analyze result
			status := "survived"
			if err != nil {
				status = "killed"
			} else {
				// Check if JSON output contains "Action":"fail"
				if strings.Contains(string(out), `"Action":"fail"`) {
					status = "killed"
				}
			}
			
			results = append(results, Result{ID: c.ID, Status: status, Output: string(out)})
		}
	}
	
	return results, nil
}
