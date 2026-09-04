package ast

import "github.com/vertex-language/vcc/token"

// BadExpr covers tokens the parser gave up on. Its span is non-empty
// even when nothing was consumed.
type BadExpr struct {
	Span
}

// BasicLit is an undecoded INT_LIT, FLOAT_LIT, or CHAR_LIT: two
// positions and a kind. Slice the span for the raw spelling.
type BasicLit struct {
	Span
	Kind token.Kind
}

// StringLit is one phase-6 run of adjacent string literals: one node,
// one span per segment, prefixes included — u8"a" "b" is one node,
// two segments. Concatenation and decoding happen above this package.
type StringLit struct {
	Span
	Segs []Span
}

// ParenExpr is ( X ).
type ParenExpr struct {
	Span
	Lparen token.Pos
	X      Expr
	Rparen token.Pos
}

// GenericExpr is a C11 _Generic selection.
type GenericExpr struct {
	Span
	Generic token.Pos
	Lparen  token.Pos
	Ctrl    Expr
	Assocs  []*GenericAssoc
	Rparen  token.Pos
}

// GenericAssoc is one association: TypeName : expr, or default : expr
// (Type nil, Default valid).
type GenericAssoc struct {
	Span
	Type    *TypeName // nil for default
	Default token.Pos // NoPos unless the default association
	Colon   token.Pos
	Value   Expr
}

// IndexExpr is X[Index].
type IndexExpr struct {
	Span
	X      Expr
	Lbrack token.Pos
	Index  Expr
	Rbrack token.Pos
}

// CallExpr is Fun(Args...).
type CallExpr struct {
	Span
	Fun    Expr
	Lparen token.Pos
	Args   []Expr
	Rparen token.Pos
}

// SelectorExpr is X.Sel or X->Sel; Op distinguishes PERIOD from ARROW.
type SelectorExpr struct {
	Span
	X     Expr
	OpPos token.Pos
	Op    token.Kind // PERIOD or ARROW
	Sel   *Ident
}

// IncDecExpr is the postfix X++ or X--. Prefix forms are UnaryExpr.
type IncDecExpr struct {
	Span
	X     Expr
	OpPos token.Pos
	Op    token.Kind // INC or DEC
}

// CompoundLit is ( TypeName ) { InitializerList }.
type CompoundLit struct {
	Span
	Lparen token.Pos
	Type   *TypeName
	Rparen token.Pos
	Init   *InitList
}

// UnaryExpr is a prefix operator: & * + - ~ ! and prefix ++ --.
type UnaryExpr struct {
	Span
	OpPos token.Pos
	Op    token.Kind
	X     Expr
}

// SizeofExpr is sizeof X or sizeof ( TypeName ): exactly one of X and
// Type is non-nil. Lparen/Rparen are NoPos in the operand form.
type SizeofExpr struct {
	Span
	Sizeof token.Pos
	Lparen token.Pos
	Type   *TypeName
	X      Expr
	Rparen token.Pos
}

// AlignofExpr is _Alignof ( TypeName ).
type AlignofExpr struct {
	Span
	Alignof token.Pos
	Lparen  token.Pos
	Type    *TypeName
	Rparen  token.Pos
}

// CastExpr is ( TypeName ) X — an ordinary expression node.
type CastExpr struct {
	Span
	Lparen token.Pos
	Type   *TypeName
	Rparen token.Pos
	X      Expr
}

// BinaryExpr collapses the precedence tower: one node, a token.Kind
// operator (the ten binary levels plus COMMA). Precedence lives in
// token.Precedence.
type BinaryExpr struct {
	Span
	X     Expr
	OpPos token.Pos
	Op    token.Kind
	Y     Expr
}

// CondExpr is Cond ? Then : Else.
type CondExpr struct {
	Span
	Cond     Expr
	Question token.Pos
	Then     Expr
	Colon    token.Pos
	Else     Expr
}

// AssignExpr is Lhs op= Rhs. Right-associative; kept apart from
// BinaryExpr because its left operand is constrained to a unary
// expression and it is not driven by the precedence table.
type AssignExpr struct {
	Span
	Lhs   Expr
	OpPos token.Pos
	Op    token.Kind // ASSIGN, MUL_ASSIGN, …
	Rhs   Expr
}

// StmtExpr is gcc's statement expression: ({ ... }) in expression position.
// Its value is the value of the last statement in the block, which must be
// an expression statement for the whole to have a value.
//
// It is here rather than tolerated because there is no other way to write
// what it writes: a macro that must evaluate an argument exactly once, name
// the result, and still be an expression has this and nothing else, which is
// why every non-trivial header macro is built on it.
type StmtExpr struct {
	Span
	Lparen token.Pos
	Body   *CompoundStmt
	Rparen token.Pos
}

// LabelAddrExpr is gcc's &&label: the address of a label, for use as the
// target of a computed goto. It exists so an interpreter's dispatch loop can
// be a table of labels, which is the one thing a switch cannot be made into.
type LabelAddrExpr struct {
	Span
	AndAnd token.Pos
	Label  *Ident
}

// OffsetofExpr is __builtin_offsetof(type, member). It is a builtin rather
// than a call because its first argument is a type name and its second is a
// member designator, and neither is an expression.
type OffsetofExpr struct {
	Span
	Keyword token.Pos
	Lparen  token.Pos
	Type    *TypeName
	Member  Expr // a designator chain: Ident, then .name and [index]
	Rparen  token.Pos
}

// VaArgExpr is gcc's and clang's __builtin_va_arg(ap, type): the next
// variadic argument, read at a type the second operand names.
//
// vcc's own <stdarg.h> writes va_arg as an address computation over
// __builtin_va_arg_ref instead, which needs no new syntax. This exists
// because clang's <stdarg.h> does not, and a .i file preprocessed by clang
// carries whichever one its headers used.
type VaArgExpr struct {
	Span
	Keyword token.Pos
	Lparen  token.Pos
	Ap      Expr
	Type    *TypeName
	Rparen  token.Pos
}

// TypesCompatibleExpr is gcc's and clang's
// __builtin_types_compatible_p(type, type). Both operands are type names, so
// like OffsetofExpr it cannot be a call, and its value is decided at compile
// time — the whole point is that a macro may branch on it.
type TypesCompatibleExpr struct {
	Span
	Keyword token.Pos
	Lparen  token.Pos
	A, B    *TypeName
	Rparen  token.Pos
}

// InitList is a braced initializer; it is an Expr because an
// Initializer is either an assignment expression or a braced list.
// Comma records a trailing comma's position (NoPos if absent).
type InitList struct {
	Span
	Lbrace token.Pos
	Items  []*InitItem
	Comma  token.Pos // trailing comma, NoPos if absent
	Rbrace token.Pos
}

// InitItem is one [Designation] Initializer, designators in written
// order.
type InitItem struct {
	Span
	Designators []Node    // *IndexDesignator or *FieldDesignator
	Assign      token.Pos // NoPos when no designation
	Value       Expr      // expression or nested *InitList
}

// IndexDesignator is [ ConstantExpression ].
type IndexDesignator struct {
	Span
	Lbrack token.Pos
	Index  Expr

	// High is the upper bound of gcc's designator range, `[lo ... hi] =`,
	// and nil for the ordinary single-element form. Inclusive at both ends.
	High Expr

	Rbrack token.Pos
}

// FieldDesignator is . Identifier.
type FieldDesignator struct {
	Span
	Dot  token.Pos
	Name *Ident
}

func (*BadExpr) exprNode()     {}
func (*BasicLit) exprNode()    {}
func (*StringLit) exprNode()   {}
func (*ParenExpr) exprNode()   {}
func (*GenericExpr) exprNode() {}
func (*IndexExpr) exprNode()   {}
func (*CallExpr) exprNode()    {}
func (*SelectorExpr) exprNode(){}
func (*IncDecExpr) exprNode()  {}
func (*CompoundLit) exprNode() {}
func (*UnaryExpr) exprNode()   {}
func (*SizeofExpr) exprNode()  {}
func (*AlignofExpr) exprNode() {}
func (*CastExpr) exprNode()    {}
func (*BinaryExpr) exprNode()  {}
func (*CondExpr) exprNode()    {}
func (*AssignExpr) exprNode()  {}
func (*InitList) exprNode()    {}
func (*StmtExpr) exprNode()   {}
func (*LabelAddrExpr) exprNode() {}
func (*OffsetofExpr) exprNode()  {}
func (*VaArgExpr) exprNode()     {}

func (*TypesCompatibleExpr) exprNode() {}