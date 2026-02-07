package mutator

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

// helper to assert mutation
func assertMutation(t *testing.T, m Mutator, src, expected string) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, 0)
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

	// Normalize expected source
	fsetExpected := token.NewFileSet()
	fileExpected, err := parser.ParseFile(fsetExpected, "", expected, 0)
	if err != nil {
		t.Fatalf("invalid expected source: %s", err)
	}
	var bufExpected strings.Builder
	if err := printer.Fprint(&bufExpected, fsetExpected, fileExpected); err != nil {
		t.Fatal(err)
	}
	normalizedExpected := bufExpected.String()

	mut := mutations[0]
	mut.Apply()

	var buf strings.Builder
	if err := printer.Fprint(&buf, fset, file); err != nil {
		t.Fatal(err)
	}

	if got := buf.String(); got != normalizedExpected {
		t.Errorf("expected:\n%s\ngot:\n%s", normalizedExpected, got)
	}

	mut.Revert()
	buf.Reset()
	if err := printer.Fprint(&buf, fset, file); err != nil {
		t.Fatal(err)
	}

	// Normalize original source for revert check
	var bufSrc strings.Builder
	// Re-parse original src to normalize it
	fileSrc, _ := parser.ParseFile(fset, "", src, 0)
	if err := printer.Fprint(&bufSrc, fset, fileSrc); err != nil {
		t.Fatal(err)
	}
	normalizedSrc := bufSrc.String()

	if got := buf.String(); got != normalizedSrc {
		t.Errorf("revert failed. expected:\n%s\ngot:\n%s", normalizedSrc, got)
	}
}

func TestArithmeticMutator(t *testing.T) {
	m := &ArithmeticMutator{}
	if m.Name() != "ArithmeticMutator" {
		t.Errorf("expected ArithmeticMutator, got %s", m.Name())
	}

	tests := []struct {
		name     string
		src      string
		expected string
		none     bool
	}{
		{
			name: "Add to Sub",
			src: `package main

func main() { x := 1 + 2 }
`,
			expected: `package main

func main() { x := 1 - 2 }
`,
		},
		{
			name: "Sub to Add",
			src: `package main

func main() { x := 1 - 2 }
`,
			expected: `package main

func main() { x := 1 + 2 }
`,
		},
		{
			name: "Mul to Quo",
			src: `package main

func main() { x := 1 * 2 }
`,
			expected: `package main

func main() { x := 1 / 2 }
`,
		},
		{
			name: "Quo to Mul",
			src: `package main

func main() { x := 1 / 2 }
`,
			expected: `package main

func main() { x := 1 * 2 }
`,
		},
		{
			name: "Ignored Bitwise",
			src: `package main

func main() { x := 1 << 2 }
`,
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
			assertMutation(t, m, tt.src, tt.expected)
		})
	}
}

func TestComparisonMutator(t *testing.T) {
	m := &ComparisonMutator{}
	if m.Name() != "ComparisonMutator" {
		t.Errorf("expected ComparisonMutator, got %s", m.Name())
	}

	tests := []struct {
		name     string
		src      string
		expected string
		none     bool
	}{
		{
			name: "EQL to NEQ",
			src: `package main

func main() { if x == y {} }
`,
			expected: `package main

func main() { if x != y {} }
`,
		},
		{
			name: "LSS to GEQ",
			src: `package main

func main() { if x < y {} }
`,
			expected: `package main

func main() { if x >= y {} }
`,
		},
		{
			name: "NEQ to EQL",
			src: `package main

func main() { if x != y {} }
`,
			expected: `package main

func main() { if x == y {} }
`,
		},
		{
			name: "GTR to LEQ",
			src: `package main

func main() { if x > y {} }
`,
			expected: `package main

func main() { if x <= y {} }
`,
		},
		{
			name: "LEQ to GTR",
			src: `package main

func main() { if x <= y {} }
`,
			expected: `package main

func main() { if x > y {} }
`,
		},
		{
			name: "GEQ to LSS",
			src: `package main

func main() { if x >= y {} }
`,
			expected: `package main

func main() { if x < y {} }
`,
		},
		{
			name: "Ignored Arithmetic",
			src: `package main
func main() { x := 1 + 2 }`,
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
			assertMutation(t, m, tt.src, tt.expected)
		})
	}
}

func TestBooleanMutator(t *testing.T) {
	m := &BooleanMutator{}
	if m.Name() != "BooleanMutator" {
		t.Errorf("expected BooleanMutator, got %s", m.Name())
	}

	tests := []struct {
		name     string
		src      string
		expected string
		none     bool
	}{
		{
			name: "LAND to LOR",
			src: `package main

func main() { if x && y {} }
`,
			expected: `package main

func main() { if x || y {} }
`,
		},
		{
			name: "LOR to LAND",
			src: `package main

func main() { if x || y {} }
`,
			expected: `package main

func main() { if x && y {} }
`,
		},
		{
			name: "Ignored XOR",
			src: `package main
func main() { x := true ^ false }`,
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
			assertMutation(t, m, tt.src, tt.expected)
		})
	}
}
