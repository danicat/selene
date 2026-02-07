package mutator

import (
	"fmt"
	"go/ast"
	"go/token"
)

// ArithmeticMutator mutates arithmetic operators (+, -, *, /).
type ArithmeticMutator struct{}

func (m *ArithmeticMutator) Name() string {
	return "ArithmeticMutator"
}

func (m *ArithmeticMutator) Check(n ast.Node) []Mutation {
	expr, ok := n.(*ast.BinaryExpr)
	if !ok {
		return nil
	}

	var newOp token.Token
	switch expr.Op {
	case token.ADD:
		newOp = token.SUB
	case token.SUB:
		newOp = token.ADD
	case token.MUL:
		newOp = token.QUO
	case token.QUO:
		newOp = token.MUL
	default:
		return nil
	}

	originalOp := expr.Op

	return []Mutation{
		{
			ID:  fmt.Sprintf("Arithmetic_%d", expr.Pos()),
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

// ComparisonMutator mutates comparison operators (==, !=, <, >, <=, >=).
type ComparisonMutator struct{}

func (m *ComparisonMutator) Name() string {
	return "ComparisonMutator"
}

func (m *ComparisonMutator) Check(n ast.Node) []Mutation {
	expr, ok := n.(*ast.BinaryExpr)
	if !ok {
		return nil
	}

	var newOp token.Token
	switch expr.Op {
	case token.EQL:
		newOp = token.NEQ
	case token.NEQ:
		newOp = token.EQL
	case token.LSS:
		newOp = token.GEQ
	case token.GTR:
		newOp = token.LEQ
	case token.LEQ:
		newOp = token.GTR
	case token.GEQ:
		newOp = token.LSS
	default:
		return nil
	}

	originalOp := expr.Op

	return []Mutation{
		{
			ID:  fmt.Sprintf("Comparison_%d", expr.Pos()),
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

// BooleanMutator mutates boolean operators (&&, ||).
type BooleanMutator struct{}

func (m *BooleanMutator) Name() string {
	return "BooleanMutator"
}

func (m *BooleanMutator) Check(n ast.Node) []Mutation {
	expr, ok := n.(*ast.BinaryExpr)
	if !ok {
		return nil
	}

	var newOp token.Token
	switch expr.Op {
	case token.LAND:
		newOp = token.LOR
	case token.LOR:
		newOp = token.LAND
	default:
		return nil
	}

	originalOp := expr.Op

	return []Mutation{
		{
			ID:  fmt.Sprintf("Boolean_%d", expr.Pos()),
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
