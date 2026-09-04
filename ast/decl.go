package ast

import "github.com/vertex-language/vcc/token"

// DeclSpecs is a declaration-specifier list in written order —
// `int static const x;` is valid, deprecated placement and all.
// Constraint checking (specifier multisets, at most one storage
// class) is the type-building phase's job.
type DeclSpecs []Expr

// KeywordSpec is any single-keyword specifier or qualifier: a storage
// class, a builtin type specifier, const/restrict/volatile, _Atomic
// as a qualifier (no parenthesis), inline, _Noreturn.
type KeywordSpec struct {
	Span
	Kind token.Kind
}

// AlignasSpec is _Alignas(TypeName) or _Alignas(ConstantExpression):
// exactly one of Type and X is non-nil.
type AlignasSpec struct {
	Span
	Alignas token.Pos
	Lparen  token.Pos
	Type    *TypeName
	X       Expr
	Rparen  token.Pos
}

// TypeofType is gcc's typeof: a type specifier that names the type of an
// expression, or of a type name. Exactly one of Type and X is non-nil.
//
// It is here rather than tolerated because a macro written with it has no
// other spelling — __typeof__(a) _x = (a) is how a header writes a
// temporary whose type it does not know — and a compiler that cannot read
// it cannot read the header.
type TypeofType struct {
	Span
	Keyword token.Pos
	Lparen  token.Pos
	Type    *TypeName
	X       Expr
	Rparen  token.Pos
}

// AtomicType is _Atomic(TypeName) — the type-specifier form. The
// qualifier form is a KeywordSpec; _Atomic exists as both.
type AtomicType struct {
	Span
	Atomic token.Pos
	Lparen token.Pos
	Type   *TypeName
	Rparen token.Pos
}

// StructType is a struct-or-union specifier; Kind is STRUCT or UNION.
// Fields is nil for the incomplete form (no brace list); each member
// is a *FieldDecl or *StaticAssertDecl.
type StructType struct {
	Span
	Keyword token.Pos
	Kind    token.Kind // STRUCT or UNION
	Name    *Ident     // nil for anonymous
	Lbrace  token.Pos  // NoPos for the incomplete form
	Fields  []Decl
	Rbrace  token.Pos

	// Attrs are the attributes written on the specifier itself, in either
	// position gcc admits: between the keyword and the tag, and after the
	// closing brace. They belong to the type rather than to a declaration of
	// it, which is what makes `struct __attribute__((packed)) s` a packed
	// struct everywhere s is named.
	Attrs []*Attr
}

// Attr is one entry of an __attribute__((...)) list.
//
// The name is kept as written, both spellings: gcc allows every attribute to
// be spelled with two leading and two trailing underscores, so that a macro
// named `packed` cannot break a header that says `__packed__`. Args are the
// parenthesized arguments, unparsed beyond being expressions — most
// attributes take identifiers that name nothing in scope (`format(printf, 1,
// 2)`), so they are not analyzed as expressions and only the attributes that
// need a value read one.
type Attr struct {
	Span
	Name   *Ident
	Lparen token.Pos // NoPos when the attribute takes no arguments
	Args   []Expr
	Rparen token.Pos
}

// AttrSpec carries the attributes written among a declaration's specifiers.
// It is an Expr because DeclSpecs is a list of them; it specifies nothing on
// its own.
type AttrSpec struct {
	Span
	Attrs []*Attr
}

// EnumDecl is an enum specifier. It sits in specifier position but is
// named for what it does: declare constants. Comma records a trailing
// comma (NoPos if absent); List is nil for the incomplete form.
type EnumDecl struct {
	Span
	Enum   token.Pos
	Name   *Ident    // nil for anonymous
	Lbrace token.Pos // NoPos for the incomplete form
	List   []*Enumerator
	Comma  token.Pos // trailing comma, NoPos if absent
	Rbrace token.Pos
}

// Enumerator is Name [= Value].
type Enumerator struct {
	Span
	Name   *Ident
	Assign token.Pos // NoPos when no initializer
	Value  Expr
}

// TypedefType records the parser's typedef-table match: an identifier
// used as a type specifier.
type TypedefType struct {
	Span
	Name *Ident
}

// TypeName is SpecifierQualifierList [AbstractDeclarator] — the
// operand of casts, sizeof, _Alignof, _Atomic(), _Generic, and
// compound literals.
type TypeName struct {
	Span
	Specs DeclSpecs
	Decl  Declarator // nil when absent entirely
}

func (*KeywordSpec) exprNode() {}
func (*AttrSpec) exprNode()    {}
func (*AlignasSpec) exprNode() {}
func (*AtomicType) exprNode()  {}
func (*TypeofType) exprNode()  {}
func (*StructType) exprNode()  {}
func (*EnumDecl) exprNode()    {}
func (*TypedefType) exprNode() {}
func (*TypeName) exprNode()    {}

// ---- declarations ----

// BadDecl covers a declaration the parser gave up on.
type BadDecl struct {
	Span
}

// GenDecl is one ordinary declaration:
// DeclarationSpecifiers [InitDeclaratorList] ;
type GenDecl struct {
	Span
	Specs DeclSpecs
	List  []*InitDeclarator
	Semi  token.Pos
}

// InitDeclarator is Declarator [= Initializer].
//
// AsmLabel is gcc's assembler label, the `__asm__("name")` written after a
// declarator, and is nil when there is none. It renames the symbol and
// nothing else: the object keeps its C name, its type and its linkage, and
// only the name the linker sees changes. System headers use it heavily —
// it is how a libc points `fopen` at `fopen64` without a macro.
type InitDeclarator struct {
	Span
	Decl     Declarator
	AsmLabel *StringLit
	// Attrs are the attributes written on this declarator rather than in
	// the declaration's specifiers. `int a __attribute__((aligned(16))), b;`
	// aligns a and not b, which is why they are here and not there.
	Attrs  []*Attr
	Assign token.Pos // NoPos when no initializer
	Init   Expr      // expression or *InitList
}

// FuncDecl is a function definition:
// DeclarationSpecifiers Declarator [DeclarationList] CompoundStatement.
// KR holds the declaration list of a K&R definition, kept whole.
//
// Name aliases the identifier inside Decl — the same node, not a
// copy — so Walk skips it.
type FuncDecl struct {
	Span
	Specs    DeclSpecs
	Decl     Declarator
	AsmLabel *StringLit // gcc's assembler label; see InitDeclarator
	Name     *Ident     `ast:"-"` // alias into Decl; never nil in a valid definition
	KR       []*GenDecl
	Body     *CompoundStmt
}

// FieldDecl is one struct/union member declaration:
// SpecifierQualifierList [StructDeclaratorList] ;
type FieldDecl struct {
	Span
	Specs DeclSpecs
	List  []*FieldDeclarator
	Semi  token.Pos
}

// FieldDeclarator is [Declarator] [: ConstantExpression]. A bit-field
// has a valid Colon; an unnamed bit-field has a nil Decl too.
type FieldDeclarator struct {
	Span
	Decl  Declarator
	Colon token.Pos // NoPos unless a bit-field
	Width Expr      // constant-ness is a check, not a shape
}

// StaticAssertDecl is _Static_assert ( ConstantExpression , StringLiteral ) ;
type StaticAssertDecl struct {
	Span
	Keyword token.Pos
	Lparen  token.Pos
	Cond    Expr
	Comma   token.Pos
	Msg     *StringLit
	Rparen  token.Pos
	Semi    token.Pos
}

// EmptyDecl keeps a stray file-scope semicolon visible.
type EmptyDecl struct {
	Span
	Semi token.Pos
}

// AsmDecl is gcc's file-scope assembly: `asm("...")` outside any function.
//
// It is a declaration only in the grammatical sense of appearing where one
// does. It declares nothing — whatever its text defines, it defines to the
// linker and not to this translation unit, so a name it emits is not in the
// file's namespace and cannot be called, aliased, or given a type. That is
// the whole of what file-scope assembly means, and the reason this node has
// no name field: giving it one would be claiming knowledge of the text.
type AsmDecl struct {
	Span
	Keyword  token.Pos
	Lparen   token.Pos
	Template *StringLit
	Rparen   token.Pos
	Semi     token.Pos
}

func (*BadDecl) declNode()          {}
func (*AsmDecl) declNode()          {}
func (*GenDecl) declNode()          {}
func (*FuncDecl) declNode()         {}
func (*FieldDecl) declNode()        {}
func (*StaticAssertDecl) declNode() {}
func (*EmptyDecl) declNode()        {}
