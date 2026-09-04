package lower

import (
	"strings"

	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// fileVar accumulates every file-scope declaration of one name.
//
// C lets a name be declared many times and defined once, and does not say
// which declaration carries the initializer until the file ends. So pass one
// records rather than emits, and emitTentative decides at the end what each
// name became: a definition, a zero-initialized tentative definition, or a
// reference to another translation unit.
type fileVar struct {
	name  string
	typ   types.Type
	decl  ast.Node
	link  linkage
	tls   bool
	align int64

	init *ast.InitDeclarator // the declarator that carried "= ...", or nil
	tent bool                // a tentative definition was seen
	ext  bool                // an extern declaration was seen

	g   *ir.Global
	obj *object
}

// declareFunc names a function and, where this unit defines it, creates the
// definition its body will be emitted into.
func (u *unit) declareFunc(d *ast.FuncDecl) {
	u.declareEnums(d.Specs)

	name := u.name(d.Name)
	if name == "" {
		return
	}
	ft, ok := types.Unqualify(u.typeOf(d)).(*types.Func)
	if !ok {
		u.errorf(d, "internal: %s does not declare a function type", name)
		return
	}
	sto, _ := specStorage(d.Specs)
	link := fileLinkage(sto, true)

	if o := u.top.objs[name]; o != nil && o.isStatic() {
		// Already named by an earlier declaration. A definition arriving now
		// upgrades nothing: emitFuncSymbol below already chose definition over
		// import by consulting the whole file.
		return
	}
	u.emitFuncSymbol(name, ft, link, d, d.Body != nil && !u.notEmitted(name, d))
}

// emitFuncSymbol creates the module item for a function name and binds it.
func (u *unit) emitFuncSymbol(name string, ft *types.Func, link linkage, at ast.Node, define bool) *object {
	if define {
		ft = u.definedType(name, ft)
	}
	o := &object{name: name, typ: ft, decl: at, sto: externStorage, link: link}
	if define {
		f := u.mod.Func(u.sym(name))
		if link == internalLinkage {
			f.Internal()
		} else {
			f.Export()
			// An inline definition that is emitted is emitted weak.
			//
			// §6.7.4p7 says a unit that declares a function inline and, in
			// one of those declarations, extern provides the external
			// definition — and §6.9p5 allows the program exactly one. A
			// header is where that rule breaks. Darwin's <stdio.h> resolves
			// __header_inline to `extern __inline` for any compiler that
			// defines __GNUC__ and is not clang, so every unit including it
			// provides an external definition of __sputc, and two of them
			// collide on a symbol neither one wrote.
			//
			// Weak is the linker's word for "these are the same one, keep
			// either", and it is what both the ELF and Mach-O rules already
			// coalesce. A real out-of-line definition still displaces them
			// all, which is the resolution §6.7.4p7 has in mind. What stays
			// an error is what should be: two units defining the same
			// ordinary function.
			if u.inlineDefinition(name) {
				f.Weak()
			}
		}
		// The signature belongs to the declaration, not to the body: a call
		// this unit emits before it walks the callee's body must see the real
		// arity. See object.sig.
		o.sig = u.declareSig(f, ft, at)
		o.sym, o.call = f, f
	} else {
		// Named, not created: see object.pending. u.imported turns this into
		// an ir.FuncImport at the first call or address-of.
		o.pending = true
	}
	u.top.objs[name] = o
	return o
}

// notEmitted reports whether this unit emits no symbol for a definition it
// holds — because another unit provides it, or because nothing can reach it.
//
// A static function nothing in this unit mentions is the second case, and it
// applies on every target: internal linkage means no other unit can name it,
// so a definition nothing here uses is one nothing can call. Emitting it
// only drags in whatever it happens to reference, which is what stops a
// program that includes <windows.h> from linking. See inlineset.go.
//
// For an inline definition there are two rules, and which one applies is the
// target's. Under §6.7.4p7 a function declared inline in every declaration in
// the unit, with no extern anywhere,
// provides an inline definition and not an external one; that is what a C99
// libc is written against, and it is where "not a GNU C compiler" costs
// something visible — gnu89 inline emitted the body, C99 does not, and code
// written for the former links against nothing. The diagnostic below is the
// whole warning it gets.
//
// Under the Microsoft ABI the rule is MSVC's, which is C++'s: a unit that uses
// an inline definition emits its own copy and the linker keeps one. It has to
// be, because the Windows SDK provides no library definition to fall back on —
// ucrt.lib exports no printf, only the __stdio_common_vfprintf that <stdio.h>
// wraps inline. See inlineset.go for the "uses" half, which is the part that
// keeps a unit from emitting the intrinsics-based helpers in <immintrin.h> and
// referencing symbols no library has.
func (u *unit) notEmitted(name string, def *ast.FuncDecl) bool {
	if sto, _ := specStorage(def.Specs); sto == staticStorage {
		return !u.usedDefs()[name]
	}
	if !u.isInlineDefinition(name, def) {
		return false
	}
	if u.layout.ABI == "ms" {
		return !u.usedDefs()[name]
	}
	u.warnf(def, "inline definition of %s provides no external definition (§6.7.4p7); "+
		"add extern to one declaration if this unit should emit it", name)
	return true
}

// usedDefs is the closure, computed once per unit.
func (u *unit) usedDefs() useSet {
	if u.used == nil {
		u.used = u.planUsed()
	}
	return u.used
}

func (u *unit) declaresName(d *ast.GenDecl, name string) bool {
	for _, id := range d.List {
		if n := id.Decl.DeclName(); n != nil && u.name(n) == name {
			return true
		}
	}
	return false
}

// declareGen records one file-scope declaration.
func (u *unit) declareGen(d *ast.GenDecl) {
	u.declareEnums(d.Specs)

	sto, explicit := specStorage(d.Specs)
	if sto == typedefStorage {
		return // a typedef declares no object; the parser's table has it
	}
	tls := u.declaresTLS(d.Specs)

	for _, id := range d.List {
		nameID := id.Decl.DeclName()
		if nameID == nil {
			continue
		}
		name := u.name(nameID)
		t := u.typeOf(id)
		t = u.completeArray(t, id.Init)

		if ft, ok := types.Unqualify(t).(*types.Func); ok {
			if u.top.objs[name] == nil {
				u.emitFuncSymbol(name, ft, fileLinkage(sto, explicit), id,
					u.definesFunc(name))
			}
			continue
		}

		v := u.vars[name]
		if v == nil {
			v = &fileVar{name: name, typ: t, decl: id, link: fileLinkage(sto, explicit), tls: tls}
			u.vars[name] = v
			u.varOrder = append(u.varOrder, v)
		}
		// A later declaration may complete an incomplete type: `int a[]; int a[3];`
		if _, ok := u.model.Sizeof(v.typ); !ok {
			v.typ = t
		}
		if a := u.declAlign(d.Specs, id.Attrs, id); a > v.align {
			v.align = a
		}
		switch {
		case id.Init != nil:
			v.init = id
		case sto == externStorage:
			v.ext = true
		default:
			v.tent = true
		}
	}
}

// inlineDefinition reports whether the body this unit emits for name was
// declared inline. It looks for the definition rather than consulting the
// declaration in hand, because a name can be declared in one place and defined
// in another, and it is the definition that decides.
//
// Unlike definition it never warns: by the time this is asked the unit has
// already decided to emit, and notEmitted has already had its say.
func (u *unit) inlineDefinition(name string) bool {
	for _, d := range u.file.Decls {
		f, ok := d.(*ast.FuncDecl)
		if ok && f.Body != nil && u.name(f.Name) == name {
			return specHas(f.Specs, token.INLINE)
		}
	}
	return false
}

// definesFunc reports whether this unit contains a body for name.
func (u *unit) definesFunc(name string) bool {
	return u.definition(name) != nil
}

// definition returns the FuncDecl that defines name in this unit, or nil.
func (u *unit) definition(name string) *ast.FuncDecl {
	for _, d := range u.file.Decls {
		f, ok := d.(*ast.FuncDecl)
		if ok && f.Body != nil && u.name(f.Name) == name {
			if u.notEmitted(name, f) {
				return nil
			}
			return f
		}
	}
	return nil
}

// definedType is the function type the definition of name has, where that
// says more than the declaration reached first.
//
// An identifier-list definition — `int f(a, b) int a; int b; {…}` — declares
// its parameters in a list of its own, which the analyzer resolves onto the
// definition's type. A prototype-less declaration seen earlier has no
// parameters at all, and building the signature from that one leaves the body
// with nothing to bind and every call to it an arity mismatch. §6.9.1p7 makes
// the definition's list what the function is compiled with, so it wins.
func (u *unit) definedType(name string, decl *types.Func) *types.Func {
	if decl.Proto || len(decl.Params) > 0 {
		return decl
	}
	def := u.definition(name)
	if def == nil {
		return decl
	}
	ft, ok := types.Unqualify(u.typeOf(def)).(*types.Func)
	if !ok {
		return decl
	}
	return ft
}

// emitTentative resolves every file-scope object to a module item.
//
// §6.9.2p2: a name with a tentative definition and no initializer anywhere in
// the unit is defined at the end of it, with zero initialization. vcc emits
// exactly that — a definition, not a common symbol. The traditional common
// spelling (ir.Global.Common) would let two units each write `int x;` and
// link; ISO says the second one is a second definition, and vcc is not a dialect host.
func (u *unit) emitTentative() {
	for _, v := range u.varOrder {
		o := &object{name: v.name, typ: v.typ, decl: v.decl, sto: externStorage, link: v.link}

		switch {
		case v.init == nil && !v.tent && v.ext:
			// An extern declaration with no definition here is a name, not a
			// reference; the import is created on first use. Every <stdio.h>
			// carries a handful of these — __stdinp, sys_errlist — and a unit
			// that touches none of them must not import them.
			o.pending, o.align, o.tls = true, v.align, v.tls
		default:
			dom := ir.RW
			if v.tls {
				dom = ir.TLS
			} else if types.QualsOf(v.typ)&types.QConst != 0 && !u.hasReloc(v) {
				dom = ir.RO
			}
			g := u.mod.Global(u.sym(v.name), dom, u.types.ftype(v.typ))
			if v.link == internalLinkage {
				g.Internal()
			} else {
				g.Export()
			}
			if v.align > 0 {
				g.Align(uint64(v.align))
			}
			if v.init == nil {
				g.Init(ir.ZeroInit)
			}
			v.g, o.sym = g, g
		}
		v.obj = o
		u.top.objs[v.name] = o
	}
}

// hasReloc reports whether a const object's initializer names an address, in
// which case it cannot sit in a read-only section that the loader will not
// relocate. Conservative: an unknown initializer is assumed to relocate.
func (u *unit) hasReloc(v *fileVar) bool {
	if v.init == nil || v.init.Init == nil {
		return false
	}
	found := false
	ast.Inspect(v.init.Init, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				found = true
			}
		case *ast.CompoundLit:
			// An array compound literal decays to its own address, so the
			// initializer holds a pointer even though nothing in it is
			// written with &. A struct one does not; the & in front of it
			// is caught above.
			if _, isArr := asArray(u.compoundLitType(n)); isArr {
				found = true
			}
		case *ast.Ident, *ast.StringLit:
			found = true
		}
		return !found
	})
	return found
}

// defineGen is pass two for a file-scope declaration: attach initializers.
//
// Initializers are attached here rather than in pass one because a static
// initializer may name a symbol declared below it — `int *p = &x; int x;` —
// and ir.RelocInit takes a Symbol, not a name to be resolved later.
func (u *unit) defineGen(d *ast.GenDecl) {
	for _, id := range d.List {
		nameID := id.Decl.DeclName()
		if nameID == nil || id.Init == nil {
			continue
		}
		v := u.vars[u.name(nameID)]
		if v == nil || v.g == nil || v.init != id {
			continue
		}
		v.g.Init(u.staticInit(v.typ, id.Init))
	}
}

// defineFunc emits one function body.
func (u *unit) defineFunc(d *ast.FuncDecl) {
	name := u.name(d.Name)
	o := u.top.objs[name]
	if o == nil {
		return
	}
	fn, ok := o.sym.(*ir.Func)
	if !ok {
		return // inline definition, or a name this unit only imports
	}
	ft, ok := types.Unqualify(o.typ).(*types.Func)
	if !ok {
		return
	}

	f := &fnState{
		decl: d, ft: ft, fn: fn, retTy: ft.Ret,
		labels: map[string]*labelBlock{},
		isMain: name == "main" && o.link == externalLinkage,
	}
	u.fn = f
	defer func() { u.fn = nil }()

	if specHas(d.Specs, token.NORETURN) {
		fn.NoReturn()
	}
	// Unwinding is opt-in per function: a body that can be crossed by an
	// unwind while an object with a cleanup is live declares a personality.
	// vcc emits none today, so every function it defines is nounwind.
	fn.NoUnwind()

	// The signature was built where the name was declared. Declaring it again
	// here would append a second copy of every parameter.
	sig := o.sig
	if sig == nil {
		sig = u.declareSig(fn, ft, d)
	}
	f.params, f.sretParam, f.hasSret = sig.params, sig.sretParam, sig.hasSret
	f.entry = fn.Entry()

	// The body goes in its own block so that the entry block stays open for
	// the whole walk: ptr.alloc is entry-block-only (§19.6), and an object
	// needing storage can turn up at any depth.
	body := fn.Block("body")
	f.cur, f.live = body, true

	u.push()
	u.bindParams(f, d, ft)
	u.bindFuncName(name)
	u.prescanLabels(d.Body)
	u.stmtList(d.Body)
	u.pop()
	u.finishFunc(f)

	// Closing the entry block is the last thing, for the same reason.
	f.entry.Br(body.To())

	u.pruneUnreachable(fn)
}

// pruneUnreachable drops the blocks nothing branches to.
//
// Unreachable code still has to be lowered — it can declare an object, and it
// can hold a label something jumps to — so it is emitted into a block nothing
// reaches (see blk). §19.2 admits only one such block, the entry, so the ones
// that are still unreached when the body closes are removed here. A goto that
// arrived later is why this cannot happen as the block is created.
//
// Values defined in a removed block are not used in a kept one: a use of one
// would have failed dominance in the source too, which the analyzer has
// already refused. verify.Module is what says whether that held.
func (u *unit) pruneUnreachable(fn *ir.Func) {
	live := make(map[*ir.Block]bool)
	for _, b := range fn.RPO() {
		live[b] = true
	}
	blocks := append([]*ir.Block(nil), fn.Blocks()...)
	for _, b := range blocks {
		if !live[b] {
			fn.RemoveBlock(b)
		}
	}
}

// declareParams freezes the signature. It runs before Entry(), because the
// entry block's existence freezes the parameter list.
func (u *unit) declareSig(fn *ir.Func, ft *types.Func, at ast.Node) *funcSig {
	tm := u.types
	ret := types.Unqualify(ft.Ret)
	f := &funcSig{}

	if ft.Variadic {
		fn.Variadic()
	}

	if r, ok := ret.(*types.Record); ok && !r.Vector {
		f.sretParam = fn.ParamPtr("__ret", ir.SRet(tm.record(r)))
		f.hasSret = true
	} else if !isVoid(ret) {
		rt, ok := tm.regType(ret)
		if !ok {
			if !u.unsupported128(at, ret) {
				u.errorf(at, "internal: return type %s has no register type", ret)
			}
			rt = ir.TypeI32
		}
		switch rt {
		case ir.TypeI1:
			fn.ReturnsI1(tm.extAttrs(ret)...)
		case ir.TypeI32:
			fn.ReturnsI32(tm.extAttrs(ret)...)
		case ir.TypeI64:
			fn.ReturnsI64(tm.extAttrs(ret)...)
		case ir.TypeF32:
			fn.ReturnsF32()
		case ir.TypeF64:
			fn.ReturnsF64()
		case ir.TypeF80:
			fn.ReturnsF80()
		case ir.TypeF128:
			fn.ReturnsF128()
		case ir.TypeV128:
			fn.ReturnsV128()
		case ir.TypePtr:
			fn.ReturnsPtr()
		}
	}

	for i, p := range ft.Params {
		pt := types.Unqualify(types.AdjustParam(p.Type))
		if isVoid(pt) {
			continue
		}
		if !ft.Proto {
			// §6.9.1p10: an identifier-list definition receives its
			// arguments already promoted — a char parameter arrives as an
			// int, a float as a double — because §6.5.2.2p6 is all a call to
			// an unprototyped function can do. The signature says what
			// arrives; bindParams converts it down to what was declared.
			pt = u.defaultPromote(pt)
		}
		nm := p.Name
		if nm == "" {
			nm = "arg" + itoa(i)
		}
		if named, ok := tm.indirect(pt); ok {
			f.params = append(f.params, fn.ParamPtr(nm, ir.ByVal(named)))
			continue
		}
		attrs := tm.extAttrs(pt)
		rt, _ := tm.regType(pt)
		var v ir.Value
		switch rt {
		case ir.TypeI32:
			v = fn.ParamI32(nm, attrs...)
		case ir.TypeI64:
			v = fn.ParamI64(nm, attrs...)
		case ir.TypeF32:
			v = fn.ParamF32(nm)
		case ir.TypeF64:
			v = fn.ParamF64(nm)
		case ir.TypeF80:
			v = fn.ParamF80(nm)
		case ir.TypeF128:
			v = fn.ParamF128(nm)
		case ir.TypeV128:
			v = fn.ParamV128(nm)
		default:
			v = fn.ParamPtr(nm)
		}
		f.params = append(f.params, v)
	}
	return f
}

// bindParams gives each parameter a home in the frame.
//
// A scalar parameter is copied out of its register into an alloca, because C
// makes it a modifiable lvalue whose address may be taken. An aggregate
// parameter is byval: the pointer already names storage the callee owns, so
// the pointer is the object's address and nothing is copied.
func (u *unit) bindParams(f *fnState, d *ast.FuncDecl, ft *types.Func) {
	names := u.paramNames(d, ft)
	if f.hasSret {
		f.sret = f.sretParam
	}
	fd := funcDeclarator(d.Decl)
	pi := 0
	for i, p := range ft.Params {
		pt := types.AdjustParam(p.Type)
		if isVoid(types.Unqualify(pt)) {
			continue
		}
		if fd != nil && i < len(fd.Params) {
			u.recordVLAExprs(p.Type, fd.Params[i])
		}
		if pi >= len(f.params) {
			break
		}
		v := f.params[pi]
		pi++
		name := p.Name
		if i < len(names) && names[i] != "" {
			name = names[i]
		}
		if name == "" {
			continue // an unnamed parameter is unreachable from the body
		}
		o := &object{name: name, typ: pt, decl: d, sto: autoStorage}
		if _, indirect := u.types.indirect(pt); indirect {
			// byval: the pointer already names storage the callee owns,
			// so it is the object's address and nothing is copied.
			o.addr = v.(ir.Ptr)
		} else {
			// The value arrives with the signature's type, which for an
			// identifier-list definition is the promoted one; the object has
			// the declared type. See declareSig.
			in := pt
			if !ft.Proto {
				in = u.defaultPromote(pt)
			}
			o.addr = u.alloca(pt, name+"_addr", d)
			u.store(value{v, in}, o.addr, pt, d)
		}
		u.bind(o)
	}
}

// paramNames recovers parameter names for a K&R definition, whose declarator
// carries an identifier list while the types came from the declaration list
// the analyzer matched against it.
// paramNames is the names the *definition* gives its parameters.
//
// The type is not the place to look. A prototype may leave every parameter
// unnamed — `static int twice(int);` — and the definition that follows names
// them; the type came from whichever declaration was reached first, so
// binding the body's objects from it leaves the parameters unreachable and
// every use of one an unbound name. §6.9.1p5 gives the definition's
// declarator the names, and this is that declarator.
func (u *unit) paramNames(d *ast.FuncDecl, ft *types.Func) []string {
	fd := funcDeclarator(d.Decl)
	if fd == nil {
		return nil
	}
	if !ft.Proto || len(fd.Params) == 0 {
		names := make([]string, len(fd.Idents))
		for i, id := range fd.Idents {
			names[i] = u.name(id)
		}
		return names
	}
	names := make([]string, len(fd.Params))
	for i, p := range fd.Params {
		if p.Decl == nil {
			continue
		}
		if id := p.Decl.DeclName(); id != nil {
			names[i] = u.name(id)
		}
	}
	return names
}

// funcDeclarator is the derivation that makes the declared name a function:
// the innermost one, nearest the name.
//
// Nearest is the whole point, because a declarator may derive a function
// twice. `void (*pick(int a))(void)` says pick takes an int and returns a
// pointer to a function taking none, and the parameters that belong to pick
// are the inner list — the one the name is inside. Reading the outer one
// bound the body's parameters from `(void)`, which names nothing, so every
// use of a parameter in the body was an unbound name. SQLite's
// sqlite3OsDlSym is that shape, and it needed a prototype before it to show:
// without one, the type carried the definition's own parameter names and
// bindParams never had to ask.
func funcDeclarator(d ast.Declarator) *ast.FuncDeclarator {
	var last *ast.FuncDeclarator
	for {
		switch x := d.(type) {
		case *ast.FuncDeclarator:
			last, d = x, x.Inner
		case *ast.PtrDeclarator:
			d = x.Inner
		case *ast.ParenDeclarator:
			d = x.Inner
		case *ast.ArrayDeclarator:
			d = x.Inner
		default:
			return last
		}
	}
}

// finishFunc terminates whatever block the body left open.
//
// §5.1.2.2.3 gives main an implicit `return 0`. Every other function that
// falls off its end with a non-void return type produced no value, which
// §6.9.1p12 makes undefined if the caller reads it — so it traps. VIR has no
// unreachable, and this is the case that most wants one.
func (u *unit) finishFunc(f *fnState) {
	if !f.live {
		return
	}
	switch {
	case f.isMain:
		f.cur.Return(f.cur.I32.Const(0))
	case isVoid(types.Unqualify(f.retTy)) || f.hasSret:
		f.cur.Return()
	default:
		f.cur.Trap()
	}
	f.live = false
}

// declareEnums binds every enumeration constant a specifier list introduces.
//
// Info.Enums has the values; what it does not have is a scope, and an
// enumerator is an ordinary identifier reachable from expression position for
// the rest of its scope. Inspect rather than a direct scan, because an enum
// may be declared inside a struct member list.
func (u *unit) declareEnums(specs ast.DeclSpecs) {
	for _, s := range specs {
		ast.Inspect(s, func(n ast.Node) bool {
			ed, ok := n.(*ast.EnumDecl)
			if !ok {
				return true
			}
			// The enumeration decides the type of its constants, so the
			// walk is over the declaration and not over the enumerators:
			// int, until one of them did not fit in an int. Enum.ConstType
			// is the rule, stated once and read here and in the analyzer.
			t := types.Type(types.Typ(types.Int))
			if en, ok := types.Unqualify(u.typeOf(ed)).(*types.Enum); ok {
				t = en.ConstType()
			}
			for _, e := range ed.List {
				v, ok := u.info.Enums[e]
				if !ok {
					u.errorf(e, "internal: no value recorded for enumerator")
					continue
				}
				u.bind(&object{
					name: u.name(e.Name), typ: t, decl: e,
					val: v, isEnum: true,
				})
			}
			return true
		})
	}
}

// declAlign is the alignment a declaration asks for, or 0 if it asks for
// none: the strictest of _Alignas and the three attribute spellings of the
// same idea.
//
// The type's own alignment is not here. This is what the *declaration*
// overrode it with, which is a different question and the one the object
// needs answered: `__attribute__((aligned(16))) short data[64]` is an array
// of shorts that has to start on a sixteen-byte boundary, and a compiler
// that gives it two produces code that faults the moment something reads it
// with an aligned vector load. Every SIMD library in C spells its buffers
// that way — stb_image's STBI_SIMD_ALIGN is exactly this — so the attribute
// is load-bearing rather than advisory.
func (u *unit) declAlign(specs ast.DeclSpecs, extra []*ast.Attr, at ast.Node) int64 {
	best := u.alignasOf(specs, at)
	best = u.attrAlign(extra, best)
	for _, s := range specs {
		if spec, ok := s.(*ast.AttrSpec); ok {
			best = u.attrAlign(spec.Attrs, best)
		}
	}
	return best
}

// attrAlign raises best to the strictest alignment attrs ask for.
func (u *unit) attrAlign(attrs []*ast.Attr, best int64) int64 {
	for _, a := range attrs {
		if a.Name == nil {
			continue
		}
		switch attrBaseName(u.name(a.Name)) {
		case "aligned", "align":
		default:
			continue
		}
		if len(a.Args) == 0 {
			// aligned with no argument asks for the biggest alignment the
			// target uses for any type, which is max_align_t's.
			if n := u.alignof(types.Typ(types.LongDouble), a); n > best {
				best = n
			}
			continue
		}
		if n, ok := u.constInt(a.Args[0]); ok && n > best {
			best = n
		}
	}
	return best
}

// attrBaseName strips gcc's doubled underscores: __aligned__ and aligned
// are the same attribute.
func attrBaseName(n string) string {
	if len(n) > 4 && strings.HasPrefix(n, "__") && strings.HasSuffix(n, "__") {
		return n[2 : len(n)-2]
	}
	return n
}

// alignasOf returns the strictest _Alignas on a declaration, or 0.
func (u *unit) alignasOf(specs ast.DeclSpecs, at ast.Node) int64 {
	var best int64
	for _, s := range specs {
		a, ok := s.(*ast.AlignasSpec)
		if !ok {
			continue
		}
		var n int64
		if a.Type != nil {
			n = u.alignof(u.typeOf(a.Type), a)
		} else if v, ok := u.constOf(a.X); ok {
			n = v
		}
		if n > best {
			best = n
		}
	}
	return best
}
