package types

import (
	"sort"
	"strings"

	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
)

// Resolver is what construction needs from its caller: name and tag
// resolution, constant evaluation, and a place to send diagnostics.
// The analyzer implements it; a tool with simpler needs can too.
type Resolver interface {
	// Typedef returns the type a typedef name denotes. The parser
	// guaranteed the name is in its table; the resolver owns what it
	// means.
	Typedef(id *ast.Ident) Type
	// Tag resolves a StructType, EnumDecl, or AtomicType specifier
	// node to a type, declaring tags as needed.
	Tag(spec ast.Expr) Type
	// Eval evaluates an integer constant expression. ok is false
	// when the expression isn't constant — which is not an error
	// here: a non-constant array length is a VLA.
	Eval(e ast.Expr) (int64, bool)
	// Report sends one diagnostic sited at n.
	Report(n ast.Node, msg string)
	// TypeOf returns the type of an expression, for gcc's typeof. It is
	// the expression's own type — an array stays an array and the
	// qualifiers stay on — because that is what typeof means.
	TypeOf(e ast.Expr) Type
}

// Spec is what a specifier list denotes: a (qualified) type plus the
// non-type facts riding along with it.
type Spec struct {
	// Attrs are the __attribute__ entries written among the specifiers, in
	// order. Only the ones that change layout mean anything; the rest are
	// carried so a caller that grows an interest in one can find it.
	Attrs       []*ast.Attr
	Type        Type
	Storage     token.Kind // TYPEDEF/EXTERN/STATIC/AUTO/REGISTER, or 0
	ThreadLocal bool
	Inline      bool
	Noreturn    bool
	Aligns      []*ast.AlignasSpec // kept as nodes; checked by the analyzer

	// Auto is gcc's __auto_type: the declaration's type is its
	// initializer's, so it cannot be resolved from the specifier list this
	// Spec was built from. Type is int here, as a placeholder that keeps
	// every consumer working on a real type; the deduction happens in the
	// analyzer, which is the first place that has both the specifiers and
	// each declarator's initializer. A caller that does not look at this
	// field gets int, which is wrong but not unsound — hence the analyzer
	// reporting any position where deduction cannot happen.
	Auto bool
}

// BuildSpecs folds a written-order specifier list into a Spec,
// enforcing §6.7.1's one-storage-class rule and §6.7.2's type
// specifier multiset table. On an invalid multiset it reports once
// and yields int, so construction always produces a type.
func BuildSpecs(unit *token.File, specs ast.DeclSpecs, r Resolver) Spec {
	var sp Spec
	var quals Qual
	var kws []token.Kind
	var other Type // struct/union/enum/typedef/_Atomic() specifier
	var otherNode ast.Node
	reported := false

	for _, s := range specs {
		switch s := s.(type) {
		case *ast.KeywordSpec:
			switch s.Kind {
			case token.TYPEDEF, token.EXTERN, token.STATIC, token.AUTO, token.REGISTER:
				if sp.Storage != 0 && !reported {
					r.Report(s, "multiple storage classes in declaration specifiers")
					reported = true
				}
				sp.Storage = s.Kind
			case token.THREAD_LOCAL:
				sp.ThreadLocal = true
			case token.INLINE:
				sp.Inline = true
			case token.NORETURN:
				sp.Noreturn = true
			case token.CONST:
				quals |= QConst
			case token.VOLATILE:
				quals |= QVolatile
			case token.RESTRICT:
				quals |= QRestrict
			case token.ATOMIC:
				quals |= QAtomic
			case token.AUTO_TYPE:
				// Not a member of §6.7.2p2's multiset table: it names no
				// type of its own, so it is recorded and kept out of kws.
				sp.Auto = true
			default: // a basic type keyword
				kws = append(kws, s.Kind)
			}
		case *ast.AlignasSpec:
			sp.Aligns = append(sp.Aligns, s)
		case *ast.StructType, *ast.EnumDecl, *ast.AtomicType:
			if other != nil {
				r.Report(s, "two or more data types in declaration specifiers")
			}
			other, otherNode = r.Tag(s), s
		case *ast.TypedefType:
			if other != nil {
				r.Report(s, "two or more data types in declaration specifiers")
			}
			other, otherNode = r.Typedef(s.Name), s
		case *ast.TypeofType:
			if other != nil {
				r.Report(s, "two or more data types in declaration specifiers")
			}
			var t Type
			if s.Type != nil {
				t = r.Tag(s.Type)
			} else {
				t = r.TypeOf(s.X)
			}
			if t == nil {
				t = Typ(Int)
			}
			other, otherNode = t, s
		case *ast.AttrSpec:
			// Attributes among the specifiers: the ones that mean something
			// are read where the thing they describe is built — a record's
			// in the analyzer, an object's below — and the rest carry no
			// type information at all.
			for _, a := range s.Attrs {
				sp.Attrs = append(sp.Attrs, a)
			}
		}
	}

	// _Thread_local combines only with static and extern (§6.7.1p3).
	if sp.ThreadLocal && sp.Storage != 0 && sp.Storage != token.STATIC && sp.Storage != token.EXTERN {
		r.Report(anchor(specs), "_Thread_local may combine only with static or extern")
	}

	var base Type
	switch {
	case sp.Auto:
		// __auto_type names the initializer's type, so there is nothing to
		// build here and nothing else may be written beside it. int stands
		// in until the analyzer deduces the real one.
		if other != nil || len(kws) > 0 {
			r.Report(anchor(specs), "__auto_type must be the only type specifier")
		}
		base = Typ(Int)
	case other != nil && len(kws) > 0:
		r.Report(otherNode, "two or more data types in declaration specifiers")
		base = other
	case other != nil:
		base = other
	case len(kws) > 0:
		k, ok := multiset(kws)
		if !ok {
			r.Report(anchor(specs), "invalid type specifier combination '"+kwString(kws)+"'")
			k = Int
		}
		if IsComplex(Typ(k)) {
			// §6.10.8.3 lets an implementation that defines
			// __STDC_NO_COMPLEX__ omit the complex types, and vcc defines it.
			// Diagnosing here is what makes that claim true: without it the
			// type builds, reaches lower, and fails there with an internal
			// error about a register type, which tells the user nothing they
			// can act on.
			r.Report(anchor(specs), "vcc does not implement "+Typ(k).String()+
				"; it defines __STDC_NO_COMPLEX__, which a program may test for")
			k = Double
		}
		base = Typ(k)
	default:
		// C11 requires a type specifier (§6.7.2p2): implicit int is gone.
		r.Report(anchor(specs), "type specifier missing; C11 requires one")
		base = Typ(Int)
	}

	// §6.7.3p2–3: restrict only qualifies pointers; _Atomic must not
	// qualify an array or function type (reachable via typedef).
	if quals&QRestrict != 0 && Unqualify(base).Kind() != PointerKind {
		r.Report(anchor(specs), "restrict requires a pointer type")
		quals &^= QRestrict
	}
	if quals&QAtomic != 0 {
		switch Unqualify(base).Kind() {
		case ArrayKind, FuncKind:
			r.Report(anchor(specs), "_Atomic must not qualify an array or function type")
			quals &^= QAtomic
		}
	}

	sp.Type = Qualify(base, quals)
	return sp
}

func anchor(specs ast.DeclSpecs) ast.Node {
	if len(specs) > 0 {
		return specs[0]
	}
	return nil
}

// multiset canonicalizes a basic-keyword multiset and looks it up in
// §6.7.2p2's table.
func multiset(kws []token.Kind) (Kind, bool) {
	k, ok := multisets[kwString(kws)]
	return k, ok
}

func kwString(kws []token.Kind) string {
	names := make([]string, len(kws))
	for i, k := range kws {
		names[i] = k.String()
	}
	sort.Slice(names, func(i, j int) bool { return kwOrder[names[i]] < kwOrder[names[j]] })
	return strings.Join(names, " ")
}

var kwOrder = map[string]int{
	"unsigned": 0, "signed": 1, "long": 2, "short": 3, "char": 4,
	"int": 5, "float": 6, "double": 7, "void": 8, "_Bool": 9, "_Complex": 10,
	"__int128": 11,
	"__int64":  12, "__int32": 13, "__int16": 14, "__int8": 15,
}

var multisets = map[string]Kind{
	"void": Void, "_Bool": Bool,
	"char": Char, "signed char": SChar, "unsigned char": UChar,

	"short": Short, "signed short": Short, "short int": Short, "signed short int": Short,
	"unsigned short": UShort, "unsigned short int": UShort,

	"int": Int, "signed": Int, "signed int": Int,
	"unsigned": UInt, "unsigned int": UInt,

	"long": Long, "signed long": Long, "long int": Long, "signed long int": Long,
	"unsigned long": ULong, "unsigned long int": ULong,

	"long long": LongLong, "signed long long": LongLong,
	"long long int": LongLong, "signed long long int": LongLong,
	"unsigned long long": ULongLong, "unsigned long long int": ULongLong,

	// gcc's 128-bit integers. The signedness keywords combine with
	// __int128 exactly as they do with int, and int itself does not:
	// `unsigned int __int128` is two data types, which the absence of
	// an entry here reports.
	"__int128": Int128, "signed __int128": Int128,
	"unsigned __int128": UInt128,

	"__int64": LongLong, "signed __int64": LongLong,
	"unsigned __int64": ULongLong,

	"__int32": Int, "signed __int32": Int,
	"unsigned __int32": UInt,

	"__int16": Short, "signed __int16": Short,
	"unsigned __int16": UShort,

	"__int8": SChar, "signed __int8": SChar,
	"unsigned __int8": UChar,

	"float": Float, "double": Double, "long double": LongDouble,
	"float _Complex": ComplexFloat, "double _Complex": ComplexDouble,
	"long double _Complex": ComplexLongDouble,
}

// ---- declarators, read inside-out ----

type builder struct {
	unit   *token.File
	r      Resolver
	param  bool
	arrays map[*Array]*ast.ArrayDeclarator

	// incomplete holds each array whose element type was not complete,
	// against the element type to name in the report. It is a map and not
	// a report on the spot because whether it is one depends on a fact the
	// derivation does not have yet — see the loop in BuildDeclarator.
	incomplete map[*Array]Type
}

// BuildDeclarator derives a type from base through a declarator
// tree, reporting derivation constraints as it goes. It returns the
// derived type and the declared identifier (nil for an abstract
// declarator). param enables the parameter-only forms and checks.
//
// A nil declarator is the fully absent one: base itself, no name.
func BuildDeclarator(unit *token.File, base Type, d ast.Declarator, param bool, r Resolver) (Type, *ast.Ident) {
	b := &builder{unit: unit, r: r, param: param,
		arrays:     map[*Array]*ast.ArrayDeclarator{},
		incomplete: map[*Array]Type{},
	}
	t, id := b.build(base, d)

	// [static …] and [*] belong only to the outermost array
	// derivation of a parameter (§6.7.6.2p3, §6.7.6.3p7) — the one
	// the parameter adjustment rewrites.
	outer, _ := Unqualify(t).(*Array)
	for arr, node := range b.arrays {
		if !arr.Static && arr.Form != StarArray {
			continue
		}
		if !param || arr != outer {
			b.r.Report(node, "'static' and '[*]' require the outermost array of a function parameter")
			arr.Static = false
			if arr.Form == StarArray {
				arr.Form = IncompleteArray
			}
		}
	}

	// §6.7.6.2p1 wants a complete element type, and the outermost array of
	// a parameter is the one place there is no array to want it for: the
	// same adjustment rewrites it to a pointer to the element (§6.7.6.3p7),
	// so `void f(struct S a[])` is `void f(struct S *a)` and a forward
	// declaration is all it needs. The Windows SDK's propidlbase.h declares
	// one over a PROPVARIANT completed further down the header, and gcc,
	// clang and MSVC all accept it.
	//
	// Every other array in the derivation is a real one, including the
	// inner arrays of a multidimensional parameter, and is reported.
	for arr, elem := range b.incomplete {
		if param && arr == outer {
			continue
		}
		b.r.Report(b.arrays[arr], "array has incomplete element type "+elem.String())
	}
	return t, id
}

func (b *builder) build(base Type, d ast.Declarator) (Type, *ast.Ident) {
	switch d := d.(type) {
	case nil, *ast.BadDeclarator:
		return base, nil

	case *ast.NameDeclarator:
		return base, d.Ident

	case *ast.ParenDeclarator:
		return b.build(base, d.Inner)

	case *ast.PtrDeclarator:
		var q Qual
		for _, s := range d.Quals {
			switch s.(*ast.KeywordSpec).Kind {
			case token.CONST:
				q |= QConst
			case token.VOLATILE:
				q |= QVolatile
			case token.RESTRICT:
				q |= QRestrict
			case token.ATOMIC:
				q |= QAtomic
			}
		}
		return b.build(Qualify(&Pointer{Elem: base}, q), d.Inner)

	case *ast.ArrayDeclarator:
		arr := &Array{Elem: base}
		b.arrays[arr] = d
		switch under := Unqualify(base); {
		case under.Kind() == FuncKind:
			b.r.Report(d, "array of functions")
			arr.Elem = &Pointer{Elem: base}
		case !b.complete(under):
			b.incomplete[arr] = base
		}
		switch {
		case d.Star.IsValid():
			arr.Form = StarArray
		case d.Len == nil:
			arr.Form = IncompleteArray
		default:
			if n, ok := b.r.Eval(d.Len); ok {
				switch {
				case n < 0:
					b.r.Report(d.Len, "array length is negative")
					n = 1
				case n == 0:
					// gcc's zero-length array: a struct's trailing member
					// with no elements, which is what C code written before
					// C99's flexible array member uses and every such
					// header still says. It is a complete type of size zero,
					// so a struct ending in one is the size of everything
					// before it.
				}
				arr.Form, arr.Len = FixedArray, n
			} else {
				arr.Form = VLA // legality per scope is the analyzer's call
			}
		}
		arr.Static = d.Static.IsValid()
		var q Qual
		for _, s := range d.Quals {
			switch s.(*ast.KeywordSpec).Kind {
			case token.CONST:
				q |= QConst
			case token.VOLATILE:
				q |= QVolatile
			case token.RESTRICT:
				q |= QRestrict
			case token.ATOMIC:
				q |= QAtomic
			}
		}
		// Qualifiers in the brackets belong to the parameter's
		// adjusted pointer (§6.7.6.3p7); outside a parameter their
		// presence is a constraint violation caught with static/[*].
		return b.build(Qualify(arr, q), d.Inner)

	case *ast.FuncDeclarator:
		fn := &Func{Ret: base}
		switch under := Unqualify(base).Kind(); under {
		case FuncKind:
			b.r.Report(d, "function returning a function")
			fn.Ret = Typ(Int)
		case ArrayKind:
			b.r.Report(d, "function returning an array")
			fn.Ret = Typ(Int)
		}
		b.buildParams(fn, d)
		return b.build(fn, d.Inner)
	}
	return base, nil
}

func (b *builder) buildParams(fn *Func, d *ast.FuncDeclarator) {
	if len(d.Params) == 0 {
		// f() and f(a, b): parameters unspecified; the K&R names,
		// if any, get their types from the definition's
		// declaration list — the analyzer's job.
		return
	}
	fn.Proto = true
	fn.Variadic = d.Ellipsis.IsValid()

	for _, p := range d.Params {
		sp := BuildSpecs(b.unit, p.Specs, b.r)
		if sp.Storage != 0 && sp.Storage != token.REGISTER {
			b.r.Report(p, "parameter storage class must be register or nothing")
		}
		inner := &builder{unit: b.unit, r: b.r, param: true, arrays: map[*Array]*ast.ArrayDeclarator{}}
		t, id := BuildDeclarator(b.unit, sp.Type, p.Decl, true, inner.r)

		// (void) as the whole list means no parameters.
		if len(d.Params) == 1 && id == nil &&
			Unqualify(t).Kind() == Void && QualsOf(t) == 0 {
			return
		}
		if Unqualify(t).Kind() == Void {
			b.r.Report(p, "'void' must be the only parameter and unnamed")
			t = Typ(Int)
		}
		t = AdjustParam(t)
		name := ""
		if id != nil {
			name = id.Name(b.unit)
		}
		fn.Params = append(fn.Params, Param{Name: name, Type: t})
	}
}

// AdjustParam applies §6.7.6.3p7–8: a parameter of array type
// becomes a pointer to the element type, carrying the qualifiers
// written inside the brackets; a parameter of function type becomes
// a pointer to it.
func AdjustParam(t Type) Type {
	q := QualsOf(t)
	switch u := Unqualify(t).(type) {
	case *Array:
		return Qualify(&Pointer{Elem: u.Elem}, q)
	case *Func:
		return &Pointer{Elem: u}
	}
	return t
}

// complete reports whether t can be an array element or a declared
// object's type: not void, not an incomplete record or enum, not an
// incomplete array.
func (b *builder) complete(t Type) bool {
	switch t := Unqualify(t).(type) {
	case *Basic:
		return t.K != Void
	case *Record:
		return t.Complete
	case *Enum:
		return t.Complete
	case *Array:
		return t.Form != IncompleteArray
	}
	return true
}

// Complete is the exported form of the element/object completeness
// test, for the analyzer's declared-object checks.
func Complete(t Type) bool {
	return (&builder{}).complete(t)
}
