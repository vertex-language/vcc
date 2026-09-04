package lower

import (
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
)

// Which inline definitions this unit emits.
//
// §6.7.4p7 says an inline definition with no extern declaration anywhere in
// the unit provides no external definition, so a call to it is a reference
// another unit satisfies. That is the C99 rule and it is what glibc and the
// Darwin SDK are written against: the header declares the function inline, the
// library defines it, and the definition in the header is an optimization the
// compiler may use and need not emit.
//
// The Windows SDK is not written against it. `__inline` is MSVC's spelling and
// MSVC's meaning, which is C++'s: every unit that uses one emits a COMDAT copy
// and the linker keeps one. There is no library definition to fall back on —
// <stdio.h> defines printf as an inline wrapper around __stdio_common_vfprintf
// and ucrt.lib exports no printf at all — so a compiler that emits nothing
// links nothing.
//
// Emitting all of them is not the answer either, and that is what this file is
// for. <immintrin.h>, reached from <wchar.h>, defines inline helpers that call
// __arch_inverted and __isa_available — intrinsics the MSVC compiler resolves
// and no library provides. A unit that emits every inline definition it read
// therefore references symbols that exist nowhere, for functions it never
// called. The rule that works is the one every compiler applies to `static
// inline`: emit a definition when this unit uses it, and not otherwise.
//
// Usage is computed over the syntax rather than over the lowered module,
// because the decision has to be made in pass one — before a body is walked —
// and because an identifier in a body is exactly what "uses" means here. It is
// deliberately conservative: any mention of the name counts, including one
// under a #if that folded to false at a level this cannot see, and including
// an address taken rather than a call.

// The same rule covers the other kind of definition a unit may hold and not
// emit: a static function it never uses. Internal linkage means no other unit
// can name one, so a definition nothing here mentions is a definition nothing
// can call, and emitting it only drags in whatever it happens to reference.
// The Windows SDK's stralign.h is where that costs something: it defines
// `static __inline ua_CharUpperW` behind #if defined(CharUpper), and the
// worker that wrapper calls is in no import library an ordinary program
// links, so every program that includes <windows.h> would fail to link over
// a function it never called.
//
// Both kinds go in one closure because they reach each other: a static
// function called only from an inline definition is emitted exactly when
// that definition is, and neither answer can be given without the other.

// useSet is the set of definition names this unit will emit — the inline
// definitions and the static functions it uses.
//
// The closure starts at everything the unit emits unconditionally — an
// external function's body, a file-scope initializer — and follows names into
// the definitions they mention, then into the names those mention.
type useSet map[string]bool

// planUsed computes the closure. The result is always a real set, never nil,
// so a caller can index it without asking.
func (u *unit) planUsed() useSet {
	// Every definition whose emission depends on being used, by name, so the
	// walk below knows which names are worth following and which are
	// ordinary references.
	bodies := make(map[string]*ast.FuncDecl)
	for _, d := range u.file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		name := u.name(fd.Name)
		if sto, _ := specStorage(fd.Specs); sto == staticStorage || u.isInlineDefinition(name, fd) {
			bodies[name] = fd
		}
	}

	used := make(useSet, len(bodies))
	var work []string

	// The roots: everything the unit emits unconditionally.
	for _, d := range u.file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Body == nil || bodies[u.name(d.Name)] != nil {
				continue
			}
			work = append(work, u.mentions(d.Body, bodies)...)
		case *ast.GenDecl:
			work = append(work, u.mentions(d, bodies)...)
		}
	}

	// The closure. An inline definition that is emitted brings in whatever
	// its own body mentions, which is how printf reaches _vfprintf_l and
	// _vfprintf_l reaches __local_stdio_printf_options.
	for len(work) > 0 {
		name := work[len(work)-1]
		work = work[:len(work)-1]
		if used[name] {
			continue
		}
		used[name] = true
		work = append(work, u.mentions(bodies[name].Body, bodies)...)
	}
	return used
}

// mentions is every name in bodies that appears anywhere in n.
func (u *unit) mentions(n ast.Node, bodies map[string]*ast.FuncDecl) []string {
	var out []string
	ast.Inspect(n, func(x ast.Node) bool {
		id, ok := x.(*ast.Ident)
		if !ok {
			return true
		}
		if name := u.name(id); bodies[name] != nil {
			out = append(out, name)
		}
		return true
	})
	return out
}

// isInlineDefinition reports whether every declaration of name in this unit
// says inline and none says extern or static — §6.7.4p7's condition, and the
// one thing both rules above agree on. What the two rules disagree about is
// what to do with the answer.
func (u *unit) isInlineDefinition(name string, def *ast.FuncDecl) bool {
	if !specHas(def.Specs, token.INLINE) {
		return false
	}
	if sto, _ := specStorage(def.Specs); sto == staticStorage || sto == externStorage {
		return false
	}
	for _, d := range u.file.Decls {
		var specs ast.DeclSpecs
		switch d := d.(type) {
		case *ast.FuncDecl:
			if u.name(d.Name) != name {
				continue
			}
			specs = d.Specs
		case *ast.GenDecl:
			if !u.declaresName(d, name) {
				continue
			}
			specs = d.Specs
		default:
			continue
		}
		if sto, _ := specStorage(specs); sto == externStorage {
			return false
		}
		if !specHas(specs, token.INLINE) {
			return false
		}
	}
	return true
}
