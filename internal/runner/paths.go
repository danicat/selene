package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PackageTarget represents a resolved Go package and its source files.
type PackageTarget struct {
	Dir        string   // Absolute directory path on disk
	ImportPath string   // Go import path (e.g., github.com/danicat/selene/internal/mutator)
	GoFiles    []string // List of absolute paths to Go source files (excluding _test.go)
}

type goListPackage struct {
	Dir        string       `json:"Dir"`
	ImportPath string       `json:"ImportPath"`
	Name       string       `json:"Name"`
	GoFiles    []string     `json:"GoFiles"`
	CgoFiles   []string     `json:"CgoFiles"`
	Error      *goListError `json:"Error"`
	Incomplete bool         `json:"Incomplete"`
}

type goListError struct {
	Err string `json:"Err"`
}

type pkgTracker struct {
	dir        string
	importPath string
	allFiles   bool
	fileMap    map[string]bool
	allGoFiles []string
	order      int
}

// ResolveTargets resolves any combination of files, directory paths, and Go package patterns.
func ResolveTargets(patterns []string) ([]PackageTarget, []string, error) {
	if len(patterns) == 0 {
		return nil, nil, fmt.Errorf("no patterns provided")
	}

	trackers := make(map[string]*pkgTracker) // keyed by absolute pkg.Dir
	var trackerOrder []*pkgTracker

	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return nil, nil, fmt.Errorf("empty pattern provided")
		}

		if isSingleGoFile(pattern) {
			if err := resolveSingleFile(pattern, trackers, &trackerOrder); err != nil {
				return nil, nil, err
			}
		} else {
			if err := resolvePackagePattern(pattern, trackers, &trackerOrder); err != nil {
				return nil, nil, err
			}
		}
	}

	var targets []PackageTarget
	var allFiles []string
	seenFiles := make(map[string]bool)

	for _, pt := range trackerOrder {
		var pkgFiles []string
		seenPkgFiles := make(map[string]bool)

		if pt.allFiles {
			for _, f := range pt.allGoFiles {
				if !strings.HasSuffix(f, "_test.go") && !seenPkgFiles[f] {
					seenPkgFiles[f] = true
					pkgFiles = append(pkgFiles, f)
				}
			}
		} else {
			for _, f := range pt.allGoFiles {
				if pt.fileMap[f] && !strings.HasSuffix(f, "_test.go") && !seenPkgFiles[f] {
					seenPkgFiles[f] = true
					pkgFiles = append(pkgFiles, f)
				}
			}
			// In case specific files were given that were not in allGoFiles
			for f := range pt.fileMap {
				if !strings.HasSuffix(f, "_test.go") && !seenPkgFiles[f] {
					seenPkgFiles[f] = true
					pkgFiles = append(pkgFiles, f)
				}
			}
		}

		if len(pkgFiles) > 0 {
			targets = append(targets, PackageTarget{
				Dir:        pt.dir,
				ImportPath: pt.importPath,
				GoFiles:    pkgFiles,
			})
			for _, f := range pkgFiles {
				if !seenFiles[f] {
					seenFiles[f] = true
					allFiles = append(allFiles, f)
				}
			}
		}
	}

	if len(allFiles) == 0 || len(targets) == 0 {
		return nil, nil, fmt.Errorf("no Go source files found for patterns: %v", patterns)
	}

	return targets, allFiles, nil
}

func isSingleGoFile(pattern string) bool {
	if strings.HasSuffix(pattern, ".go") {
		if st, err := os.Stat(pattern); err == nil && st.IsDir() {
			return false
		}
		return true
	}
	return false
}

func resolveSingleFile(pattern string, trackers map[string]*pkgTracker, trackerOrder *[]*pkgTracker) error {
	absFile, err := filepath.Abs(pattern)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for %s: %w", pattern, err)
	}

	st, err := os.Stat(absFile)
	if err != nil {
		return fmt.Errorf("file not found: %s: %w", pattern, err)
	}
	if st.IsDir() {
		return fmt.Errorf("expected file but got directory: %s", pattern)
	}

	// Test files are excluded
	if strings.HasSuffix(absFile, "_test.go") {
		return nil
	}

	dir := filepath.Dir(absFile)
	pkgs, err := runGoList(dir, ".")
	if err != nil {
		pkgs, err = runGoList("", dir)
		if err != nil {
			return fmt.Errorf("failed to resolve package for file %s: %w", pattern, err)
		}
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no package found for file %s", pattern)
	}

	pkg := pkgs[0]
	if pkg.Error != nil {
		return fmt.Errorf("package error for file %s: %s", pattern, pkg.Error.Err)
	}

	absDir, err := filepath.Abs(pkg.Dir)
	if err == nil {
		pkg.Dir = absDir
	}

	pt, exists := trackers[pkg.Dir]
	if !exists {
		var allGoFiles []string
		for _, f := range append(pkg.GoFiles, pkg.CgoFiles...) {
			if !strings.HasSuffix(f, "_test.go") {
				abs := f
				if !filepath.IsAbs(abs) {
					abs = filepath.Join(pkg.Dir, f)
				}
				allGoFiles = append(allGoFiles, abs)
			}
		}

		pt = &pkgTracker{
			dir:        pkg.Dir,
			importPath: pkg.ImportPath,
			fileMap:    make(map[string]bool),
			allGoFiles: allGoFiles,
			order:      len(*trackerOrder),
		}
		trackers[pkg.Dir] = pt
		*trackerOrder = append(*trackerOrder, pt)
	}

	pt.fileMap[absFile] = true
	return nil
}

func resolvePackagePattern(pattern string, trackers map[string]*pkgTracker, trackerOrder *[]*pkgTracker) error {
	var pkgs []*goListPackage
	var err error

	if st, statErr := os.Stat(pattern); statErr == nil && st.IsDir() {
		absDir, _ := filepath.Abs(pattern)
		pkgs, err = runGoList(absDir, ".")
		if err != nil {
			pkgs, err = runGoList("", pattern)
		}
	} else {
		pkgs, err = runGoList("", pattern)
	}

	if err != nil {
		return fmt.Errorf("failed to resolve pattern %q: %w", pattern, err)
	}
	if len(pkgs) == 0 {
		return fmt.Errorf("no packages found for pattern %q", pattern)
	}

	for _, pkg := range pkgs {
		if pkg.Error != nil {
			return fmt.Errorf("error resolving pattern %q: %s", pattern, pkg.Error.Err)
		}
		if pkg.Dir == "" {
			continue
		}

		absDir, err := filepath.Abs(pkg.Dir)
		if err == nil {
			pkg.Dir = absDir
		}

		var allGoFiles []string
		for _, f := range append(pkg.GoFiles, pkg.CgoFiles...) {
			if !strings.HasSuffix(f, "_test.go") {
				abs := f
				if !filepath.IsAbs(abs) {
					abs = filepath.Join(pkg.Dir, f)
				}
				allGoFiles = append(allGoFiles, abs)
			}
		}

		pt, exists := trackers[pkg.Dir]
		if !exists {
			pt = &pkgTracker{
				dir:        pkg.Dir,
				importPath: pkg.ImportPath,
				fileMap:    make(map[string]bool),
				allGoFiles: allGoFiles,
				order:      len(*trackerOrder),
			}
			trackers[pkg.Dir] = pt
			*trackerOrder = append(*trackerOrder, pt)
		} else {
			if len(pt.allGoFiles) == 0 {
				pt.allGoFiles = allGoFiles
			}
		}
		pt.allFiles = true
	}

	return nil
}

func runGoList(workDir, pattern string) ([]*goListPackage, error) {
	cmd := exec.Command("go", "list", "-json", pattern)
	if workDir != "" {
		cmd.Dir = workDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	outBytes := stdout.Bytes()

	var pkgs []*goListPackage
	if len(outBytes) > 0 {
		dec := json.NewDecoder(bytes.NewReader(outBytes))
		for {
			var pkg goListPackage
			if decodeErr := dec.Decode(&pkg); decodeErr == io.EOF {
				break
			} else if decodeErr != nil {
				return nil, fmt.Errorf("json decoding error: %w", decodeErr)
			}
			pkgs = append(pkgs, &pkg)
		}
	}

	if err != nil {
		for _, p := range pkgs {
			if p.Error != nil {
				return nil, fmt.Errorf("%s", p.Error.Err)
			}
		}
		errOutput := strings.TrimSpace(stderr.String())
		if errOutput == "" {
			errOutput = strings.TrimSpace(stdout.String())
		}
		if errOutput != "" {
			return nil, fmt.Errorf("%s", errOutput)
		}
		return nil, err
	}

	return pkgs, nil
}
