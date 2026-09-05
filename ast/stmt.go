package ast

import "github.com/vertex-language/vcc/token"

// BadStmt covers a statement the parser gave up on.
type BadStmt struct {
	Span
}

// LabeledStmt is Identifier : Statement.
type LabeledStmt struct {
	Span
	Label *Ident
	Colon token.Pos
	Stmt  Stmt
}

// CaseStmt is case ConstantExpression : Statement, or default :
// Statement (Kind DEFAULT, Value nil). Placement discipline is
// checked later.
type CaseStmt struct {
	Span
	Keyword token.Pos
	Kind    token.Kind // CASE or DEFAULT
	Value   Expr       // nil for default

	// High is the upper bound of gcc's case range, `case lo ... hi:`, and
	// nil for the ordinary single-value form. The range is inclusive at both
	// ends.
	High Expr

	Colon token.Pos
	Stmt  Stmt
}

// CompoundStmt is { BlockItemList }.
type CompoundStmt struct {
	Span
	Lbrace token.Pos
	Items  []Stmt // declarations arrive wrapped in DeclStmt
	Rbrace token.Pos
}

// DeclStmt adapts a Decl into a block item.
type DeclStmt struct {
	Span
	D Decl
}

// ExprStmt is [Expression] ; with a non-nil X; a lone semicolon is
// EmptyStmt.
type ExprStmt struct {
	Span
	X    Expr
	Semi token.Pos
}

// EmptyStmt keeps a stray semicolon visible.
type EmptyStmt struct {
	Span
	Semi token.Pos
}

// IfStmt covers both selection forms; the dangling else resolved
// during parsing, binding to the nearest unmatched if. ElsePos and
// Else are zero when there is no else.
type IfStmt struct {
	Span
	If      token.Pos
	Lparen  token.Pos
	Cond    Expr
	Rparen  token.Pos
	Then    Stmt
	ElsePos token.Pos // NoPos when no else
	Else    Stmt      // nil when no else
}

// SwitchStmt is switch ( Expression ) Statement.
type SwitchStmt struct {
	Span
	Switch token.Pos
	Lparen token.Pos
	Cond   Expr
	Rparen token.Pos
	Body   Stmt
}

// WhileStmt is while ( Expression ) Statement.
type WhileStmt struct {
	Span
	While  token.Pos
	Lparen token.Pos
	Cond   Expr
	Rparen token.Pos
	Body   Stmt
}

// DoStmt is do Statement while ( Expression ) ;
type DoStmt struct {
	Span
	Do     token.Pos
	Body   Stmt
	While  token.Pos
	Lparen token.Pos
	Cond   Expr
	Rparen token.Pos
	Semi   token.Pos
}

// ForStmt covers both for forms. In the declaration form, Init is a
// *GenDecl (wrapped by nothing — the semicolon is the declaration's
// own) and Semi1 is NoPos: no ';' terminates the for-init clause
// separately. In the expression form Init is an Expr or nil and Semi1
// is the first semicolon.
type ForStmt struct {
	Span
	For    token.Pos
	Lparen token.Pos
	Init   Node      // nil, Expr, or *GenDecl
	Semi1  token.Pos // NoPos in the declaration form
	Cond   Expr
	Semi2  token.Pos
	Post   Expr
	Rparen token.Pos
	Body   Stmt
}

// AsmStmt is gcc's inline assembly statement.
//
// The extended form is the one with colons in it, and the colons are what
// this node records: a template, the operands the template references by
// position, the registers the text destroys, and — for the goto form — the
// labels it may branch to. The basic form, `asm("...")` with no colon, is
// the same node with three empty lists; Extended tells them apart, because
// the difference is not the emptiness of the lists but what gcc guarantees
// about the registers, and only the extended form makes a promise.
//
// Template is nil only after a parse error. An operand's expression is an
// ordinary Expr: an output designates an object and is checked for that,
// an input is read like any other rvalue.
type AsmStmt struct {
	Span
	Keyword  token.Pos
	Volatile bool
	Inline   bool
	Goto     bool
	Extended bool
	Lparen   token.Pos
	Template *StringLit
	Outputs  []*AsmOperand
	Inputs   []*AsmOperand
	Clobbers []*StringLit
	Labels   []*Ident
	Rparen   token.Pos
	Semi     token.Pos
}

// AsmOperand is one entry of an asm statement's output or input list.
//
// Name is gcc's symbolic operand name, the `[x]` in `[x] "=r" (v)`, and is
// nil for an operand the template refers to by number. The two spellings
// name the same operand list — a symbolic name is an alias for a position,
// not a second way to declare one — so nothing downstream keeps them apart
// beyond resolving the name to its index.
type AsmOperand struct {
	Span
	Lbrack     token.Pos // NoPos when the operand has no symbolic name
	Name       *Ident
	Rbrack     token.Pos
	Constraint *StringLit
	Lparen     token.Pos
	X          Expr
	Rparen     token.Pos
}

// GotoStmt is goto Identifier ;
type GotoStmt struct {
	Span
	Goto  token.Pos
	Label *Ident
	Semi  token.Pos

	// Target is the operand of gcc's computed goto, `goto *expr;`, and nil
	// for the ordinary form. Exactly one of Label and Target is set.
	Target Expr
}

// ContinueStmt is continue ;
type ContinueStmt struct {
	Span
	Continue token.Pos
	Semi     token.Pos
}

// BreakStmt is break ;
type BreakStmt struct {
	Span
	Break token.Pos
	Semi  token.Pos
}

// ReturnStmt is return [Expression] ;
type ReturnStmt struct {
	Span
	Return token.Pos
	Result Expr // nil for a bare return
	Semi   token.Pos
}

func (*AsmStmt) stmtNode()      {}
func (*BadStmt) stmtNode()      {}
func (*LabeledStmt) stmtNode()  {}
func (*CaseStmt) stmtNode()     {}
func (*CompoundStmt) stmtNode() {}
func (*DeclStmt) stmtNode()     {}
func (*ExprStmt) stmtNode()     {}
func (*EmptyStmt) stmtNode()    {}
func (*IfStmt) stmtNode()       {}
func (*SwitchStmt) stmtNode()   {}
func (*WhileStmt) stmtNode()    {}
func (*DoStmt) stmtNode()       {}
func (*ForStmt) stmtNode()      {}
func (*GotoStmt) stmtNode()     {}
func (*ContinueStmt) stmtNode() {}
func (*BreakStmt) stmtNode()    {}
func (*ReturnStmt) stmtNode()   {}
