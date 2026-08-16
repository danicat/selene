package mutator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestBooleanLiteralMutator(t *testing.T) {
	m := &BooleanLiteralMutator{}
	if m.Name() != "BooleanLiteralMutator" {
		t.Errorf("expected BooleanLiteralMutator, got %s", m.Name())
	}

	tests := []struct {
		name     string
		src      string
		expected string
		none     bool
	}{
		{
			name: "True to False",
			src: `package main

func main() { x := true }
`,
			expected: `package main

func main() { x := false }
`,
		},
		{
			name: "False to True",
			src: `package main

func main() { x := false }
`,
			expected: `package main

func main() { x := true }
`,
		},
		{
			name: "Ignored Identifier",
			src: `package main

func main() { x := other }
`,
			none: true,
		},
		{
			name: "Ignored Integer",
			src: `package main

func main() { x := 1 }
`,
			none: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.none {
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
				if len(mutations) != 0 {
					t.Errorf("expected 0 mutations, got %d", len(mutations))
				}
				return
			}
			assertMutation(t, m, tt.src, tt.expected)
		})
	}
}

func TestIntegerLiteralMutator(t *testing.T) {
	m := &IntegerLiteralMutator{}
	if m.Name() != "IntegerLiteralMutator" {
		t.Errorf("expected IntegerLiteralMutator, got %s", m.Name())
	}

	tests := []struct {
		name     string
		src      string
		expected string
		none     bool
	}{
		{
			name: "Zero to One",
			src: `package main

func main() { x := 0 }
`,
			expected: `package main

func main() { x := 1 }
`,
		},
		{
			name: "One to Zero",
			src: `package main

func main() { x := 1 }
`,
			expected: `package main

func main() { x := 0 }
`,
		},
		{
			name: "42 to Zero",
			src: `package main

func main() { x := 42 }
`,
			expected: `package main

func main() { x := 0 }
`,
		},
		{
			name: "100 to Zero",
			src: `package main

func main() { x := 100 }
`,
			expected: `package main

func main() { x := 0 }
`,
		},
		{
			name: "Ignored String Literal",
			src: `package main

func main() { x := "0" }
`,
			none: true,
		},
		{
			name: "Ignored Float Literal",
			src: `package main

func main() { x := 3.14 }
`,
			none: true,
		},
		{
			name: "Ignored Identifier",
			src: `package main

func main() { x := count }
`,
			none: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.none {
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
				if len(mutations) != 0 {
					t.Errorf("expected 0 mutations, got %d", len(mutations))
				}
				return
			}
			assertMutation(t, m, tt.src, tt.expected)
		})
	}
}

func TestStringLiteralMutator(t *testing.T) {
	m := &StringLiteralMutator{}
	if m.Name() != "StringLiteralMutator" {
		t.Errorf("expected StringLiteralMutator, got %s", m.Name())
	}

	tests := []struct {
		name     string
		src      string
		expected string
		none     bool
	}{
		{
			name: "Empty String to Mutated",
			src: `package main

func main() { s := "" }
`,
			expected: `package main

func main() { s := "selene_mutated" }
`,
		},
		{
			name: "Empty Raw String to Mutated",
			src:  "package main\n\nfunc main() { s := `` }\n",
			expected: `package main

func main() { s := "selene_mutated" }
`,
		},
		{
			name: "Non-empty String to Empty",
			src: `package main

func main() { s := "hello" }
`,
			expected: `package main

func main() { s := "" }
`,
		},
		{
			name: "Non-empty Raw String to Empty",
			src:  "package main\n\nfunc main() { s := `hello world` }\n",
			expected: `package main

func main() { s := "" }
`,
		},
		{
			name: "Ignored Integer",
			src: `package main

func main() { s := 123 }
`,
			none: true,
		},
		{
			name: "Ignored Boolean",
			src: `package main

func main() { s := true }
`,
			none: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.none {
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
				if len(mutations) != 0 {
					t.Errorf("expected 0 mutations, got %d", len(mutations))
				}
				return
			}
			assertMutation(t, m, tt.src, tt.expected)
		})
	}
}
