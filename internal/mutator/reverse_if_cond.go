package mutator

import (
	"fmt"
	"go/ast"
	"go/token"
)

// ReverseIfCond negates the condition of an if statement.
type ReverseIfCond struct{}

func (m *ReverseIfCond) Name() string {
	return "ReverseIfCond"
}

func (m *ReverseIfCond) Check(n ast.Node) []Mutation {
	stmt, ok := n.(*ast.IfStmt)
	if !ok {
		return nil
	}

	// Capture the original condition
	originalCond := stmt.Cond

	// Define the mutated condition
	var mutatedCond ast.Expr
	if unary, ok := stmt.Cond.(*ast.UnaryExpr); ok && unary.Op == token.NOT {
		mutatedCond = unary.X
	} else {
		mutatedCond = &ast.UnaryExpr{
			Op: token.NOT,
			X:  &ast.ParenExpr{X: stmt.Cond},
		}
	}

	return []Mutation{
		{
			ID:  fmt.Sprintf("ReverseIfCond_%d", stmt.Pos()),
			Pos: stmt.Pos(),
			Apply: func() {
				stmt.Cond = mutatedCond
			},
			Revert: func() {
				stmt.Cond = originalCond
			},
		},
	}
}
