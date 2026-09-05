package ast

import "github.com/vertex-language/vcc/token"

// SEHTryStmt represents MSVC __try { ... } __except(...) { ... } or __finally { ... }
type SEHTryStmt struct {
	Try     token.Pos
	Body    Stmt
	Except  token.Pos
	Filter  Expr
	Handler Stmt
	Finally token.Pos
	Span
}

func (s *SEHTryStmt) stmtNode() {}

type SEHLeaveStmt struct {
	Leave token.Pos
	Semi  token.Pos
	Span
}

func (s *SEHLeaveStmt) stmtNode() {}
