package mutator

import (
	"fmt"
	"go/ast"
	"go/token"
)

// ConditionalsBoundaryMutator relaxes or tightens boundary checks (< ↔ <=, > ↔ >=).
type ConditionalsBoundaryMutator struct{}

func (m *ConditionalsBoundaryMutator) Name() string {
	return "ConditionalsBoundaryMutator"
}

func (m *ConditionalsBoundaryMutator) Check(n ast.Node) []Mutation {
	expr, ok := n.(*ast.BinaryExpr)
	if !ok {
		return nil
	}

	var newOp token.Token
	switch expr.Op {
	case token.LSS: // <
		newOp = token.LEQ // <=
	case token.LEQ: // <=
		newOp = token.LSS // <
	case token.GTR: // >
		newOp = token.GEQ // >=
	case token.GEQ: // >=
		newOp = token.GTR // >
	default:
		return nil
	}

	originalOp := expr.Op

	return []Mutation{
		{
			ID:  fmt.Sprintf("ConditionalsBoundary_%d", expr.Pos()),
			Pos: expr.Pos(),
			Apply: func() {
				expr.Op = newOp
			},
			Revert: func() {
				expr.Op = originalOp
			},
		},
	}
}

// IncrementDecrementMutator swaps ++ and --.
type IncrementDecrementMutator struct{}

func (m *IncrementDecrementMutator) Name() string {
	return "IncrementDecrementMutator"
}

func (m *IncrementDecrementMutator) Check(n ast.Node) []Mutation {
	stmt, ok := n.(*ast.IncDecStmt)
	if !ok {
		return nil
	}

	var newTok token.Token
	switch stmt.Tok {
	case token.INC:
		newTok = token.DEC
	case token.DEC:
		newTok = token.INC
	default:
		return nil
	}

	originalTok := stmt.Tok

	return []Mutation{
		{
			ID:  fmt.Sprintf("IncrementDecrement_%d", stmt.Pos()),
			Pos: stmt.Pos(),
			Apply: func() {
				stmt.Tok = newTok
			},
			Revert: func() {
				stmt.Tok = originalTok
			},
		},
	}
}
