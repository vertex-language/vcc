package analyzer

import (
	"strings"

	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// The checker implements types.Resolver.

func (c *checker) Typedef(id *ast.Ident) types.Type {
	if s := c.lookup(c.name(id)); s != nil && s.kind == symTypedef {
		return s.typ
	}
	// The parser's table said typedef; an inconsistent resolution
	// here means an earlier declaration failed. Recover with int.
	return types.Typ(types.Int)
}

func (c *checker) Eval(e ast.Expr) (int64, bool) { return c.evalInt(e) }

// TypeOf implements types.Resolver for gcc's typeof.
//
// The expression's own type, not the converted one: typeof(arr) is the array
// and typeof(cp) keeps the const, which is the whole reason a macro reaches
// for it. §gcc leaves the operand unevaluated except for a variably modified
// type's size, which is the same rule sizeof follows.
func (c *checker) TypeOf(e ast.Expr) types.Type { return c.expr(e) }

func (c *checker) Report(n ast.Node, msg string) { c.report(n, msg) }

func (c *checker) Tag(spec ast.Expr) types.Type {
	if tn, ok := spec.(*ast.TypeName); ok {
		return c.typeName(tn)
	}
	switch spec := spec.(type) {
	case *ast.StructType:
		return c.structType(spec)
	case *ast.EnumDecl:
		return c.enumType(spec)
	case *ast.AtomicType:
		return types.Qualify(c.typeName(spec.Type), types.QAtomic)
	}
	return types.Typ(types.Int)
}

func (c *checker) typeName(tn *ast.TypeName) types.Type {
	if tn == nil {
		return types.Typ(types.Int)
	}
	sp := types.BuildSpecs(c.unit, tn.Specs, c)
	t, _ := types.BuildDeclarator(c.unit, sp.Type, tn.Decl, false, c)
	c.info.Types[tn] = t
	return t
}

// ---- tags ----

func (c *checker) structType(st *ast.StructType) types.Type {
	name := ""
	if st.Name != nil {
		name = c.name(st.Name)
	}

	var rec *types.Record
	if name != "" {
		if prev := c.lookupTag(name); prev != nil {
			r, ok := prev.typ.(*types.Record)
			switch {
			case !ok || r.Union != (st.Kind == token.UNION):
				c.report(st, "'"+name+"' declared as a different kind of tag")
			case st.Lbrace.IsValid() && r.Complete && c.currentTag(name) == prev:
				c.report(st, "redefinition of '"+prev.typ.String()+"'")
			case !st.Lbrace.IsValid() || c.currentTag(name) == prev:
				rec = r // refer to, or complete, the existing tag
			}
		}
	}
	if rec == nil {
		rec = &types.Record{Union: st.Kind == token.UNION, Name: name}
		if name != "" {
			c.declareTag(name, &tagsym{typ: rec, node: st})
		}
	}
	if !st.Lbrace.IsValid() {
		return rec
	}
	// The ceiling #pragma pack had in force where this definition begins.
	// It belongs to the definition and not to the tag: a record completed
	// inside pshpack2.h is two-byte packed wherever else its name appears.
	rec.Pack = c.packAt(st.Pos())
	c.applyRecordAttrs(rec, st.Attrs)

	for i, f := range st.Fields {
		fd, ok := f.(*ast.FieldDecl)
		if !ok {
			c.checkDecl(f, false) // StaticAssertDecl
			continue
		}
		sp := types.BuildSpecs(c.unit, fd.Specs, c)
		if len(fd.List) == 0 {
			if anonymousMember(fd.Specs, sp.Type) {
				rec.Fields = append(rec.Fields, types.Field{Type: sp.Type})
			} else {
				c.report(fd, "declaration declares nothing")
			}
			continue
		}
		for j, d := range fd.List {
			t, id := types.BuildDeclarator(c.unit, sp.Type, d.Decl, false, c)
			fld := types.Field{Type: t}
			if id != nil {
				fld.Name = c.name(id)
			}
			if d.Colon.IsValid() {
				fld.BitField = true
				fld.Width = c.bitFieldWidth(d, t)
			} else if !types.Complete(t) {
				isFAM := false
				if a, ok := t.(*types.Array); ok && a.Form == types.IncompleteArray {
					// §6.7.2.1p18 puts a flexible array member last, with at
					// least one member before it, because everything after
					// it would have no offset. A union has no after: every
					// member starts at the base, so one is admissible
					// anywhere in one — which is what winioctl.h writes,
					// two zero-length arrays as the arms of one union.
					isFAM = rec.Union ||
						i == len(st.Fields)-1 && j == len(fd.List)-1 && len(rec.Fields) > 0
				}
				if !isFAM {
					c.report(d, "member has incomplete type "+t.String())
				}
			}
			c.info.Types[d] = t
			rec.Fields = append(rec.Fields, fld)
		}
	}
	rec.Complete = true
	return rec
}

// anonymousMember reports whether a member declaration carrying no
// declarator is an anonymous member rather than a declaration of nothing.
//
// §6.7.2.1p13 admits one shape: a struct or union defined right there, with
// no tag and no member name, whose members belong to the record containing
// it. MSVC admits two more, and the Windows SDK writes both — a tagged
// definition, which is objidl.h's `struct _STGMEDIUM_UNION {…};`, and a
// typedef name, which is what winuser.h's `MOUSEHOOKSTRUCT DUMMYSTRUCTNAME;`
// becomes once the macro expands to nothing. Both promote their members the
// same way, and every lookup that walks into an anonymous member already
// walks into these.
//
// What stays a declaration of nothing is the bare tag: `struct S;` inside a
// record body declares the tag and no member, which the standard allows and
// which is not what either shape above looks like.
func anonymousMember(specs ast.DeclSpecs, t types.Type) bool {
	r, ok := types.Unqualify(t).(*types.Record)
	if !ok {
		return false
	}
	if r.Name == "" {
		return true
	}
	for _, s := range specs {
		switch s := s.(type) {
		case *ast.StructType:
			return s.Lbrace.IsValid()
		case *ast.TypedefType:
			return true
		}
	}
	return false
}

// applyRecordAttrs reads the attributes that decide a record's layout.
//
// packed and aligned are the two that mean something to the shape of the
// object, and dropping them is not a conservative choice: a packed struct is
// a wire format, and a compiler that pads it produces a program that reads
// the wrong bytes. Every other attribute is accepted and ignored, which is
// what it is worth here.
func (c *checker) applyRecordAttrs(rec *types.Record, attrs []*ast.Attr) {
	for _, a := range attrs {
		switch attrName(c, a) {
		case "intrin_type":
			// MSVC's mark on __m128i and its siblings: this record names a
			// vector register rather than a place in memory. It changes no
			// offset and no width — the members stay where they are — and
			// it is here rather than with packed and aligned because what
			// it decides is where a *value* of the type lives. See
			// types.Record.Vector and lower's regType.
			rec.Vector = true
		case "packed":
			rec.Packed = true
		case "aligned", "align":
			// Two spellings, one meaning: gcc writes
			// __attribute__((aligned(n))) and MSVC __declspec(align(n)).
			// The Windows SDK's <setjmp.h> writes the second.
			if len(a.Args) == 0 {
				// aligned with no argument asks for the biggest alignment
				// the target uses for any type, which is what max_align_t
				// names.
				if al, ok := c.model.Alignof(types.Typ(types.LongDouble)); ok && al > rec.Align {
					rec.Align = al
				}
				continue
			}
			v, ok := c.evalInt(a.Args[0])
			if !ok || v <= 0 || v&(v-1) != 0 {
				c.report(a, "aligned takes a positive power of two")
				continue
			}
			if v > rec.Align {
				rec.Align = v
			}
		}
	}
}

// attrName is an attribute's name with gcc's doubled underscores stripped.
// __packed__ and packed are the same attribute; the doubled spelling exists
// so a macro named packed cannot break a header.
func attrName(c *checker, a *ast.Attr) string {
	if a.Name == nil {
		return ""
	}
	n := c.name(a.Name)
	if len(n) > 4 && strings.HasPrefix(n, "__") && strings.HasSuffix(n, "__") {
		return n[2 : len(n)-2]
	}
	return n
}

// bitFieldWidth enforces §6.7.2.1p3–5: the declared type is one a
// bit-field may have; the width is a constant, not negative, at most
// the type's width, and nonzero when named.
//
// p5 names _Bool, signed int and unsigned int, and then admits "some
// other implementation-defined type". vcc's is any integer type, which
// is what every compiler whose headers vcc has to read assumes: char
// and short bit-fields are ordinary in system headers and in protocol
// structures, and rejecting them rejects the code, not a dialect. The
// layout rules in lower/layout.go are already stated per declared type
// rather than per int, so the storage unit follows from the type with
// nothing further to define.
func (c *checker) bitFieldWidth(d *ast.FieldDeclarator, t types.Type) int64 {
	if u := types.Unqualify(t); u.Kind() != types.Bool && !types.IsInteger(u) {
		c.report(d, "bit-field has type "+t.String()+"; a bit-field's type must be _Bool or an integer type")
	}
	w, ok := c.requireConst(d.Width, "bit-field width")
	if !ok {
		return 1
	}
	bits, _ := c.model.IntBits(t)
	switch {
	case w < 0:
		c.report(d.Width, "bit-field width is negative")
		w = 1
	case w > bits:
		c.report(d.Width, "bit-field width exceeds the width of its type")
		w = bits
	case w == 0 && d.Decl != nil:
		c.report(d.Width, "named bit-field must have nonzero width")
		w = 1
	}
	return w
}

func (c *checker) enumType(ed *ast.EnumDecl) types.Type {
	name := ""
	if ed.Name != nil {
		name = c.name(ed.Name)
	}

	var en *types.Enum
	if name != "" {
		if prev := c.lookupTag(name); prev != nil {
			e, ok := prev.typ.(*types.Enum)
			switch {
			case !ok:
				c.report(ed, "'"+name+"' declared as a different kind of tag")
			case ed.Lbrace.IsValid() && e.Complete && c.currentTag(name) == prev:
				c.report(ed, "redefinition of 'enum "+name+"'")
			default:
				en = e
			}
		}
	}
	if en == nil {
		en = &types.Enum{Name: name}
		if name != "" {
			c.declareTag(name, &tagsym{typ: en, node: ed})
		}
	}
	// Recorded here rather than at the end, so that a mention with no body
	// — `enum E x;` — has one too: lower reads it for every EnumDecl it
	// walks, to give the enumerators the type this gave them.
	c.info.Types[ed] = en

	if !ed.Lbrace.IsValid() {
		if !en.Complete {
			// §6.7.2.3p3: an enum without a body must name a
			// completed enumeration.
			c.report(ed, "enum '"+name+"' is incomplete here")
		}
		return en
	}

	next := int64(0)
	values := make([]int64, 0, len(ed.List))
	syms := make([]*symbol, 0, len(ed.List))
	for _, e := range ed.List {
		if e.Value != nil {
			if v, ok := c.requireConst(e.Value, "enumerator value"); ok {
				next = v
			}
		}
		values = append(values, next)
		c.info.Enums[e] = next
		// Declared as the list is walked, because an enumerator is in
		// scope for the ones after it: `A, B = A + 1` is the whole reason
		// the value of one can be written in terms of another.
		s := &symbol{kind: symEnumConst, typ: types.Typ(types.Int), node: e, value: next}
		syms = append(syms, s)
		c.declare(e.Name, s)
		next++
	}

	// Which type holds them is a fact about the whole list, so it is
	// patched back onto the enumerators rather than guessed one at a time.
	// It matters only where the list widened: 0x80000000 declared as an int
	// is negative, and every comparison against it reads the wrong way.
	en.Under = c.enumUnder(values)
	for _, s := range syms {
		s.typ = en.ConstType()
	}
	en.Complete = true
	return en
}

// enumUnder is the integer type an enumeration's values fit in, as the Kind
// to record in Enum.Under — Invalid where they all fit in int, which is
// every enumeration §6.7.2.2p2 allows.
//
// Beyond that paragraph vcc follows gcc, clang and MSVC, which all widen
// rather than refuse: the narrowest of unsigned int, long, unsigned long
// and long long that holds every value, in that order, so a list of
// non-negative values that overflows int becomes unsigned rather than
// jumping to 64 bits. There is no wider answer to reach for — a value is
// held here as an int64, so long long always fits.
//
// The alternative is refusing, and refusing means <windows.h> does not
// open: wingdi.h alone writes thirty-four enumerators past INT_MAX.
func (c *checker) enumUnder(values []int64) types.Kind {
	fits := func(k types.Kind) bool {
		t := types.Typ(k)
		hi := int64(c.model.IntMax(t))
		lo := int64(0)
		if types.IsSigned(t) {
			lo = -hi - 1
		}
		for _, v := range values {
			if v < lo || v > hi {
				return false
			}
		}
		return true
	}
	if fits(types.Int) {
		return types.Invalid
	}
	for _, k := range []types.Kind{types.UInt, types.Long, types.ULong, types.LongLong} {
		if fits(k) {
			return k
		}
	}
	return types.LongLong
}

// ---- declarations ----

func (c *checker) checkDecl(d ast.Decl, external bool) {
	switch d := d.(type) {
	case *ast.GenDecl:
		c.checkGenDecl(d, external)
	case *ast.FuncDecl:
		c.checkFuncDecl(d)
	case *ast.StaticAssertDecl:
		c.checkStaticAssert(d)
	case *ast.AsmDecl:
		// File-scope assembly declares nothing this package can check. Its
		// text is not read here, and it names no object, so there is no
		// type, no linkage and no redeclaration to reconcile.
	case *ast.EmptyDecl, *ast.BadDecl:
		// Reported by the parser.
	}
}

func (c *checker) checkGenDecl(d *ast.GenDecl, external bool) {
	sp := types.BuildSpecs(c.unit, d.Specs, c)
	c.checkStorage(d, sp, external)

	if len(d.List) == 0 {
		// struct S {…};  enum E {…}; — the specifier was the point.
		// A bare basic type declares nothing.
		if _, ok := types.Unqualify(sp.Type).(*types.Basic); ok {
			c.report(d, "declaration declares nothing")
		}
		return
	}

	var autoT types.Type // the type __auto_type deduced, for the second declarator on

	for _, id := range d.List {
		base := sp.Type
		if sp.Auto {
			base = c.autoType(id, &autoT)
		}
		t, name := types.BuildDeclarator(c.unit, base, id.Decl, false, c)
		t = c.completeArray(t, id.Init)
		c.info.Types[id] = t
		if name == nil {
			c.decodeAll(id.Init)
			continue
		}
		under := types.Unqualify(t)

		if c.hasVLA(t) && (external || sp.Storage == token.STATIC || sp.Storage == token.EXTERN) {
			c.report(id, "variably modified type requires block scope and automatic storage")
		}

		sym := &symbol{typ: t, node: id,
			extern: external || sp.Storage == token.EXTERN}
		switch {
		case sp.Storage == token.TYPEDEF:
			sym.kind = symTypedef
			if id.Init != nil {
				c.report(id, "typedef declares no object; it cannot be initialized")
			}
		case under.Kind() == types.FuncKind:
			sym.kind = symFunc
			if id.Init != nil {
				c.report(id, "function declared like a variable cannot be initialized")
			}
		default:
			sym.kind = symObject
			if sp.Inline || sp.Noreturn {
				c.report(id, "function specifiers apply only to functions")
			}
			// Object completeness: extern declarations and
			// initialized incomplete arrays get a pass.
			if !types.Complete(t) {
				arr, isArr := under.(*types.Array)
				deferred := sp.Storage == token.EXTERN ||
					(isArr && arr.Form == types.IncompleteArray && id.Init != nil)
				if !deferred {
					c.report(id, "'"+c.name(name)+"' has incomplete type "+t.String())
				}
			}
		}
		c.declare(name, sym)
		if sp.Auto {
			// autoType already walked this initializer, and walking it again
			// would report everything in it twice. There is also nothing to
			// check: the declared type was taken from this expression, so
			// the assignment it would test holds by construction. It is also
			// the one initializer analyzed before its own name is visible,
			// which is what makes __auto_type x = f(&x) undeclared rather
			// than circular.
			continue
		}
		// §6.2.1p7: an identifier's scope begins just after its declarator,
		// so the initializer is analyzed with the name already visible. That
		// is what makes `struct node *n = malloc(sizeof *n);` legal, and
		// analyzing the initializer first reports n as undeclared in its own
		// declaration.
		init := c.expr(id.Init)
		// §6.7.9p11: a *scalar*'s initializer obeys the same constraints as a
		// simple assignment. An aggregate's does not — it is a braced list,
		// or, for a character array, a string literal (§6.7.9p14), and
		// neither is an assignment. Their shape is checked elsewhere.
		if id.Init != nil && sym.kind == symObject && types.IsScalar(t) {
			if _, braced := id.Init.(*ast.InitList); !braced {
				c.checkAssign(id.Init, t, init, "initializing")
			}
		}
	}
}

func (c *checker) checkStorage(d ast.Node, sp types.Spec, external bool) {
	if external && (sp.Storage == token.AUTO || sp.Storage == token.REGISTER) {
		c.report(d, "file-scope declaration cannot be auto or register")
	}
	for _, a := range sp.Aligns {
		if a.X != nil {
			if v, ok := c.requireConst(a.X, "_Alignas argument"); ok {
				if v != 0 && (v&(v-1)) != 0 {
					c.report(a, "_Alignas argument must be a power of two")
				}
			}
		}
	}
}

func (c *checker) hasVLA(t types.Type) bool {
	switch t := types.Unqualify(t).(type) {
	case *types.Array:
		return t.Form == types.VLA || c.hasVLA(t.Elem)
	case *types.Pointer:
		return c.hasVLA(t.Elem)
	}
	return false
}

// autoType deduces what __auto_type stands for in one declarator: the
// initializer's type after the conversions of §6.3.2.1 and with the
// top-level qualifiers dropped, which is gcc's definition and is why it is
// not a spelling of typeof — `__auto_type x = arr` is a pointer where
// `typeof(arr) x` is an array, and `__auto_type y = ci` is not const.
//
// The specifiers are shared by every declarator in the declaration but the
// initializers are not, so each is deduced from its own and they are
// required to agree; gcc and clang both reject a declaration that deduces
// two types. first carries the one already deduced, or nil.
//
// A declarator with no initializer has nothing to deduce from. That is the
// one place this can fail, and it is reported rather than guessed at.
func (c *checker) autoType(id *ast.InitDeclarator, first *types.Type) types.Type {
	if id.Init == nil {
		c.report(id, "__auto_type requires an initializer")
		return types.Typ(types.Int)
	}
	if _, braced := id.Init.(*ast.InitList); braced {
		// A braced list has no type of its own to deduce from.
		c.report(id, "__auto_type cannot be deduced from a braced initializer")
		return types.Typ(types.Int)
	}
	if !plainNameDeclarator(id.Decl) {
		// `__auto_type *p = &v` would have to solve the declarator's
		// derivation against the initializer's type — p is int*, not int**,
		// so what is deduced is not the initializer's type but the one that
		// yields it after the derivation. vcc does not do that, and
		// deducing the initializer's type here would silently declare p one
		// pointer too deep.
		c.report(id, "__auto_type deduces the type of a plain identifier; "+
			"this declarator derives from it")
		return types.Typ(types.Int)
	}
	t := c.expr(id.Init)
	if t == nil {
		return types.Typ(types.Int)
	}
	t = types.Unqualify(types.Decay(t))
	if *first == nil {
		*first = t
		return t
	}
	if !types.Compatible(*first, t) {
		c.report(id, "__auto_type deduces "+(*first).String()+
			" earlier in this declaration and "+t.String()+" here")
		return *first
	}
	return *first
}

// plainNameDeclarator reports whether d is just an identifier, parentheses
// aside — the only shape __auto_type deduces for.
func plainNameDeclarator(d ast.Declarator) bool {
	for {
		switch x := d.(type) {
		case *ast.NameDeclarator:
			return true
		case *ast.ParenDeclarator:
			d = x.Inner
		default:
			return false
		}
	}
}

func (c *checker) checkStaticAssert(d *ast.StaticAssertDecl) {
	v, ok := c.requireConst(d.Cond, "_Static_assert condition")
	if ok && v == 0 {
		msg := "static assertion failed"
		if d.Msg != nil {
			sv := DecodeString(c.unit, d.Msg, c.model, func(string) {})
			b := make([]byte, 0, len(sv.Data))
			for _, u := range sv.Data[:len(sv.Data)-1] {
				b = append(b, byte(u))
			}
			msg += ": " + string(b)
		}
		c.report(d, msg)
	}
}

// ---- functions ----

func (c *checker) checkFuncDecl(fn *ast.FuncDecl) {
	sp := types.BuildSpecs(c.unit, fn.Specs, c)
	t, name := types.BuildDeclarator(c.unit, sp.Type, fn.Decl, false, c)
	c.info.Types[fn] = t

	ft, ok := types.Unqualify(t).(*types.Func)
	if !ok {
		c.report(fn, "function definition requires a function declarator")
		return
	}
	if name != nil {
		c.declare(name, &symbol{kind: symFunc, typ: t, node: fn, extern: true})
	}

	c.push()
	c.declareFnParams(fn, ft)
	c.declareFuncName(fn, name)

	c.labels = map[string]*ast.LabeledStmt{}
	c.gotos = nil
	prevRet := c.fnRet
	c.fnRet = ft.Ret
	c.checkStmt(fn.Body, false) // scope already pushed
	c.fnRet = prevRet
	for _, g := range c.gotos {
		if g.Label != nil && c.labels[c.name(g.Label)] == nil {
			c.report(g, "goto to undefined label '"+c.name(g.Label)+"'")
		}
	}
	for _, l := range c.labelRefs {
		if c.labels[c.name(l)] == nil {
			c.report(l, "'&&"+c.name(l)+"' names no label in this function")
		}
	}
	for _, l := range c.asmLabels {
		if c.labels[c.name(l)] == nil {
			c.report(l, "'asm goto' names no label '"+c.name(l)+"' in this function")
		}
	}
	c.labelRefs, c.asmLabels = nil, nil
	c.pop()
}

// declareFuncName declares §6.4.2.2's predefined identifier.
//
// "The identifier __func__ shall be implicitly declared by the translator as
// if, immediately following the opening brace of each function definition,
// the declaration `static const char __func__[] = "function-name";` appeared."
// It is a declaration, not a macro, which is why it belongs here and not in
// phase 4.
//
// __FUNCTION__ and __PRETTY_FUNCTION__ are gcc's spellings of the same thing.
// For C they name the same string; the distinction __PRETTY_FUNCTION__ draws
// is a C++ one.
func (c *checker) declareFuncName(fn *ast.FuncDecl, name *ast.Ident) {
	if name == nil {
		return
	}
	text := c.name(name)
	t := types.Qualify(&types.Array{
		Elem: types.Typ(types.Char),
		Form: types.FixedArray,
		Len:  int64(len(text)) + 1,
	}, types.QConst)
	for _, spelling := range [...]string{"__func__", "__FUNCTION__", "__PRETTY_FUNCTION__"} {
		c.scopes[len(c.scopes)-1].ordinary[spelling] =
			&symbol{kind: symObject, typ: t, node: fn}
	}
}

// declareFnParams brings the definition's parameters into the body
// scope: prototype parameters directly, K&R parameters through the
// identifier list matched against the declaration list — the check
// the parser deliberately deferred.
func (c *checker) declareFnParams(fn *ast.FuncDecl, ft *types.Func) {
	fd := outermostFunc(fn.Decl)
	if fd == nil {
		return
	}

	if len(fd.Idents) == 0 && len(fn.KR) == 0 {
		// Prototype (or empty) parameter list.
		for _, p := range fd.Params {
			if id := paramIdent(p); id != nil {
				var t types.Type = types.Typ(types.Int)
				for _, fp := range ft.Params {
					if fp.Name == c.name(id) {
						t = fp.Type
					}
				}
				c.declare(id, &symbol{kind: symObject, typ: t, node: p})
			}
		}
		return
	}

	// K&R: every declaration must name a parameter; undeclared
	// parameters default to int (§6.9.1p7).
	named := map[string]*ast.Ident{}
	for _, id := range fd.Idents {
		named[c.name(id)] = id
	}
	declared := map[string]types.Type{}
	for _, kd := range fn.KR {
		ksp := types.BuildSpecs(c.unit, kd.Specs, c)
		if ksp.Storage != 0 && ksp.Storage != token.REGISTER {
			c.report(kd, "K&R parameter declarations take only register")
		}
		for _, kid := range kd.List {
			t, id := types.BuildDeclarator(c.unit, ksp.Type, kid.Decl, true, c)
			if id == nil {
				continue
			}
			n := c.name(id)
			if named[n] == nil {
				c.report(id, "'"+n+"' declared but not in the parameter list")
				continue
			}
			declared[n] = types.AdjustParam(t)
		}
	}
	params := make([]types.Param, 0, len(fd.Idents))
	for _, id := range fd.Idents {
		t := declared[c.name(id)]
		if t == nil {
			t = types.Typ(types.Int)
		}
		c.declare(id, &symbol{kind: symObject, typ: t, node: id})
		params = append(params, types.Param{Name: c.name(id), Type: t})
	}
	// The identifier-list declarator says nothing about the parameters, so
	// the type BuildDeclarator produced has none. The definition's own
	// declaration list does say, and the parameters it names are what the
	// function is compiled with — so the resolved list is recorded on the
	// type. Proto stays false: §6.7.6.3p14 keeps a call to this function
	// unchecked against the list, which is the whole difference between the
	// two forms.
	ft.Params = params
}

func outermostFunc(d ast.Declarator) *ast.FuncDeclarator {
	// The function derivation of the definition is the FuncDeclarator
	// whose Inner carries the name — the last one applied.
	var found *ast.FuncDeclarator
	for {
		switch dd := d.(type) {
		case *ast.FuncDeclarator:
			found, d = dd, dd.Inner
		case *ast.PtrDeclarator:
			d = dd.Inner
		case *ast.ParenDeclarator:
			d = dd.Inner
		case *ast.ArrayDeclarator:
			d = dd.Inner
		default:
			return found
		}
	}
}

func paramIdent(p *ast.ParamDecl) *ast.Ident {
	if p.Decl == nil {
		return nil
	}
	return p.Decl.DeclName()
}

// ---- statements: label discipline and scope structure ----

// checkReturn is §6.8.6.4: a return with a value in a void function and one
// without in a function that returns something are both constraint
// violations, and a value returned obeys simple assignment's constraints.
func (c *checker) checkReturn(s *ast.ReturnStmt, got types.Type) {
	if c.fnRet == nil {
		return // outside a function, or the return type is not known
	}
	if types.IsVoid(c.fnRet) {
		if s.Result != nil {
			c.report(s, "returning a value from a function whose return type is void")
		}
		return
	}
	if s.Result == nil {
		c.report(s, "returning nothing from a function that returns "+c.fnRet.String())
		return
	}
	c.checkAssign(s.Result, c.fnRet, got, "returning")
}

func (c *checker) checkStmt(s ast.Stmt, pushScope bool) {
	switch s := s.(type) {
	case *ast.CompoundStmt:
		if pushScope {
			c.push()
			defer c.pop()
		}
		for _, item := range s.Items {
			c.checkStmt(item, true)
		}

	case *ast.DeclStmt:
		c.checkDecl(s.D, false)

	case *ast.LabeledStmt:
		n := c.name(s.Label)
		if prev := c.labels[n]; prev != nil {
			c.report(s.Label, "duplicate label '"+n+"'")
		} else {
			c.labels[n] = s
		}
		c.checkStmt(s.Stmt, true)

	case *ast.CaseStmt:
		if c.switchD == 0 {
			if s.Kind == token.CASE {
				c.report(s, "'case' outside a switch statement")
			} else {
				c.report(s, "'default' outside a switch statement")
			}
		} else if s.Value != nil {
			c.requireConst(s.Value, "case label")
			if s.High != nil {
				c.requireConst(s.High, "the upper bound of a case range")
			}
		}
		c.checkStmt(s.Stmt, true)

	case *ast.SwitchStmt:
		c.decodeAll(s.Cond)
		c.switchD++
		c.checkStmt(s.Body, true)
		c.switchD--

	case *ast.WhileStmt:
		c.decodeAll(s.Cond)
		c.loopD++
		c.checkStmt(s.Body, true)
		c.loopD--

	case *ast.DoStmt:
		c.loopD++
		c.checkStmt(s.Body, true)
		c.loopD--
		c.decodeAll(s.Cond)

	case *ast.ForStmt:
		c.push()
		if d, ok := s.Init.(*ast.GenDecl); ok {
			c.checkDecl(d, false)
		} else if e, ok := s.Init.(ast.Expr); ok {
			c.decodeAll(e)
		}
		c.decodeAll(s.Cond)
		c.decodeAll(s.Post)
		c.loopD++
		c.checkStmt(s.Body, true)
		c.loopD--
		c.pop()

	case *ast.IfStmt:
		c.decodeAll(s.Cond)
		c.checkStmt(s.Then, true)
		if s.Else != nil {
			c.checkStmt(s.Else, true)
		}

	case *ast.GotoStmt:
		if s.Target != nil {
			c.requireScalar(s, c.rvalue(s.Target), "the operand of a computed goto")
			return
		}
		c.gotos = append(c.gotos, s)

	case *ast.BreakStmt:
		if c.loopD == 0 && c.switchD == 0 {
			c.report(s, "'break' outside a loop or switch")
		}

	case *ast.ContinueStmt:
		if c.loopD == 0 {
			c.report(s, "'continue' outside a loop")
		}

	case *ast.SEHTryStmt:
		c.checkStmt(s.Body, true)
		if s.Filter != nil {
			c.requireScalar(s, c.rvalue(s.Filter), "the filter of an __except block")
		}
		c.checkStmt(s.Handler, true)

	case *ast.SEHLeaveStmt:
		// Allowed inside __try blocks.

	case *ast.AsmStmt:
		c.checkAsm(s)

	case *ast.ExprStmt:
		c.decodeAll(s.X)

	case *ast.ReturnStmt:
		got := c.expr(s.Result)
		if s.Result != nil {
			got = types.Decay(got)
		}
		c.checkReturn(s, got)
	}
}

// decodeAll runs phases 5–6 over every literal in an expression —
// the value-level diagnostics (constant too large, escape out of
// range, bad UCN code points, mixed string prefixes) fire here,
// exactly once per literal, whether or not the expression is
// otherwise analyzed yet.
func (c *checker) decodeAll(e ast.Expr) { c.expr(e) }

// reportOnce funnels a decoder's reports so one bad literal yields
// one diagnostic, never a cascade.
func (c *checker) reportOnce(n ast.Node, f func(report func(string))) {
	done := false
	f(func(msg string) {
		if !done {
			done = true
			c.report(n, msg)
		}
	})
}

// completeArray gives an incomplete array the length its initializer supplies
// (§6.7.9p22): `int a[] = {1,2,3}` declares an int[3], and every use of a
// after that declarator sees one.
//
// The length is decided here, and not further down, because it is a fact
// about the type and the type is what the phases above read. sizeof(a) is an
// integer constant expression the moment the declaration is complete, so
// `int b[sizeof a / sizeof a[0]]` is an ordinary array — while a compiler
// that left the type incomplete makes it a VLA, with a VLA's storage
// lifetime and a VLA's diagnostics, for a bound the standard calls constant.
// SQLite writes exactly that over a block-scope static.
func (c *checker) completeArray(t types.Type, init ast.Expr) types.Type {
	arr, ok := types.Unqualify(t).(*types.Array)
	if !ok || arr.Form != types.IncompleteArray || init == nil {
		return t
	}
	n, ok := c.inferArrayLen(init)
	if !ok {
		return t
	}
	return types.Qualify(&types.Array{Elem: arr.Elem, Form: types.FixedArray, Len: n},
		types.QualsOf(t))
}

// inferArrayLen is the count §6.7.9p22 arrives at: one past the largest index
// the initializer reaches, counting a designator's index and then continuing
// from it, or for a string literal the characters plus the terminator
// (§6.7.9p14).
//
// It reports false where the count is not knowable — an index that is not a
// constant, a member designator on something that is not a record — and the
// array then stays incomplete, which is the diagnosis it already had.
func (c *checker) inferArrayLen(init ast.Expr) (int64, bool) {
	list, ok := init.(*ast.InitList)
	if !ok {
		at, ok := types.Unqualify(c.quietType(init)).(*types.Array)
		if !ok || at.Form != types.FixedArray {
			return 0, false
		}
		return at.Len, true
	}
	maxIdx, idx := int64(-1), int64(0)
	for _, it := range list.Items {
		if len(it.Designators) > 0 {
			d, ok := it.Designators[0].(*ast.IndexDesignator)
			if !ok {
				return 0, false
			}
			v, ok := c.evalInt(d.Index)
			if !ok {
				return 0, false
			}
			idx = v
		}
		if idx > maxIdx {
			maxIdx = idx
		}
		idx++
	}
	return maxIdx + 1, true
}
