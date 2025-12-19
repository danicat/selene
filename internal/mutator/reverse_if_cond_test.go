package mutator

import (
	"bytes"
	"go/parser"
	"go/printer"
	"go/token"
	"testing"

	"golang.org/x/tools/go/ast/astutil"
)

func TestReverseIfCond(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected string
	}{
		{
			name: "boolean variable",
			src: `package main
func main() {
	if a {
		return
	}
}
`,
			expected: `package main

func main() {
	if !a {
		return
	}
}
`,
		},
		{
			name: "function call",
			src: `package main
func main() {
	if isReady() {
		return
	}
}
`,
			expected: `package main

func main() {
	if !isReady() {
		return
	}
}
`,
		},
		{
			name: "boolean literal",
			src: `package main
func main() {
	if true {
		return
	}
}
`,
			expected: `package main

func main() {
	if !true {
		return
	}
}
`,
		},
		{
			name: "multiple ifs",
			src: `package main
func main() {
	if a {
		return
	}
	if b {
		return
	}
}
`,
			expected: `package main

func main() {
	if !a {
		return
	}
	if !b {
		return
	}
}
`,
		},
		{
			name: "nested ifs",
			src: `package main
func main() {
	if a {
		if b {
			return
		}
	}
}
`,
			expected: `package main

func main() {
	if !a {
		if !b {
			return
		}
	}
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
				m := &ReverseIfCond{}
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
