package mutator

import (
	"go/ast"
	"go/token"
)

func init() {
	Register(&ReverseIfCond{})
}

// ReverseIfCond negates boolean expressions in if statements.
type ReverseIfCond struct{}

func (m *ReverseIfCond) Name() string {
	return "ReverseIfCond"
}

func (m *ReverseIfCond) Check(node ast.Node) bool {
	_, ok := node.(*ast.IfStmt)
	return ok
}

func (m *ReverseIfCond) Apply(node ast.Node) {
	if x, ok := node.(*ast.IfStmt); ok {
		cond := x.Cond
		// Wrap in parentheses if it's a binary expression to ensure correct precedence
		if _, ok := cond.(*ast.BinaryExpr); ok {
			cond = &ast.ParenExpr{X: cond}
		}

		notExpr := &ast.UnaryExpr{
			Op: token.NOT,
			X:  cond,
		}
		x.Cond = notExpr
	}
}

func (m *ReverseIfCond) Position(node ast.Node) token.Pos {
	return node.Pos()
}
