package mutator

import (
	"fmt"
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/ast/astutil"
)

// Mutator defines the interface for AST mutations.
type Mutator interface {
	Name() string
	Check(node ast.Node) bool
	Apply(node ast.Node)
	Position(node ast.Node) token.Pos
}

var registry = make(map[string]Mutator)

// Register adds a mutator to the registry.
func Register(m Mutator) {
	registry[m.Name()] = m
}

// All returns all registered mutators.
func All() []Mutator {
	var mutators []Mutator
	for _, m := range registry {
		mutators = append(mutators, m)
	}
	return mutators
}

// Get returns a mutator by name.
func Get(name string) (Mutator, bool) {
	m, ok := registry[name]
	return m, ok
}

// Candidate represents a potential mutation.
type Candidate struct {
	ID      string
	Mutator Mutator
	Node    ast.Node
}



// Scan finds all mutation candidates in a file.
func Scan(file *ast.File, fset *token.FileSet, mutators []Mutator) []Candidate {
	var candidates []Candidate
	astutil.Apply(file, nil, func(c *astutil.Cursor) bool {
		for _, m := range mutators {
			if m.Check(c.Node()) {
				pos := fset.Position(m.Position(c.Node()))
				id := fmt.Sprintf("%s-%s:%d:%d", m.Name(), pos.Filename, pos.Line, pos.Column)
				candidates = append(candidates, Candidate{
					ID:      id,
					Mutator: m,
					Node:    c.Node(),
				})
			}
		}
		return true
	})
	return candidates
}
