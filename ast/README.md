# ast

`package ast` defines the syntax tree vcc's parser builds.

```
import "github.com/vertex-language/vcc/ast"
```

Four hierarchies — `Expr`, `Stmt`, `Decl`, `Declarator` — mirroring Annex
A.2's four families. Declarators are first-class because in C the declarator
*is* the type syntax. Declaration specifiers (type specifiers, qualifiers,
storage classes) implement `Expr` so that a specifier list is one ordered
slice (`DeclSpecs`); they never appear in expression position — the parser
doesn't build such trees.

## Invariants

**Every node embeds a `Span`.** `Pos` and `End` are stored, not derived, so
even error-recovery nodes have a real, non-empty extent.

**Nodes hold no text.** An `Ident` is two positions; a literal is two
positions and a `token.Kind`. Decoding, escape interpretation, and phase-6
string concatenation belong to phases above this one.

```go
type Node interface {
	Pos() token.Pos // first byte
	End() token.Pos // one past the last byte
}
```

Marker methods are unexported, so the hierarchies are closed.

## Usage

```go
unit := token.NewFile("a.c", src)
file, diags := parser.ParseFile(unit, parser.DefaultMode)
defer file.Release()

ast.Inspect(file, func(n ast.Node) bool {
	if fn, ok := n.(*ast.FuncDecl); ok {
		p := unit.Position(fn.Name.Pos())
		fmt.Printf("%d:%d\t%s\n", p.Line, p.Column, fn.Name.Name(unit))
	}
	return true
})
```

Since the tree holds no strings, anything reading spelling takes the
`*token.File`: `ident.Name(f)`, `f.Slice(lit.Lo, lit.Hi)`,
`ast.Fdump(w, f, n)`.

## `File`

`File.Decls` holds the external declarations (each a `FuncDecl` or `Decl`);
`File.Unit` is the position space every span resolves through. `File.Comments`
retains comment tokens when the parser runs with `parser.ParseComments`.

```go
type Releaser interface {
	Release()
}
```

`Releaser` is the one-method window through which `ast` sees the parser's
arena. The parser calls `SetReleaser` to attach the tree's backing storage;
consumers call `File.Release` to give it back. `Release` is safe on a tree
with no releaser and safe to call twice, but **every node is invalid
afterwards** — copy what you need (usually a span and a string) before
calling it. `Release` is a promise, not a check: nothing detects a kept
pointer.

## Declarators

`Declarator.DeclName()` returns the declared identifier by walking the
declarator inside-out, or nil for an abstract declarator (a declarator with
a nil name). `int *(*f[3])(void)` reads as pointer‑to‑array‑of‑pointer, and
`DeclName()` finds `f` regardless of how deep it's nested.

`FuncDecl.Name` aliases the identifier inside `FuncDecl.Decl` — the same
node, not a copy — so it's tagged `ast:"-"` and `Walk` visits it exactly
once, through `Decl`.

## What the tree collapses

Grammar distinctions that are parsing devices, not shapes:

- **The precedence tower.** One `BinaryExpr` with a `token.Kind` operator;
  precedence lives in `token.Precedence`.
- **`ConstantExpression`** is an `Expr` — constant-ness is a check, not a
  shape.
- **Abstract declarators** are declarators with a nil `DeclName()`.
- **`CastExpr`** is an ordinary expression node.
- **Dangling else** resolves during parsing; `IfStmt` covers both forms.
- **Digraphs** collapse to canonical punctuators; `FlagDigraph` and
  `File.Raw` recover the spelling.

Where the distinction is real it survives: `SizeofExpr` has both `Type` and
`X` (exactly one non-nil); `_Atomic` exists as both `AtomicType` and a
qualifier flag; `TypedefType` records the parser's table match; `TypeName`
(the operand of casts, `sizeof`, `_Alignof`, `_Atomic()`, `_Generic`, and
compound literals) is `SpecifierQualifierList` plus an optional abstract
declarator.

## What the tree preserves for formatters

- **`DeclSpecs` in written order** (`int static const x;` is valid,
  deprecated placement and all).
- **`EmptyStmt`** and **`EmptyDecl`** keep stray semicolons visible.
- **Trailing commas**: `InitList.Comma`, `EnumDecl.Comma`.
- **Designators in written order**.
- **K&R definitions whole**: identifier-list parameters plus the
  `DeclarationList` before the body.
- **`static`/qualifier order in array declarators**:
  `ArrayDeclarator.StaticAfterQuals()` reports whether `static` was written
  after the qualifier list (`[const static n]` vs `[static const n]`).
- **Every delimiter position** (`Lbrace`, `Rparen`, `Semi`, …); `Semi` is
  `NoPos` where no `;` terminates (a `for`-init clause).
- **String literal runs**: `StringLit` holds one span per segment, prefixes
  included — `u8"a" "b"` is one node, two segments. Segments are extents,
  not children; `Walk` doesn't descend into them.

`BadExpr`, `BadStmt`, `BadDecl`, `BadDeclarator` cover tokens the parser
gave up on, so consumers can still report a location.

## Traversal and dumping

```go
func Walk(v Visitor, n Node)
func Inspect(n Node, f func(Node) bool) // f returns false to skip a subtree
```

Children are discovered by reflection over exported fields in source order,
so new fields traverse without `walk.go` changing. `Span` fields and fields
tagged `ast:"-"` are skipped, as are typed nils (e.g. an interface field
holding a nil `*BadExpr`). If the walk shows up in a profile, generate the
switch and keep this as the reference implementation.

`ast.Fdump(w, unit, node)` prints identifiers and literals with their
resolved text and positions as raw `line:column`, so dumps line up with what
the user typed even through trigraphs and splices.

## Dependencies

Imports only `token`, `reflect`, `fmt`, `io`. [`token`](../token) defines
spans and kinds; [`scanner`](../scanner) produces tokens; [`parser`](../parser)
builds this tree.