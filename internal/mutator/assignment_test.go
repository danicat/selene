package mutator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestAssignmentMutator(t *testing.T) {
	m := &AssignmentMutator{}
	if m.Name() != "AssignmentMutator" {
		t.Errorf("expected AssignmentMutator, got %s", m.Name())
	}

	tests := []struct {
		name     string
		src      string
		expected string
		none     bool
	}{
		{
			name: "ADD_ASSIGN to SUB_ASSIGN",
			src: `package main

func main() { x += y }
`,
			expected: `package main

func main() { x -= y }
`,
		},
		{
			name: "SUB_ASSIGN to ADD_ASSIGN",
			src: `package main

func main() { x -= y }
`,
			expected: `package main

func main() { x += y }
`,
		},
		{
			name: "MUL_ASSIGN to QUO_ASSIGN",
			src: `package main

func main() { x *= y }
`,
			expected: `package main

func main() { x /= y }
`,
		},
		{
			name: "QUO_ASSIGN to MUL_ASSIGN",
			src: `package main

func main() { x /= y }
`,
			expected: `package main

func main() { x *= y }
`,
		},
		{
			name: "AND_ASSIGN to OR_ASSIGN",
			src: `package main

func main() { x &= y }
`,
			expected: `package main

func main() { x |= y }
`,
		},
		{
			name: "OR_ASSIGN to AND_ASSIGN",
			src: `package main

func main() { x |= y }
`,
			expected: `package main

func main() { x &= y }
`,
		},
		{
			name: "XOR_ASSIGN to AND_ASSIGN",
			src: `package main

func main() { x ^= y }
`,
			expected: `package main

func main() { x &= y }
`,
		},
		{
			name: "SHL_ASSIGN to SHR_ASSIGN",
			src: `package main

func main() { x <<= y }
`,
			expected: `package main

func main() { x >>= y }
`,
		},
		{
			name: "SHR_ASSIGN to SHL_ASSIGN",
			src: `package main

func main() { x >>= y }
`,
			expected: `package main

func main() { x <<= y }
`,
		},
		{
			name: "AND_NOT_ASSIGN to AND_ASSIGN",
			src: `package main

func main() { x &^= y }
`,
			expected: `package main

func main() { x &= y }
`,
		},
		{
			name: "Ignored Standard Assign",
			src: `package main

func main() { x = y }
`,
			none: true,
		},
		{
			name: "Ignored Define Assign",
			src: `package main

func main() { x := y }
`,
			none: true,
		},
		{
			name: "Ignored Rem Assign",
			src: `package main

func main() { x %= y }
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

func TestBitwiseMutator(t *testing.T) {
	m := &BitwiseMutator{}
	if m.Name() != "BitwiseMutator" {
		t.Errorf("expected BitwiseMutator, got %s", m.Name())
	}

	tests := []struct {
		name     string
		src      string
		expected string
		none     bool
	}{
		{
			name: "AND to OR",
			src: `package main

func main() { x := a & b }
`,
			expected: `package main

func main() { x := a | b }
`,
		},
		{
			name: "OR to AND",
			src: `package main

func main() { x := a | b }
`,
			expected: `package main

func main() { x := a & b }
`,
		},
		{
			name: "XOR to AND",
			src: `package main

func main() { x := a ^ b }
`,
			expected: `package main

func main() { x := a & b }
`,
		},
		{
			name: "SHL to SHR",
			src: `package main

func main() { x := a << b }
`,
			expected: `package main

func main() { x := a >> b }
`,
		},
		{
			name: "SHR to SHL",
			src: `package main

func main() { x := a >> b }
`,
			expected: `package main

func main() { x := a << b }
`,
		},
		{
			name: "AND_NOT to AND",
			src: `package main

func main() { x := a &^ b }
`,
			expected: `package main

func main() { x := a & b }
`,
		},
		{
			name: "Ignored Arithmetic Addition",
			src: `package main

func main() { x := a + b }
`,
			none: true,
		},
		{
			name: "Ignored Logical AND",
			src: `package main

func main() { x := a && b }
`,
			none: true,
		},
		{
			name: "Ignored Equality",
			src: `package main

func main() { x := a == b }
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
