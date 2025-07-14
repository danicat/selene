package mutator

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/tools/go/ast/astutil"
)

// FileContentProvider defines an interface for reading file content.
type FileContentProvider interface {
	ReadFile(filename string) ([]byte, error)
}

// RealFileContentProvider implements FileContentProvider using os.ReadFile.
type RealFileContentProvider struct{}

// ReadFile reads a file from the actual file system.
func (RealFileContentProvider) ReadFile(filename string) ([]byte, error) {
	return os.ReadFile(filename)
}

// FileSystem defines an interface for file system operations, allowing for mocking in tests.
type FileSystem interface {
	MkdirAll(path string, perm os.FileMode) error
	Create(name string) (io.WriteCloser, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	Abs(path string) (string, error)
	IsNotExist(err error) bool
}

// RealFileSystem implements FileSystem using the os package.
type RealFileSystem struct{}

// MkdirAll calls os.MkdirAll.
func (RealFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// Create calls os.Create.
func (RealFileSystem) Create(name string) (io.WriteCloser, error) {
	return os.Create(name)
}

// WriteFile calls os.WriteFile.
func (RealFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

// Abs calls filepath.Abs.
func (RealFileSystem) Abs(path string) (string, error) {
	return filepath.Abs(path)
}

// IsNotExist calls os.IsNotExist.
func (RealFileSystem) IsNotExist(err error) bool {
	return os.IsNotExist(err)
}

// Mutator handles the mutation of Go source files.
type Mutator struct {
	fs  FileSystem
	fcp FileContentProvider
}

// New creates a new Mutator with the given FileSystem and FileContentProvider.
func New(fs FileSystem, fcp FileContentProvider) *Mutator {
	return &Mutator{fs: fs, fcp: fcp}
}

// CreateMutations processes the given Go source files, creates mutated versions,
// and returns the path to an overlay JSON file for use with `go test`.
// mutationDir is the directory where mutated files and the overlay file will be stored.
func (m *Mutator) CreateMutations(filenames []string, mutationDir string) (string, error) {
	if err := m.fs.MkdirAll(mutationDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create mutation directory %s: %w", mutationDir, err)
	}

	overlays := make(map[string]string)
	for _, filename := range filenames {
		content, err := m.fcp.ReadFile(filename)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %w", filename, err)
		}

		absOriginalPath, err := m.fs.Abs(filename)
		if err != nil {
			return "", fmt.Errorf("failed to get absolute path for %s: %w", filename, err)
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filename, content, 0)
		if err != nil {
			return "", fmt.Errorf("failed to parse file %s (content length %d): %w", filename, len(content), err)
		}

		// Apply mutations
		astutil.Apply(file, nil, reverseIfCond)

		mutatedFileName := filepath.Base(filename) // Keep original base name for mutated file
		// Ensure mutated file path is unique if multiple source files have the same base name
		// (e.g. src/a/foo.go and src/b/foo.go). A simple way is to include part of the original path.
		// However, for simplicity and current tool behavior, let's stick to Base.
		// A more robust solution might involve hashing or creating subdirs in mutationDir.
		// For now, the overlay map uses absolute original paths, so that part is fine.
		mutatedFilePath := filepath.Join(mutationDir, mutatedFileName)

		outFile, err := m.fs.Create(mutatedFilePath)
		if err != nil {
			return "", fmt.Errorf("failed to create mutated file %s: %w", mutatedFilePath, err)
		}

		err = printer.Fprint(outFile, fset, file)
		// It's important to check the error from Close separately.
		closeErr := outFile.Close()
		if err != nil { // Error from Fprint
			return "", fmt.Errorf("failed to print mutated code to %s: %w", mutatedFilePath, err)
		}
		if closeErr != nil { // Error from Close
			return "", fmt.Errorf("failed to close mutated file %s: %w", mutatedFilePath, closeErr)
		}
		overlays[absOriginalPath] = mutatedFilePath
	}

	type overlayJSON struct {
		Replace map[string]string
	}

	overlayData, err := json.Marshal(overlayJSON{Replace: overlays})
	if err != nil {
		return "", fmt.Errorf("failed to marshal overlay JSON: %w", err)
	}

	overlayFilePath := filepath.Join(mutationDir, "overlay.json")
	if err := m.fs.WriteFile(overlayFilePath, overlayData, 0644); err != nil {
		return "", fmt.Errorf("failed to write overlay JSON to %s: %w", overlayFilePath, err)
	}

	return overlayFilePath, nil
}

// reverseIfCond is an astutil.ApplyFunc that reverses binary conditions in if statements.
// For example, `if a == b` becomes `if !(a == b)`.
func reverseIfCond(c *astutil.Cursor) bool {
	node := c.Node()
	ifStmt, ok := node.(*ast.IfStmt)
	if !ok {
		return true
	}

	// Only apply to simple binary expressions for now to keep it similar to original.
	// Could be expanded to handle more complex conditions.
	if _, ok := ifStmt.Cond.(*ast.BinaryExpr); ok {
		newCond := &ast.UnaryExpr{
			OpPos: ifStmt.Cond.Pos(), // Keep original position if possible
			Op:    token.NOT,
			X:     ifStmt.Cond,
		}
		ifStmt.Cond = newCond
	}
	return true
}
