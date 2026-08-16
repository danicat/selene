package mutator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestBuildDestructiveExclusionSet_DestructiveCallArguments(t *testing.T) {
	src := `package test
import "os"
import "os/exec"

func Cleanup(dir string) {
	os.RemoveAll(dir + "/cache")
	exec.Command("rm", "-rf", dir)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "cleanup.go", src, 0)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	exclusions := BuildDestructiveExclusionSet(file, fset)
	if len(exclusions) == 0 {
		t.Fatalf("expected non-empty exclusion set for destructive calls")
	}

	stringMut := &StringLiteralMutator{}
	var mutCount int
	var excludedMutCount int
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		muts := stringMut.Check(n)
		for range muts {
			mutCount++
			if reason, ok := exclusions[n]; ok {
				excludedMutCount++
				t.Logf("Found excluded string mut: %s", reason)
			}
		}
		return true
	})

	if mutCount == 0 {
		t.Fatalf("expected string mutations in cleanup.go")
	}
	if excludedMutCount == 0 {
		t.Fatalf("expected string mutations inside os.RemoveAll/exec.Command to be excluded")
	}
}

func TestBuildDestructiveExclusionSet_Guards(t *testing.T) {
	src := `package test
import "os"

func SafeDelete(path string) {
	if path != "/" && path != "" {
		os.Remove(path)
	}
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "safedelete.go", src, 0)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	exclusions := BuildDestructiveExclusionSet(file, fset)
	compMut := &ComparisonMutator{}

	var compMutsCount int
	var excludedCompMutsCount int
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		muts := compMut.Check(n)
		for range muts {
			compMutsCount++
			if reason, ok := exclusions[n]; ok {
				excludedCompMutsCount++
				t.Logf("Found excluded comparison guard: %s", reason)
			}
		}
		return true
	})

	if compMutsCount == 0 {
		t.Fatalf("expected comparison mutations on 'path != \"/\"'")
	}
	if excludedCompMutsCount == 0 {
		t.Fatalf("expected comparison mutations guarding os.Remove to be excluded")
	}
}
