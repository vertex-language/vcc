package ast

import "github.com/vertex-language/vcc/token"

// Declarators are kept exactly as written — int *(*f[3])(void) stays
// a declarator tree the type-construction phase reads inside-out.
// An abstract declarator is the same tree with a nil name; a fully
// absent declarator is a nil Declarator field on its owner.

// BadDeclarator covers a declarator the parser gave up on.
type BadDeclarator struct {
	Span
}

// NameDeclarator is the leaf: the declared identifier.
type NameDeclarator struct {
	Span
	Ident *Ident
}

// PtrDeclarator is * [TypeQualifierList] Inner. Inner is nil in an
// abstract declarator that is just pointers.
type PtrDeclarator struct {
	Span
	Star  token.Pos
	Quals DeclSpecs // qualifier KeywordSpecs, written order
	Inner Declarator
}

// ParenDeclarator is ( Inner ).
type ParenDeclarator struct {
	Span
	Lparen token.Pos
	Inner  Declarator
	Rparen token.Pos
}

// ArrayDeclarator is Inner [ ... ]. Static and Star record the §6.7.6.2
// forms; their placement constraints are checked later, not here.
type ArrayDeclarator struct {
	Span
	Inner  Declarator // nil in an abstract declarator like int[3]
	Lbrack token.Pos
	Static token.Pos // NoPos if absent; before or after Quals per StaticAfterQuals
	Quals  DeclSpecs
	Len    Expr      // nil for [], [*], [static …]'s expression otherwise
	Star   token.Pos // the VLA-of-unspecified-size [*], NoPos if absent
	Rbrack token.Pos
}

// StaticAfterQuals reports whether static was written after the
// qualifier list ([const static n] vs [static const n]).
func (d *ArrayDeclarator) StaticAfterQuals() bool {
	return d.Static.IsValid() && len(d.Quals) > 0 && d.Quals[0].Pos() < d.Static
}

// FuncDeclarator is Inner ( ParameterTypeList ) or Inner ( [IdentifierList] ).
// A prototype fills Params (Ellipsis marks , ...); a K&R declarator
// fills Idents; `f()` has both nil.
type FuncDeclarator struct {
	Span
	Inner    Declarator
	Lparen   token.Pos
	Params   []*ParamDecl
	Ellipsis token.Pos // NoPos if absent
	Idents   []*Ident  // K&R identifier list
	Rparen   token.Pos
}

// ParamDecl is DeclarationSpecifiers [Declarator | AbstractDeclarator].
type ParamDecl struct {
	Span
	Specs DeclSpecs
	Decl  Declarator // nil when absent entirely, e.g. f(int)
}

func (*BadDeclarator) declaratorNode()   {}
func (*NameDeclarator) declaratorNode()  {}
func (*PtrDeclarator) declaratorNode()   {}
func (*ParenDeclarator) declaratorNode() {}
func (*ArrayDeclarator) declaratorNode() {}
func (*FuncDeclarator) declaratorNode()  {}

func (d *BadDeclarator) DeclName() *Ident   { return nil }
func (d *NameDeclarator) DeclName() *Ident  { return d.Ident }
func (d *PtrDeclarator) DeclName() *Ident   { return declName(d.Inner) }
func (d *ParenDeclarator) DeclName() *Ident { return declName(d.Inner) }
func (d *ArrayDeclarator) DeclName() *Ident { return declName(d.Inner) }
func (d *FuncDeclarator) DeclName() *Ident  { return declName(d.Inner) }

func declName(d Declarator) *Ident {
	if d == nil {
		return nil
	}
	return d.DeclName()
}
