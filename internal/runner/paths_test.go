package runner

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolveTargets_SingleFile(t *testing.T) {
	// Find project root or runner dir
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}

	// Test with paths.go in the current runner directory (or relative)
	targetFile := "paths.go"
	if !fileExists(filepath.Join(wd, targetFile)) {
		targetFile = filepath.Join(wd, "internal", "runner", "paths.go")
	}

	targets, allFiles, err := ResolveTargets([]string{targetFile})
	if err != nil {
		t.Fatalf("ResolveTargets failed for single file %s: %v", targetFile, err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected 1 package target, got %d", len(targets))
	}

	target := targets[0]
	if !filepath.IsAbs(target.Dir) {
		t.Errorf("expected target.Dir to be absolute, got %s", target.Dir)
	}

	if !strings.HasSuffix(target.ImportPath, "internal/runner") {
		t.Errorf("expected ImportPath to end with internal/runner, got %s", target.ImportPath)
	}

	if len(target.GoFiles) != 1 {
		t.Fatalf("expected 1 GoFile in target, got %d (%v)", len(target.GoFiles), target.GoFiles)
	}

	absTargetFile, _ := filepath.Abs(targetFile)
	if target.GoFiles[0] != absTargetFile {
		t.Errorf("expected GoFiles[0] == %s, got %s", absTargetFile, target.GoFiles[0])
	}

	if len(allFiles) != 1 || allFiles[0] != absTargetFile {
		t.Errorf("expected allFiles == [%s], got %v", absTargetFile, allFiles)
	}
}

func TestResolveTargets_MultipleSingleFilesSamePackage(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}

	f1 := "paths.go"
	f2 := "runner.go"
	if !fileExists(filepath.Join(wd, f1)) {
		f1 = filepath.Join(wd, "internal", "runner", "paths.go")
		f2 = filepath.Join(wd, "internal", "runner", "runner.go")
	}

	targets, allFiles, err := ResolveTargets([]string{f1, f2})
	if err != nil {
		t.Fatalf("ResolveTargets failed: %v", err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected 1 target package for files in the same dir, got %d", len(targets))
	}

	if len(targets[0].GoFiles) != 2 {
		t.Fatalf("expected 2 GoFiles in target, got %d (%v)", len(targets[0].GoFiles), targets[0].GoFiles)
	}

	if len(allFiles) != 2 {
		t.Fatalf("expected 2 allFiles, got %d (%v)", len(allFiles), allFiles)
	}

	abs1, _ := filepath.Abs(f1)
	abs2, _ := filepath.Abs(f2)
	if !contains(allFiles, abs1) || !contains(allFiles, abs2) {
		t.Errorf("allFiles missing expected files: %v vs [%s, %s]", allFiles, abs1, abs2)
	}
}

func TestResolveTargets_Directory(t *testing.T) {
	// Locate internal/mutator
	dir := findPackageDir("internal/mutator")
	if dir == "" {
		t.Skip("internal/mutator directory not found")
	}

	targets, allFiles, err := ResolveTargets([]string{dir})
	if err != nil {
		t.Fatalf("ResolveTargets failed for directory %s: %v", dir, err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected 1 target package, got %d", len(targets))
	}

	target := targets[0]
	if !filepath.IsAbs(target.Dir) {
		t.Errorf("expected target.Dir to be absolute, got %s", target.Dir)
	}

	if target.ImportPath != "github.com/danicat/selene/internal/mutator" {
		t.Errorf("expected ImportPath github.com/danicat/selene/internal/mutator, got %s", target.ImportPath)
	}

	if len(target.GoFiles) == 0 {
		t.Errorf("expected non-empty GoFiles in mutator package")
	}

	for _, f := range target.GoFiles {
		if !filepath.IsAbs(f) {
			t.Errorf("expected GoFile %s to be absolute", f)
		}
		if strings.HasSuffix(f, "_test.go") {
			t.Errorf("test file should be excluded from GoFiles: %s", f)
		}
	}

	if len(allFiles) != len(target.GoFiles) {
		t.Errorf("mismatch between target.GoFiles (%d) and allFiles (%d)", len(target.GoFiles), len(allFiles))
	}
}

func TestResolveTargets_CurrentPackage(t *testing.T) {
	targets, allFiles, err := ResolveTargets([]string{"."})
	if err != nil {
		t.Fatalf("ResolveTargets failed for '.': %v", err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected 1 target package for '.', got %d", len(targets))
	}

	target := targets[0]
	if !filepath.IsAbs(target.Dir) {
		t.Errorf("expected absolute Dir, got %s", target.Dir)
	}

	if len(target.GoFiles) == 0 {
		t.Errorf("expected GoFiles for current package")
	}

	for _, f := range target.GoFiles {
		if strings.HasSuffix(f, "_test.go") {
			t.Errorf("test file found in GoFiles: %s", f)
		}
	}

	if len(allFiles) != len(target.GoFiles) {
		t.Errorf("mismatch between target.GoFiles and allFiles: %d vs %d", len(target.GoFiles), len(allFiles))
	}
}

func TestResolveTargets_Wildcard(t *testing.T) {
	targets, allFiles, err := ResolveTargets([]string{"./..."})
	if err != nil {
		t.Fatalf("ResolveTargets failed for './...': %v", err)
	}

	if len(targets) == 0 {
		t.Fatalf("expected at least 1 target for './...', got %d", len(targets))
	}

	if len(allFiles) == 0 {
		t.Fatalf("expected files to be resolved for './...'")
	}

	// Verify all returned paths are absolute and no test files
	seenFiles := make(map[string]bool)
	for _, target := range targets {
		if !filepath.IsAbs(target.Dir) {
			t.Errorf("expected absolute target.Dir, got %s", target.Dir)
		}
		for _, f := range target.GoFiles {
			if !filepath.IsAbs(f) {
				t.Errorf("expected absolute file path, got %s", f)
			}
			if strings.HasSuffix(f, "_test.go") {
				t.Errorf("unexpected test file in GoFiles: %s", f)
			}
			if seenFiles[f] {
				t.Errorf("duplicate file in targets: %s", f)
			}
			seenFiles[f] = true
		}
	}

	if len(seenFiles) != len(allFiles) {
		t.Errorf("allFiles count (%d) does not match total GoFiles (%d)", len(allFiles), len(seenFiles))
	}

	// Also test multiple targets from repo root or internal dir if available
	internalDir := findPackageDir("internal")
	if internalDir != "" {
		multiTargets, _, err := ResolveTargets([]string{internalDir + "/..."})
		if err == nil && len(multiTargets) < 2 {
			t.Errorf("expected >= 2 targets for %s/..., got %d", internalDir, len(multiTargets))
		}
	}
}

func TestResolveTargets_ImportPath(t *testing.T) {
	importPath := "github.com/danicat/selene/internal/runner"
	targets, allFiles, err := ResolveTargets([]string{importPath})
	if err != nil {
		t.Fatalf("ResolveTargets failed for import path %s: %v", importPath, err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected 1 target for import path, got %d", len(targets))
	}

	target := targets[0]
	if target.ImportPath != importPath {
		t.Errorf("expected ImportPath %s, got %s", importPath, target.ImportPath)
	}

	if len(target.GoFiles) == 0 {
		t.Fatalf("expected non-empty GoFiles for import path %s", importPath)
	}

	if len(allFiles) != len(target.GoFiles) {
		t.Errorf("mismatch between target.GoFiles (%d) and allFiles (%d)", len(target.GoFiles), len(allFiles))
	}
}

func TestResolveTargets_Deduplication(t *testing.T) {
	// Test deduplication between "." and a specific file in "."
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}

	targetFile := "paths.go"
	if !fileExists(filepath.Join(wd, targetFile)) {
		targetFile = filepath.Join(wd, "internal", "runner", "paths.go")
	}

	targets, allFiles, err := ResolveTargets([]string{".", targetFile, "."})
	if err != nil {
		t.Fatalf("ResolveTargets failed with overlapping patterns: %v", err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected 1 deduplicated target package, got %d", len(targets))
	}

	fileSet := make(map[string]bool)
	for _, f := range targets[0].GoFiles {
		if fileSet[f] {
			t.Errorf("duplicate file in target.GoFiles: %s", f)
		}
		fileSet[f] = true
	}

	allSet := make(map[string]bool)
	for _, f := range allFiles {
		if allSet[f] {
			t.Errorf("duplicate file in allFiles: %s", f)
		}
		allSet[f] = true
	}

	if len(targets[0].GoFiles) != len(allFiles) {
		t.Errorf("target.GoFiles length (%d) != allFiles length (%d)", len(targets[0].GoFiles), len(allFiles))
	}
}

func TestResolveTargets_DuplicateSingleFiles(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}

	targetFile := "paths.go"
	if !fileExists(filepath.Join(wd, targetFile)) {
		targetFile = filepath.Join(wd, "internal", "runner", "paths.go")
	}

	targets, allFiles, err := ResolveTargets([]string{targetFile, targetFile, targetFile})
	if err != nil {
		t.Fatalf("ResolveTargets failed: %v", err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}

	if len(targets[0].GoFiles) != 1 {
		t.Errorf("expected exactly 1 GoFile after deduplication, got %d", len(targets[0].GoFiles))
	}

	if len(allFiles) != 1 {
		t.Errorf("expected exactly 1 allFile after deduplication, got %d", len(allFiles))
	}
}

func TestResolveTargets_Errors(t *testing.T) {
	testCases := []struct {
		name     string
		patterns []string
	}{
		{
			name:     "empty slice",
			patterns: []string{},
		},
		{
			name:     "empty string pattern",
			patterns: []string{""},
		},
		{
			name:     "whitespace string pattern",
			patterns: []string{"   "},
		},
		{
			name:     "non-existent .go file",
			patterns: []string{"nonexistent_selene_file_12345.go"},
		},
		{
			name:     "non-existent package wildcard",
			patterns: []string{"./nonexistent_pkg_12345/..."},
		},
		{
			name:     "non-existent import path",
			patterns: []string{"github.com/danicat/selene/nonexistent_sub_pkg"},
		},
		{
			name:     "only test file pattern",
			patterns: []string{"paths_test.go"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			targets, files, err := ResolveTargets(tc.patterns)
			if err == nil {
				t.Fatalf("expected error for patterns %v, but got targets=%v, files=%v", tc.patterns, targets, files)
			}
		})
	}
}

func TestResolveTargets_TempDirFile(t *testing.T) {
	tmpDir := t.TempDir()
	goModContent := "module selenetemp\n\ngo 1.20\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("failed to write temp go.mod: %v", err)
	}

	sampleGo := "package selenetemp\n\nfunc Add(a, b int) int { return a + b }\n"
	sampleFile := filepath.Join(tmpDir, "sample.go")
	if err := os.WriteFile(sampleFile, []byte(sampleGo), 0644); err != nil {
		t.Fatalf("failed to write temp sample.go: %v", err)
	}

	targets, allFiles, err := ResolveTargets([]string{sampleFile})
	if err != nil {
		t.Fatalf("ResolveTargets failed on temp file: %v", err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}

	if targets[0].ImportPath != "selenetemp" {
		t.Errorf("expected ImportPath selenetemp, got %s", targets[0].ImportPath)
	}

	if len(targets[0].GoFiles) != 1 || targets[0].GoFiles[0] != sampleFile {
		t.Errorf("expected GoFiles == [%s], got %v", sampleFile, targets[0].GoFiles)
	}

	if len(allFiles) != 1 || allFiles[0] != sampleFile {
		t.Errorf("expected allFiles == [%s], got %v", sampleFile, allFiles)
	}
}

func TestResolveTargets_MixedFilesAndPackages(t *testing.T) {
	mutatorDir := findPackageDir("internal/mutator")
	if mutatorDir == "" {
		t.Skip("internal/mutator directory not found")
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}

	runnerFile := "paths.go"
	if !fileExists(filepath.Join(wd, runnerFile)) {
		runnerFile = filepath.Join(wd, "internal", "runner", "paths.go")
	}

	targets, allFiles, err := ResolveTargets([]string{runnerFile, mutatorDir})
	if err != nil {
		t.Fatalf("ResolveTargets failed for mixed targets: %v", err)
	}

	if len(targets) != 2 {
		t.Fatalf("expected 2 targets for mixed patterns, got %d", len(targets))
	}

	// Target 1: runner package with single file
	if len(targets[0].GoFiles) != 1 {
		t.Errorf("expected target 0 to have 1 file, got %d", len(targets[0].GoFiles))
	}

	// Target 2: mutator package with all files
	if len(targets[1].GoFiles) < 2 {
		t.Errorf("expected target 1 to have multiple files, got %d", len(targets[1].GoFiles))
	}

	expectedTotal := len(targets[0].GoFiles) + len(targets[1].GoFiles)
	if len(allFiles) != expectedTotal {
		t.Errorf("expected allFiles length %d, got %d", expectedTotal, len(allFiles))
	}
}

func TestResolveTargets_AbsolutePaths(t *testing.T) {
	mutatorDir := findPackageDir("internal/mutator")
	if mutatorDir == "" {
		t.Skip("internal/mutator directory not found")
	}

	absMutatorDir, err := filepath.Abs(mutatorDir)
	if err != nil {
		t.Fatalf("failed to get abs mutator dir: %v", err)
	}

	targets, allFiles, err := ResolveTargets([]string{absMutatorDir})
	if err != nil {
		t.Fatalf("ResolveTargets failed for abs dir: %v", err)
	}

	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}

	if targets[0].Dir != absMutatorDir {
		t.Errorf("expected target.Dir == %s, got %s", absMutatorDir, targets[0].Dir)
	}

	if len(allFiles) == 0 {
		t.Errorf("expected allFiles to be populated")
	}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func findPackageDir(relPkg string) string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Try relative from current wd
	candidates := []string{
		relPkg,
		filepath.Join(".", relPkg),
		filepath.Join("..", relPkg),
		filepath.Join("..", "..", relPkg),
	}

	for _, c := range candidates {
		abs, err := filepath.Abs(filepath.Join(wd, c))
		if err == nil {
			if st, err := os.Stat(abs); err == nil && st.IsDir() {
				return c
			}
		}
	}
	return ""
}

func contains(slice []string, s string) bool {
	return slices.Contains(slice, s)
}
