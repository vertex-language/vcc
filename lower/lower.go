// lower.go
package lower

import (
	"fmt"

	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/analyzer"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// Options is everything lower needs from its caller that the tree does not say.
type Options struct {
	// Name is the module name: a bare identifier, from the primary source
	// file's stem. Empty means "a".
	Name string

	// Target supplies the use path and the ir.Layout the module opens with.
	Target ir.Target

	// Model must be the same model analyzer.Check ran against. sizeof
	// disagreeing between the two is a bug, not a configuration.
	Model types.Model

	// LongDouble overrides the register type long double is held in. Zero
	// means infer from Model.SizeLongDouble and the layout's extfloat list.
	LongDouble ir.RegType

	// SymbolPrefix is what a C identifier becomes in an object file: "_"
	// on Mach-O, empty on ELF and COFF.
	//
	// It is stated rather than derived from Target, because the mapping is
	// C's and not the IR's — nothing below this package renames a symbol,
	// and the platform that wants the underscore wants it on every C
	// identifier, static ones included. A module built with the wrong
	// prefix compiles and fails to link, naming what it could not find.
	SymbolPrefix string
}

// Lower emits one translation unit as a module.
//
// The module is never nil, even when diagnostics are returned: a partial
// module is what a --emit vir on broken input should print. Diagnostics are
// sorted, and a sticky builder failure inside ir surfaces as exactly one of
// them, since every call after the first is a no-op and reporting each would
// be a cascade.
func Lower(unit *token.File, file *ast.File, info *analyzer.Info, opt Options) (*ir.Module, []token.Diagnostic) {
	u := newUnit(unit, file, info, opt)
	u.declareFile()
	u.defineFile()
	if err := u.mod.Err(); err != nil {
		u.errorf(file, "internal: ir builder rejected this unit: %v", err)
	}
	token.SortDiagnostics(u.diags)
	return u.mod, u.diags
}

// unit is one translation unit's lowering state.
type unit struct {
	src   *token.File
	file  *ast.File
	info  *analyzer.Info
	model types.Model

	mod    *ir.Module
	target ir.Target
	layout ir.Layout
	types  *typeMap

	top   *scope // file scope
	scope *scope // innermost open scope
	fn    *fnState

	strs     map[string]ir.Symbol                    // pooled string literals, keyed by element run
	strCache map[*ast.StringLit]analyzer.StringValue // decoded-literal cache, keyed by node

	vars     map[string]*fileVar // file-scope object declarations, by name
	varOrder []*fileVar          // the same, in first-declaration order

	vlaExprs map[*types.Array]ast.Expr // a VLA dimension's length expression

	// clits are the globals file-scope compound literals became, keyed by the
	// syntax that wrote them. One literal is one object however many times
	// its address is folded, and folding is not once: an initializer may be
	// walked for its constant value and again for its designator.
	clits map[*ast.CompoundLit]ir.Symbol

	warned map[string]bool // warnOnce keys already reported

	// inlines is which inline definitions this unit emits, on a target
	// whose rule is "emit the ones this unit uses". Nil until asked for;
	// see inlineset.go.
	used useSet

	symPrefix string // Options.SymbolPrefix, prepended by sym

	// asmNames maps a C name to the assembler label a declaration gave it.
	// See collectAsmLabels.
	asmNames map[string]string

	diags []token.Diagnostic
	anon  int
}

func newUnit(src *token.File, file *ast.File, info *analyzer.Info, opt Options) *unit {
	name := opt.Name
	if name == "" {
		name = "a"
	}
	mod := ir.NewModule(name, opt.Target)
	top := newScope(nil)
	u := &unit{
		src:      src,
		file:     file,
		info:     info,
		model:    opt.Model,
		mod:      mod,
		target:   opt.Target,
		layout:   opt.Target.Layout(),
		top:      top,
		scope:    top,
		strs:     map[string]ir.Symbol{},
		strCache: map[*ast.StringLit]analyzer.StringValue{},
		vars:     map[string]*fileVar{},
		vlaExprs: map[*types.Array]ast.Expr{},
		clits:    map[*ast.CompoundLit]ir.Symbol{},
		warned:   map[string]bool{},

		symPrefix: opt.SymbolPrefix,
	}
	u.types = newTypeMap(u, opt.LongDouble)
	return u
}

// sym is the object-file name of a C identifier. Every name this package
// hands to ir goes through here, including the ones it invents for string
// literals and static locals: on Mach-O a local label is underscored too, and
// a symbol nobody outside the module names is not worth an exception.
// sym is the symbol a C name is emitted under.
//
// An assembler label replaces the name outright, prefix and all: gcc emits
// `__asm__("open64")` as open64 and not as _open64 even on a platform whose
// C names carry the underscore, because the label is written by someone
// naming a symbol rather than an object. That is the whole point of it in a
// libc, where the label is how one C declaration reaches a differently named
// definition.
func (u *unit) sym(name string) string {
	if s, ok := u.asmNames[name]; ok {
		return s
	}
	return u.symPrefix + name
}

// declareFile is pass one: every file-scope declarator gets an ir symbol and a
// file-scope binding, so a body may name something declared below it.
//
// A definition is not emitted here, only named. A tentative definition
// (§6.9.2p2) is recorded and resolved at the end of the pass, when it is
// finally known whether a real definition followed.
func (u *unit) declareFile() {
	u.collectAsmLabels()
	for _, d := range u.file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			u.declareFunc(d)
		case *ast.GenDecl:
			u.declareGen(d)
		case *ast.AsmDecl:
			u.asmDecl(d)
		case *ast.StaticAssertDecl, *ast.EmptyDecl, *ast.BadDecl:
			// Nothing to emit: the analyzer already had the last word.
		}
	}
	u.emitTentative()
}

// defineFile is pass two: initializers and function bodies.
func (u *unit) defineFile() {
	for _, d := range u.file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Body != nil {
				u.defineFunc(d)
			}
		case *ast.GenDecl:
			u.defineGen(d)
		}
	}
}

// typeOf returns the type analysis recorded for a declaring node.
//
// A miss is a lower bug or an analyzer bug, never user error: every node this
// is asked about is one Info documents itself as covering. It reports and
// yields int so that emission continues and the user sees one diagnostic
// rather than a nil dereference.
func (u *unit) typeOf(n ast.Node) types.Type {
	if t, ok := u.info.Types[n]; ok && t != nil {
		return t
	}
	u.errorf(n, "internal: no type recorded for %T", n)
	return types.Typ(types.Int)
}

// constOf returns the value of an expression the analyzer required to be an
// integer constant expression.
func (u *unit) constOf(e ast.Expr) (int64, bool) {
	v, ok := u.info.Consts[e]
	return v, ok
}

func (u *unit) sizeof(t types.Type, at ast.Node) int64 {
	n, ok := u.model.Sizeof(t)
	if !ok {
		u.errorf(at, "size of incomplete type %s is not known here", t)
		return 0
	}
	return n
}

func (u *unit) alignof(t types.Type, at ast.Node) int64 {
	n, ok := u.model.Alignof(t)
	if !ok || n < 1 {
		u.errorf(at, "alignment of %s is not known here", t)
		return 1
	}
	return n
}

func (u *unit) errorf(n ast.Node, format string, args ...any) {
	u.report(n, token.Error, fmt.Sprintf(format, args...))
}

func (u *unit) warnf(n ast.Node, format string, args ...any) {
	u.report(n, token.Warn, fmt.Sprintf(format, args...))
}

func (u *unit) report(n ast.Node, sev token.Severity, msg string) {
	pos, end := n.Pos(), n.End()
	if !pos.IsValid() {
		pos, end = u.file.Pos(), u.file.End()
	}
	u.diags = append(u.diags, token.Diagnostic{
		Pos: pos, End: end, Severity: sev, Message: msg,
	})
}

// name returns an identifier's spelling.
func (u *unit) name(id *ast.Ident) string {
	if id == nil {
		return ""
	}
	return id.Name(u.src)
}

// uniq mints a module-unique name for a thing C did not name: an anonymous
// record, a compound literal's backing object, a synthesized func typedef, a
// generated block label.
//
// The separator is '_', not '.': every name this produces is handed straight
// to ir as a symbol or block label, and ir.validIdent admits only letters,
// digits, and '_' (never checked here — this package has no business
// re-implementing that rule, only obeying it). A dot compiled and ran right
// up until m.Err() was finally consulted, at which point every generated
// block in every function became "invalid name" — this is the fix, not a
// workaround for it.
func (u *unit) uniq(prefix string) string {
	u.anon++
	return fmt.Sprintf("%s_%d", prefix, u.anon)
}
