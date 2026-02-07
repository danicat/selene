package mutator

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

func TestReverseIfCond(t *testing.T) {
	m := &ReverseIfCond{}
	if m.Name() != "ReverseIfCond" {
		t.Errorf("expected ReverseIfCond, got %s", m.Name())
	}

	tests := []struct {
		name     string
		src      string
		expected string
		none     bool
	}{
		{
			name: "Simple If",
			src: `package main

func main() {
	if a == b {
		return
	}
}
`,
			expected: `package main

func main() {
	if !(a == b) {
		return
	}
}
`,
		},
		{
			name: "Negated If",
			src: `package main
func main() {
	if !cond { return }
}`,
			expected: `package main
func main() {
	if cond { return }
}`,
		},
		{
			name: "Not an If",
			src: `package main
func main() { x := 1 }`,
			none: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.none {
				fset := token.NewFileSet()
				file, _ := parser.ParseFile(fset, "", tt.src, 0)
				var mutations []Mutation
				ast.Inspect(file, func(n ast.Node) bool {
					mutations = append(mutations, m.Check(n)...)
					return true
				})
				if len(mutations) != 0 {
					t.Errorf("expected 0 mutations, got %d", len(mutations))
				}
				return
			}
			// Use similar logic as assertMutation but adapted for if
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "", tt.src, 0)
			if err != nil {
				t.Fatal(err)
			}

			var mutations []Mutation
			ast.Inspect(file, func(n ast.Node) bool {
				mutations = append(mutations, m.Check(n)...)
				return true
			})

			if len(mutations) != 1 {
				t.Fatalf("expected 1 mutation, got %d", len(mutations))
			}

			mutation := mutations[0]
			mutation.Apply()

			var buf strings.Builder
			if err := printer.Fprint(&buf, fset, file); err != nil {
				t.Fatal(err)
			}

			// Normalize expected
			fsetExp := token.NewFileSet()
			fileExp, _ := parser.ParseFile(fsetExp, "", tt.expected, 0)
			var bufExp strings.Builder
			if err := printer.Fprint(&bufExp, fsetExp, fileExp); err != nil {
				t.Fatal(err)
			}

			if got := buf.String(); got != bufExp.String() {

				t.Errorf("expected:\n%s\ngot:\n%s", bufExp.String(), got)
			}

			mutation.Revert()
		})
	}
}
