package mutator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/ast/astutil"
)

// MockFileSystem is a mock implementation of the FileSystem interface for testing.
type MockFileSystem struct {
	MkdirAllFunc   func(path string, perm os.FileMode) error
	CreateFunc     func(name string) (io.WriteCloser, error)
	WriteFileFunc  func(name string, data []byte, perm os.FileMode) error
	AbsFunc        func(path string) (string, error)
	IsNotExistFunc func(err error) bool
	CreatedFiles   map[string][]byte
	MkdirAllPaths  []string
}

// NewMockFileSystem initializes a MockFileSystem.
func NewMockFileSystem() *MockFileSystem {
	return &MockFileSystem{
		CreatedFiles: make(map[string][]byte),
		AbsFunc: func(path string) (string, error) {
			if filepath.IsAbs(path) {
				return path, nil
			}
			return "/" + path, nil
		},
		IsNotExistFunc: func(err error) bool {
			return os.IsNotExist(err)
		},
	}
}

func (mfs *MockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	mfs.MkdirAllPaths = append(mfs.MkdirAllPaths, path)
	if mfs.MkdirAllFunc != nil {
		return mfs.MkdirAllFunc(path, perm)
	}
	return nil
}

type MockFileCloser struct {
	bytes.Buffer
	CloseFunc func() error
}

func (mfc *MockFileCloser) Close() error {
	if mfc.CloseFunc != nil {
		return mfc.CloseFunc()
	}
	return nil
}

func (mfs *MockFileSystem) Create(name string) (io.WriteCloser, error) {
	if mfs.CreateFunc != nil {
		return mfs.CreateFunc(name)
	}
	file := &MockFileCloser{}
	file.CloseFunc = func() error {
		mfs.CreatedFiles[name] = file.Bytes()
		return nil
	}
	return file, nil
}

func (mfs *MockFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	if mfs.WriteFileFunc != nil {
		return mfs.WriteFileFunc(name, data, perm)
	}
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	mfs.CreatedFiles[name] = dataCopy
	return nil
}

func (mfs *MockFileSystem) Abs(path string) (string, error) {
	if mfs.AbsFunc != nil {
		return mfs.AbsFunc(path)
	}
	if filepath.IsAbs(path) {
		return path, nil
	}
	return filepath.Join("/", path), nil
}

func (mfs *MockFileSystem) IsNotExist(err error) bool {
	if mfs.IsNotExistFunc != nil {
		return mfs.IsNotExistFunc(err)
	}
	return false
}

// MockFileContentProvider is a mock implementation of FileContentProvider.
// This was added in a previous step but seems to be missing from the read_files output.
// Adding it here to ensure the file is complete.
type MockFileContentProvider struct {
	ReadFileFunc  func(filename string) ([]byte, error)
	Files         map[string][]byte
	ReadFileCalls []string
}

func NewMockFileContentProvider() *MockFileContentProvider {
	return &MockFileContentProvider{
		Files: make(map[string][]byte),
	}
}

func (mfcp *MockFileContentProvider) ReadFile(filename string) ([]byte, error) {
	mfcp.ReadFileCalls = append(mfcp.ReadFileCalls, filename)
	if mfcp.ReadFileFunc != nil {
		return mfcp.ReadFileFunc(filename)
	}
	content, ok := mfcp.Files[filename]
	if !ok {
		return nil, fmt.Errorf("mock ReadFile: file not found %s", filename)
	}
	return content, nil
}

func TestReverseIfCond(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "simple if statement",
			input: `package main
func main() {
	if x > 0 {
		println("foo")
	}
}`,
			expected: `package main

func main() {
	if !(x > 0) {
		println("foo")
	}
}
`,
		},
		{
			name: "if with else",
			input: `package main
func main() {
	if x == y {
		return 1
	} else {
		return 0
	}
}`,
			expected: `package main

func main() {
	if !(x == y) {
		return 1
	} else {
		return 0
	}
}
`,
		},
		{
			name: "if with assignment",
			input: `package main
func main() {
	if err := foo(); err != nil {
		panic(err)
	}
}`,
			expected: `package main

func main() {
	if err := foo(); !(err != nil) {
		panic(err)
	}
}
`,
		},
		{
			name: "if with unary not (no change expected as per current logic)",
			input: `package main
func main() {
	if !isValid {
		return
	}
}`,
			expected: `package main

func main() {
	if !isValid {
		return
	}
}
`,
		},
		{
			name: "no if statement",
			input: `package main
func main() {
	x := 10
	println(x)
}`,
			expected: `package main

func main() {
	x := 10
	println(x)
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.input, 0)
			if err != nil {
				t.Fatalf("Failed to parse input: %v", err)
			}

			astutil.Apply(file, nil, reverseIfCond)

			var buf bytes.Buffer
			if err := printer.Fprint(&buf, fset, file); err != nil {
				t.Fatalf("Failed to print AST: %v", err)
			}
			got := buf.String()

			expectedFset := token.NewFileSet()
			expectedFile, err := parser.ParseFile(expectedFset, "expected.go", tt.expected, 0)
			if err != nil {
				t.Fatalf("Failed to parse expected output: %v", err)
			}
			var expectedBuf bytes.Buffer
			if err := printer.Fprint(&expectedBuf, expectedFset, expectedFile); err != nil {
				t.Fatalf("Failed to print expected AST: %v", err)
			}
			normalizedExpected := expectedBuf.String()

			if strings.TrimSpace(got) != strings.TrimSpace(normalizedExpected) {
				t.Errorf("reverseIfCond() output mismatch:\nGot:\n%s\nExpected:\n%s", got, normalizedExpected)
			}
		})
	}
}

func TestCreateMutations(t *testing.T) {
	const mutationTestDir = "/mutations"
	srcContent1 := `package main
func main() {
	if x > 10 {
		println("gt 10")
	}
}`
	expectedMutatedContent1 := `package main

func main() {
	if !(x > 10) {
		println("gt 10")
	}
}
`
	srcContent2 := `package main
func another() {
	if y < 5 {
		println("lt 5")
	}
}`
	expectedMutatedContent2 := `package main

func another() {
	if !(y < 5) {
		println("lt 5")
	}
}
`

	normalizeASTString := func(content string) (string, error) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "temp.go", content, parser.ParseComments)
		if err != nil {
			return "", fmt.Errorf("failed to parse content for normalization: %w", err)
		}
		var buf bytes.Buffer
		if err := printer.Fprint(&buf, fset, file); err != nil {
			return "", fmt.Errorf("failed to print content for normalization: %w", err)
		}
		return strings.TrimSpace(buf.String()), nil
	}

	normalizedExpected1, err := normalizeASTString(expectedMutatedContent1)
	if err != nil {
		t.Fatalf("Failed to normalize expected content 1: %v", err)
	}
	normalizedExpected2, err := normalizeASTString(expectedMutatedContent2)
	if err != nil {
		t.Fatalf("Failed to normalize expected content 2: %v", err)
	}

	t.Run("successful mutation of multiple files", func(t *testing.T) {
		mfs := NewMockFileSystem()
		mfcp := NewMockFileContentProvider()

		originalFile1 := "source/file1.go"
		originalFile2 := "source/pkg/file2.go"

		mfcp.Files[originalFile1] = []byte(srcContent1)
		mfcp.Files[originalFile2] = []byte(srcContent2)

		mfs.AbsFunc = func(path string) (string, error) {
			return "/abs/" + path, nil
		}

		mut := New(mfs, mfcp)
		originalFilenamesForMutator := []string{originalFile1, originalFile2}

		overlayPath, err := mut.CreateMutations(originalFilenamesForMutator, mutationTestDir)
		if err != nil {
			t.Fatalf("CreateMutations failed: %v", err)
		}

		if len(mfs.MkdirAllPaths) == 0 || mfs.MkdirAllPaths[0] != mutationTestDir {
			t.Errorf("Expected MkdirAll to be called with %s, got %v", mutationTestDir, mfs.MkdirAllPaths)
		}

		expectedOverlayPath := filepath.Join(mutationTestDir, "overlay.json")
		if overlayPath != expectedOverlayPath {
			t.Errorf("Expected overlay path %s, got %s", expectedOverlayPath, overlayPath)
		}

		overlayJSONBytes, ok := mfs.CreatedFiles[expectedOverlayPath]
		if !ok {
			t.Fatalf("Overlay JSON file %s was not created in mock filesystem", expectedOverlayPath)
		}

		var overlayData struct {
			Replace map[string]string
		}
		if err := json.Unmarshal(overlayJSONBytes, &overlayData); err != nil {
			t.Fatalf("Failed to unmarshal overlay JSON: %v", err)
		}

		absOriginal1 := "/abs/" + originalFile1
		absOriginal2 := "/abs/" + originalFile2

		expectedMutatedPath1 := filepath.Join(mutationTestDir, filepath.Base(originalFile1))
		expectedMutatedPath2 := filepath.Join(mutationTestDir, filepath.Base(originalFile2))

		if overlayData.Replace[absOriginal1] != expectedMutatedPath1 {
			t.Errorf("Overlay mapping for %s is incorrect. Got %s, want %s",
				absOriginal1, overlayData.Replace[absOriginal1], expectedMutatedPath1)
		}
		if overlayData.Replace[absOriginal2] != expectedMutatedPath2 {
			t.Errorf("Overlay mapping for %s is incorrect. Got %s, want %s",
				absOriginal2, overlayData.Replace[absOriginal2], expectedMutatedPath2)
		}
		if len(overlayData.Replace) != 2 {
			t.Errorf("Expected 2 entries in overlay replace map, got %d", len(overlayData.Replace))
		}

		mutatedContent1Bytes, ok := mfs.CreatedFiles[expectedMutatedPath1]
		if !ok {
			t.Fatalf("Mutated file %s was not created", expectedMutatedPath1)
		}
		gotMutatedContent1, err := normalizeASTString(string(mutatedContent1Bytes))
		if err != nil {
			t.Fatalf("Failed to normalize mutated content 1: %v", err)
		}

		if gotMutatedContent1 != normalizedExpected1 {
			t.Errorf("Content of mutated file %s is incorrect.\nGot:\n%s\nWant:\n%s",
				expectedMutatedPath1, gotMutatedContent1, normalizedExpected1)
		}

		mutatedContent2Bytes, ok := mfs.CreatedFiles[expectedMutatedPath2]
		if !ok {
			t.Fatalf("Mutated file %s was not created", expectedMutatedPath2)
		}
		gotMutatedContent2, err := normalizeASTString(string(mutatedContent2Bytes))
		if err != nil {
			t.Fatalf("Failed to normalize mutated content 2: %v", err)
		}
		if gotMutatedContent2 != normalizedExpected2 {
			t.Errorf("Content of mutated file %s is incorrect.\nGot:\n%s\nWant:\n%s",
				expectedMutatedPath2, gotMutatedContent2, normalizedExpected2)
		}
	})

	t.Run("error during MkdirAll", func(t *testing.T) {
		mfs := NewMockFileSystem()
		mfcp := NewMockFileContentProvider()
		mfs.MkdirAllFunc = func(path string, perm os.FileMode) error {
			return fmt.Errorf("mock MkdirAll error")
		}
		mut := New(mfs, mfcp)
		_, err := mut.CreateMutations([]string{"file.go"}, mutationTestDir)
		if err == nil {
			t.Fatal("Expected an error when MkdirAll fails, got nil")
		}
		if !strings.Contains(err.Error(), "mock MkdirAll error") {
			t.Errorf("Expected error message to contain 'mock MkdirAll error', got: %v", err)
		}
	})

	t.Run("error reading file content", func(t *testing.T) {
		mfs := NewMockFileSystem()
		mfcp := NewMockFileContentProvider()
		mfcp.ReadFileFunc = func(filename string) ([]byte, error) {
			return nil, fmt.Errorf("mock ReadFile error")
		}
		mut := New(mfs, mfcp)
		_, err := mut.CreateMutations([]string{"file.go"}, mutationTestDir)
		if err == nil {
			t.Fatal("Expected an error when ReadFile fails, got nil")
		}
		if !strings.Contains(err.Error(), "mock ReadFile error") {
			t.Errorf("Expected error to contain 'mock ReadFile error', got: %v", err)
		}
	})

	t.Run("error during file parsing", func(t *testing.T) {
		mfs := NewMockFileSystem()
		mfcp := NewMockFileContentProvider()
		mfcp.Files["invalid.go"] = []byte("package main\nfunc main() { if x > { } }")

		mut := New(mfs, mfcp)
		_, err := mut.CreateMutations([]string{"invalid.go"}, mutationTestDir)
		if err == nil {
			t.Fatal("Expected an error when parsing an invalid file, got nil")
		}
		if !strings.Contains(err.Error(), "failed to parse file") {
			t.Errorf("Expected error message to contain 'failed to parse file', got: %v", err)
		}
	})

	t.Run("error creating mutated file", func(t *testing.T) {
		mfs := NewMockFileSystem()
		mfcp := NewMockFileContentProvider()
		mfcp.Files["file1.go"] = []byte(srcContent1)

		mfs.CreateFunc = func(name string) (io.WriteCloser, error) {
			return nil, fmt.Errorf("mock create error")
		}
		mut := New(mfs, mfcp)
		_, err := mut.CreateMutations([]string{"file1.go"}, mutationTestDir)
		if err == nil {
			t.Fatalf("Expected error when mfs.Create fails, got nil")
		}
		if !strings.Contains(err.Error(), "mock create error") {
			t.Errorf("Expected error to contain 'mock create error', got '%v'", err)
		}
	})

	t.Run("error writing overlay file", func(t *testing.T) {
		mfs := NewMockFileSystem()
		mfcp := NewMockFileContentProvider()
		mfcp.Files["file1.go"] = []byte(srcContent1)

		mfs.WriteFileFunc = func(name string, data []byte, perm os.FileMode) error {
			if strings.HasSuffix(name, "overlay.json") {
				return fmt.Errorf("mock write overlay error")
			}
			dataCopy := make([]byte, len(data))
			copy(dataCopy, data)
			mfs.CreatedFiles[name] = dataCopy
			return nil
		}
		mut := New(mfs, mfcp)
		_, err := mut.CreateMutations([]string{"file1.go"}, mutationTestDir)
		if err == nil {
			t.Fatalf("Expected error when mfs.WriteFile for overlay fails, got nil")
		}
		if !strings.Contains(err.Error(), "mock write overlay error") {
			t.Errorf("Expected error to contain 'mock write overlay error', got '%v'", err)
		}
	})
}

func TestRealFileSystemAndContentProvider(t *testing.T) {
	rfs := RealFileSystem{}
	rfcp := RealFileContentProvider{}
	tempDir := t.TempDir()

	dirPath := filepath.Join(tempDir, "testdir")
	err := rfs.MkdirAll(dirPath, 0755)
	if err != nil {
		t.Fatalf("RealFileSystem.MkdirAll failed: %v", err)
	}
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		t.Errorf("RealFileSystem.MkdirAll did not create directory: %s", dirPath)
	}

	filePath := filepath.Join(tempDir, "testfile.txt")
	wc, err := rfs.Create(filePath)
	if err != nil {
		t.Fatalf("RealFileSystem.Create failed: %v", err)
	}
	testContent := []byte("hello world from RealFileSystem.Create")
	_, err = wc.Write(testContent)
	if err != nil {
		wc.Close()
		t.Fatalf("Write to RealFileSystem file failed: %v", err)
	}
	err = wc.Close()
	if err != nil {
		t.Fatalf("Close of RealFileSystem file failed: %v", err)
	}

	filePath2 := filepath.Join(tempDir, "testfile2.txt")
	testContent2 := []byte("hello world from RealFileSystem.WriteFile")
	err = rfs.WriteFile(filePath2, testContent2, 0644)
	if err != nil {
		t.Fatalf("RealFileSystem.WriteFile failed: %v", err)
	}

	readData, err := rfcp.ReadFile(filePath2)
	if err != nil {
		t.Fatalf("RealFileContentProvider.ReadFile failed: %v", err)
	}
	if !bytes.Equal(readData, testContent2) {
		t.Errorf("RealFileContentProvider.ReadFile content mismatch: got %s, want %s", string(readData), string(testContent2))
	}

	absPath, err := rfs.Abs(filePath)
	if err != nil {
		t.Fatalf("RealFileSystem.Abs failed: %v", err)
	}
	if !filepath.IsAbs(absPath) {
		t.Errorf("RealFileSystem.Abs did not return an absolute path: %s", absPath)
	}
	expectedAbsPath, _ := filepath.Abs(filePath)
	if absPath != expectedAbsPath {
		t.Errorf("RealFileSystem.Abs path mismatch: got %s, want %s", absPath, expectedAbsPath)
	}

	if rfs.IsNotExist(nil) {
		t.Error("RealFileSystem.IsNotExist(nil) should be false")
	}
	_, err = os.Stat(filepath.Join(tempDir, "nonexistentfile.txt"))
	if !rfs.IsNotExist(err) {
		t.Errorf("RealFileSystem.IsNotExist for a non-existent file should be true, err: %v", err)
	}
}
