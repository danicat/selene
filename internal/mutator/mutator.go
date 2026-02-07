package mutator

import (
	"go/ast"
	"go/token"
)

// Mutation represents a single code mutation.
type Mutation struct {
	ID     string    // Unique identifier for the mutation
	Pos    token.Pos // Position in the source file
	Apply  func()    // Applies the mutation to the AST
	Revert func()    // Reverts the mutation
}

// Mutator defines the interface for code mutations.
type Mutator interface {
	// Name returns the name of the mutator.
	Name() string
	// Check checks a node for possible mutations and returns them.
	Check(n ast.Node) []Mutation
}
