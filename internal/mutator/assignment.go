package mutator

import (
	"fmt"
	"go/ast"
	"go/token"
)

// AssignmentMutator mutates assignment operators (+=, -=, *=, /=, &=, |=, ^=, <<=, >>=, &^=).
type AssignmentMutator struct{}

func (m *AssignmentMutator) Name() string {
	return "AssignmentMutator"
}

func (m *AssignmentMutator) Check(n ast.Node) []Mutation {
	stmt, ok := n.(*ast.AssignStmt)
	if !ok {
		return nil
	}

	var newTok token.Token
	switch stmt.Tok {
	case token.ADD_ASSIGN:
		newTok = token.SUB_ASSIGN
	case token.SUB_ASSIGN:
		newTok = token.ADD_ASSIGN
	case token.MUL_ASSIGN:
		newTok = token.QUO_ASSIGN
	case token.QUO_ASSIGN:
		newTok = token.MUL_ASSIGN
	case token.AND_ASSIGN:
		newTok = token.OR_ASSIGN
	case token.OR_ASSIGN:
		newTok = token.AND_ASSIGN
	case token.XOR_ASSIGN:
		newTok = token.AND_ASSIGN
	case token.SHL_ASSIGN:
		newTok = token.SHR_ASSIGN
	case token.SHR_ASSIGN:
		newTok = token.SHL_ASSIGN
	case token.AND_NOT_ASSIGN:
		newTok = token.AND_ASSIGN
	default:
		return nil
	}

	originalTok := stmt.Tok

	return []Mutation{
		{
			ID:  fmt.Sprintf("Assignment_%d", stmt.Pos()),
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

// BitwiseMutator mutates bitwise binary operators (&, |, ^, <<, >>, &^).
type BitwiseMutator struct{}

func (m *BitwiseMutator) Name() string {
	return "BitwiseMutator"
}

func (m *BitwiseMutator) Check(n ast.Node) []Mutation {
	expr, ok := n.(*ast.BinaryExpr)
	if !ok {
		return nil
	}

	var newOp token.Token
	switch expr.Op {
	case token.AND:
		newOp = token.OR
	case token.OR:
		newOp = token.AND
	case token.XOR:
		newOp = token.AND
	case token.SHL:
		newOp = token.SHR
	case token.SHR:
		newOp = token.SHL
	case token.AND_NOT:
		newOp = token.AND
	default:
		return nil
	}

	originalOp := expr.Op

	return []Mutation{
		{
			ID:  fmt.Sprintf("Bitwise_%d", expr.Pos()),
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
