// Package ast defines the syntax tree vcc's parser builds.
//
// Four hierarchies — Expr, Stmt, Decl, Declarator — mirroring Annex
// A.2's four families. Declarators are first-class because in C the
// declarator is the type syntax. Declaration specifiers (type
// specifiers, qualifiers, storage classes) implement Expr so that a
// specifier list is one ordered slice; they never appear in
// expression position — the parser doesn't build such trees.
//
// Invariants:
//
//   - Every node embeds a Span. Pos and End are stored, not derived,
//     so even error-recovery nodes have a real, non-empty extent.
//   - Nodes hold no text. An Ident is two positions; a literal is two
//     positions and a token.Kind. Decoding, escape interpretation,
//     and phase-6 string concatenation belong to phases above this
//     one. Anything reading spelling takes the *token.File.
package ast

import "github.com/vertex-language/vcc/token"

// Node is the interface all tree nodes implement.
type Node interface {
	Pos() token.Pos // first byte
	End() token.Pos // one past the last byte
}

// Span is the stored extent every node embeds.
type Span struct {
	Lo token.Pos // inclusive
	Hi token.Pos // exclusive
}

func (s Span) Pos() token.Pos { return s.Lo }
func (s Span) End() token.Pos { return s.Hi }

// The four hierarchies. Marker methods are unexported, so the
// hierarchies are closed.

type Expr interface {
	Node
	exprNode()
}

type Stmt interface {
	Node
	stmtNode()
}

type Decl interface {
	Node
	declNode()
}

// Declarator is the type-syntax hierarchy. DeclName returns the
// declared identifier, or nil — an abstract declarator is a
// declarator with a nil name.
type Declarator interface {
	Node
	DeclName() *Ident
	declaratorNode()
}

// Ident is two positions; spelling resolves through the File that
// produced it.
type Ident struct {
	Span
}

// Name returns the identifier's spelling from its translation unit.
func (id *Ident) Name(f *token.File) string {
	return string(f.Slice(id.Lo, id.Hi))
}

func (*Ident) exprNode() {}

// Releaser is the one-method window through which ast sees the
// parser's arena.
type Releaser interface {
	Release()
}

// File is one translation unit's tree.
type File struct {
	Span
	Unit     *token.File   // the position space every span resolves through
	Decls    []Decl        // external declarations: FuncDecl or Decl
	Comments []token.Token // retained under parser.ParseComments

	rel Releaser
}

// SetReleaser attaches the tree's backing storage. The parser calls
// this; consumers call Release.
func (f *File) SetReleaser(r Releaser) { f.rel = r }

// Release returns the tree's backing storage. It is safe on a tree
// with no releaser and safe to call twice, but every node is invalid
// afterwards — copy what you need (usually a span and a string)
// before calling it. Release is a promise, not a check: nothing
// detects a kept pointer.
func (f *File) Release() {
	if f.rel != nil {
		r := f.rel
		f.rel = nil
		r.Release()
	}
}
