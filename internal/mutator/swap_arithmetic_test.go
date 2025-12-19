package mutator

import (
	"bytes"
	"go/parser"
	"go/printer"
	"go/token"
	"testing"

	"golang.org/x/tools/go/ast/astutil"
)

func TestSwapArithmetic(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected string
	}{
		{
			name: "add to sub",
			src: `package main
func main() {
	a := 1 + 2
}
`,
			expected: `package main

func main() {
	a := 1 - 2
}
`,
		},
		{
			name: "sub to add",
			src: `package main
func main() {
	a := 1 - 2
}
`,
			expected: `package main

func main() {
	a := 1 + 2
}
`,
		},
		{
			name: "mul to quo",
			src: `package main
func main() {
	a := 1 * 2
}
`,
			expected: `package main

func main() {
	a := 1 / 2
}
`,
		},
		{
			name: "quo to mul",
			src: `package main
func main() {
	a := 1 / 2
}
`,
			expected: `package main

func main() {
	a := 1 * 2
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "", tt.src, 0)
			if err != nil {
				t.Fatalf("ParseFile failed: %v", err)
			}

			astutil.Apply(file, nil, func(c *astutil.Cursor) bool {
				m := &SwapArithmetic{}
				if m.Check(c.Node()) {
					m.Apply(c.Node())
				}
				return true
			})

			var buf bytes.Buffer
			err = printer.Fprint(&buf, fset, file)
			if err != nil {
				t.Fatalf("Fprint failed: %v", err)
			}

			if got := buf.String(); got != tt.expected {
				t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", got, tt.expected)
			}
		})
	}
}
