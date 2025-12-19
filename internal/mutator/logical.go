package mutator

import (
	"go/ast"
	"go/token"
)

func init() {
	Register(&Logical{})
}

// Logical swaps logical operators (&& <-> ||).
type Logical struct{}

func (m *Logical) Name() string {
	return "Logical"
}

func (m *Logical) Check(node ast.Node) bool {
	x, ok := node.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	switch x.Op {
	case token.LAND, token.LOR:
		return true
	}
	return false
}

func (m *Logical) Apply(node ast.Node) {
	x := node.(*ast.BinaryExpr)
	switch x.Op {
	case token.LAND:
		x.Op = token.LOR
	case token.LOR:
		x.Op = token.LAND
	}
}

func (m *Logical) Position(node ast.Node) token.Pos {
	return node.(*ast.BinaryExpr).OpPos
}
