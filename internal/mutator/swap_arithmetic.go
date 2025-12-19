package mutator

import (
	"go/ast"
	"go/token"
)

func init() {
	Register(&SwapArithmetic{})
}

// SwapArithmetic swaps arithmetic operators.
type SwapArithmetic struct{}

func (m *SwapArithmetic) Name() string {
	return "SwapArithmetic"
}

func (m *SwapArithmetic) Check(node ast.Node) bool {
	if x, ok := node.(*ast.BinaryExpr); ok {
		switch x.Op {
		case token.ADD, token.SUB, token.MUL, token.QUO:
			return true
		}
	}
	return false
}

func (m *SwapArithmetic) Apply(node ast.Node) {
	if x, ok := node.(*ast.BinaryExpr); ok {
		switch x.Op {
		case token.ADD:
			x.Op = token.SUB
		case token.SUB:
			x.Op = token.ADD
		case token.MUL:
			x.Op = token.QUO
		case token.QUO:
			x.Op = token.MUL
		}
	}
}

func (m *SwapArithmetic) Position(node ast.Node) token.Pos {
	if x, ok := node.(*ast.BinaryExpr); ok {
		return x.OpPos
	}
	return node.Pos()
}
