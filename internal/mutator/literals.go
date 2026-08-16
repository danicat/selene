package mutator

import (
	"fmt"
	"go/ast"
	"go/token"
)

// BooleanLiteralMutator mutates boolean literals (true <-> false).
type BooleanLiteralMutator struct{}

func (m *BooleanLiteralMutator) Name() string {
	return "BooleanLiteralMutator"
}

func (m *BooleanLiteralMutator) Check(n ast.Node) []Mutation {
	ident, ok := n.(*ast.Ident)
	if !ok {
		return nil
	}

	var newName string
	switch ident.Name {
	case "true":
		newName = "false"
	case "false":
		newName = "true"
	default:
		return nil
	}

	originalName := ident.Name

	return []Mutation{
		{
			ID:  fmt.Sprintf("BooleanLiteral_%d", ident.Pos()),
			Pos: ident.Pos(),
			Apply: func() {
				ident.Name = newName
			},
			Revert: func() {
				ident.Name = originalName
			},
		},
	}
}

// IntegerLiteralMutator mutates integer literals (0 -> 1, non-zero -> 0).
type IntegerLiteralMutator struct{}

func (m *IntegerLiteralMutator) Name() string {
	return "IntegerLiteralMutator"
}

func (m *IntegerLiteralMutator) Check(n ast.Node) []Mutation {
	lit, ok := n.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return nil
	}

	var newValue string
	if lit.Value == "0" {
		newValue = "1"
	} else {
		newValue = "0"
	}

	originalValue := lit.Value

	return []Mutation{
		{
			ID:  fmt.Sprintf("IntegerLiteral_%d", lit.Pos()),
			Pos: lit.Pos(),
			Apply: func() {
				lit.Value = newValue
			},
			Revert: func() {
				lit.Value = originalValue
			},
		},
	}
}

// StringLiteralMutator mutates string literals ("" -> "selene_mutated", non-empty -> "").
type StringLiteralMutator struct{}

func (m *StringLiteralMutator) Name() string {
	return "StringLiteralMutator"
}

func (m *StringLiteralMutator) Check(n ast.Node) []Mutation {
	lit, ok := n.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return nil
	}

	var newValue string
	if lit.Value == `""` || lit.Value == "``" {
		newValue = `"selene_mutated"`
	} else {
		newValue = `""`
	}

	originalValue := lit.Value

	return []Mutation{
		{
			ID:  fmt.Sprintf("StringLiteral_%d", lit.Pos()),
			Pos: lit.Pos(),
			Apply: func() {
				lit.Value = newValue
			},
			Revert: func() {
				lit.Value = originalValue
			},
		},
	}
}
