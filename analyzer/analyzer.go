// Package analyzer is phases 5–6 and the semantic half of phase 7:
// literal decoding and string concatenation, scopes and namespaces
// (ordinary, tags, labels), integer constant expressions, and the
// constraint checks the parser deliberately deferred — specifier
// multisets (via types), storage classes, bit-field widths, VLA
// placement, enum values, label discipline, K&R parameter matching,
// declared-object completeness.
//
// Not here yet, by design: expression typing beyond constant
// expressions, and initializer shape/arity — both land alongside
// lowering, which needs the same machinery. The seam is Info: what
// this package learns, it records there.
package analyzer

import (
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// Info is what analysis learned about a tree.
type Info struct {
	// Types maps declaring nodes — *ast.InitDeclarator,
	// *ast.ParamDecl, *ast.FieldDeclarator, *ast.FuncDecl,
	// *ast.TypeName — to the type they declare or denote.
	Types map[ast.Node]types.Type
	// Consts maps expressions that were required to be integer
	// constant expressions, and were, to their values.
	Consts map[ast.Expr]int64
	// Enums maps every enumerator to its value, implicit or explicit.
	Enums map[*ast.Enumerator]int64
}

// Check analyzes one translation unit against a target model. The
// Info is never nil; diagnostics are sorted and each mistake is
// reported once.
//
// packs is the #pragma pack state over the unit, in offset order. A caller
// with none — a tool handed a tree it did not preprocess — passes nil, and
// every record then lays out at its members' own alignment.
func Check(unit *token.File, file *ast.File, model types.Model, packs []PackAt) (*Info, []token.Diagnostic) {
	c := &checker{
		unit:  unit,
		model: model,
		packs: packs,
		info: &Info{
			Types:  map[ast.Node]types.Type{},
			Consts: map[ast.Expr]int64{},
			Enums:  map[*ast.Enumerator]int64{},
		},
	}
	c.push() // file scope
	c.declareBuiltinTypes()
	for _, d := range file.Decls {
		c.checkDecl(d, true)
	}
	c.pop()
	token.SortDiagnostics(c.diags)
	return c.info, c.diags
}

type checker struct {
	unit  *token.File
	model types.Model
	info  *Info
	diags []token.Diagnostic
	packs []PackAt

	scopes []*scope

	// per-function state
	labels map[string]*ast.LabeledStmt
	gotos  []*ast.GotoStmt
	// labelRefs are the labels named by &&label, which must exist for the
	// same reason a goto's must.
	labelRefs []*ast.Ident
	// asmLabels are the labels named by an `asm goto`, which must exist for
	// the same reason a goto's must — with the difference that the branch to
	// one is in the assembly rather than in this tree.
	asmLabels []*ast.Ident
	switchD   int

	// quiet suppresses reporting while an expression is typed for its type
	// alone. sizeof(expr) needs its operand's type and nothing else, and the
	// operand is walked again — or, in a _Static_assert, was never going to
	// be walked at all — so anything wrong with it is reported there or not
	// at all, but never twice from here. See quietType.
	quiet int

	// undeclared remembers the names already reported as undeclared, so one
	// misspelling in a loop body is one diagnostic rather than one per use.
	undeclared map[string]bool

	// fnRet is the return type of the function being checked, or nil outside
	// one. §6.8.6.4 checks a return value against it the way §6.5.16.1 checks
	// an assignment.
	fnRet types.Type

	loopD int
}

func (c *checker) report(n ast.Node, msg string) {
	if n == nil || c.quiet > 0 {
		return
	}
	c.diags = append(c.diags, token.Diagnostic{
		Pos: n.Pos(), End: n.End(), Severity: token.Error, Message: msg,
	})
}

func (c *checker) name(id *ast.Ident) string { return id.Name(c.unit) }

// A PackAt is one #pragma pack seen in the preprocessed text: the ceiling it
// put on a member's alignment, and the offset from which that applies. Pack
// is zero where the pragma asked for the target's own alignment back.
//
// The pragma is a fact about the structs that follow it rather than about
// any one of them, and the parser never sees it — phase 4 passes the line
// through and the bridge that re-scans its output drops it, because a
// directive in scanner input is not something phase 7 reads. So the offsets
// come from the printer, which is where the line was last seen, and this is
// the list Check consults as it reaches each record.
type PackAt struct {
	Off  int32
	Pack int64
}

// packAt is the ceiling in effect at a position in the unit.
func (c *checker) packAt(pos token.Pos) int64 {
	off := int32(pos) - 1
	pack := int64(0)
	for _, p := range c.packs {
		if p.Off > off {
			break
		}
		pack = p.Pack
	}
	return pack
}
