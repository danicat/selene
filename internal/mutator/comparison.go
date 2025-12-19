package mutator

import (
	"go/ast"
	"go/token"
)

func init() {
	Register(&Comparison{})
}

// Comparison swaps relational operators with their logical opposites.
type Comparison struct{}

func (m *Comparison) Name() string {
	return "Comparison"
}

func (m *Comparison) Check(node ast.Node) bool {
	x, ok := node.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	switch x.Op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	}
	return false
}

func (m *Comparison) Apply(node ast.Node) {
	x := node.(*ast.BinaryExpr)
	switch x.Op {
	case token.EQL:
		x.Op = token.NEQ
	case token.NEQ:
		x.Op = token.EQL
	case token.LSS:
		x.Op = token.GEQ
	case token.GEQ:
		x.Op = token.LSS
	case token.GTR:
		x.Op = token.LEQ
	case token.LEQ:
		x.Op = token.GTR
	}
}

func (m *Comparison) Position(node ast.Node) token.Pos {
	return node.(*ast.BinaryExpr).OpPos
}
