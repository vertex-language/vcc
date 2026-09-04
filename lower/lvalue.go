package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/analyzer"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// lval is a designated object: where it is, what type it has, and — for a
// bit-field — which bits of the unit at addr it occupies.
//
// A bit-field is the reason this is not just an ir.Ptr. It has no address in
// C either (§6.5.3.2p1 forbids &), which is exactly the property that makes
// it need a representation of its own rather than a pointer with a footnote.
type lval struct {
	addr ir.Ptr
	t    types.Type
	bit  bool
	p    place
}

// lvalue emits an expression as a designation rather than a value.
func (u *unit) lvalue(e ast.Expr) lval {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return u.lvalue(e.X)

	case *ast.Ident:
		o := u.resolve(e)
		if o == nil {
			return lval{t: types.Typ(types.Int)}
		}
		if o.isEnum {
			u.errorf(e, "%s is an enumeration constant, not an object", o.name)
			return lval{t: o.typ}
		}
		return lval{addr: u.addrOf(o, e), t: o.typ}

	case *ast.UnaryExpr:
		if e.Op == token.MUL {
			// *p designates what p points at; the pointer's qualifiers are
			// not the object's, but the pointee type's are.
			v := u.expr(e.X)
			p, ok := asPointer(v.t)
			if !ok {
				u.errorf(e, "cannot dereference a value of type %s", v.t)
				return lval{t: types.Typ(types.Int)}
			}
			return lval{addr: u.ptr(v.v, e), t: p.Elem}
		}

	case *ast.IndexExpr:
		return u.index(e)

	case *ast.SelectorExpr:
		return u.selector(e)

	case *ast.StringLit:
		v := u.stringLit(e)
		return lval{addr: u.ptr(v.v, e), t: v.t}

	case *ast.CompoundLit:
		return u.compoundLit(e)

	case *ast.CallExpr:
		// A call returning a record: §6.5.2.2p5 makes the result an object
		// with automatic duration, so it gets a frame slot and member access
		// on it works like any other.
		v := u.call(e)
		if _, ok := asRecord(v.t); ok {
			return lval{addr: u.ptr(v.v, e), t: v.t}
		}

	case *ast.CondExpr:
		// Not an lvalue in C, but a record-valued conditional still needs an
		// address for a subsequent member access.
		v := u.conditional(e)
		if _, ok := asRecord(v.t); ok {
			return lval{addr: u.ptr(v.v, e), t: v.t}
		}
	}
	u.errorf(e, "internal: %T is not an lvalue", e)
	return lval{t: types.Typ(types.Int)}
}

// index is a[i], which §6.5.2.1p2 defines as *(a + i) — and is lowered as
// exactly that, including the case where the array is the subscript.
func (u *unit) index(e *ast.IndexExpr) lval {
	x, i := u.expr(e.X), u.expr(e.Index)
	if !isPtrType(x.t) {
		x, i = i, x
	}
	p, ok := asPointer(x.t)
	if !ok {
		u.errorf(e, "cannot subscript a value of type %s", x.t)
		return lval{t: types.Typ(types.Int)}
	}
	addr := u.ptrAdd(x, i, p.Elem, e)
	return lval{addr: u.ptr(addr.v, e), t: p.Elem}
}

// selector is x.m and x->m.
//
// The member path may descend through anonymous members, so the offset is a
// sum rather than a single lookup, and the qualifiers of the containing
// object propagate to the member (§6.5.2.3p3): a member of a const struct is
// const whether or not it was declared so.
func (u *unit) selector(e *ast.SelectorExpr) lval {
	var base ir.Ptr
	var rt types.Type

	if e.Op == token.ARROW {
		v := u.expr(e.X)
		p, ok := asPointer(v.t)
		if !ok {
			u.errorf(e, "cannot use -> on a value of type %s", v.t)
			return lval{t: types.Typ(types.Int)}
		}
		base, rt = u.ptr(v.v, e), p.Elem
	} else {
		l := u.lvalue(e.X)
		base, rt = l.addr, l.t
	}

	r, ok := asRecord(rt)
	if !ok {
		u.errorf(e, "%s is not a structure or union", rt)
		return lval{t: types.Typ(types.Int)}
	}
	name := u.name(e.Sel)
	path := u.types.member(r, name)
	if path == nil {
		u.errorf(e, "%s has no member named %s", rt, name)
		return lval{t: types.Typ(types.Int)}
	}

	off, last := u.types.offsetOf(path)
	final := path[len(path)-1]
	f := final.rec.Fields[final.index]

	mt := f.Type
	if q := types.QualsOf(rt); q != 0 {
		mt = types.Qualify(mt, q)
	}

	addr := base
	if off != 0 {
		b := u.blk()
		addr = b.Ptr.Add(base, b.I64.Const(off))
	}
	if f.BitField {
		return lval{addr: addr, t: mt, bit: true, p: last}
	}
	return lval{addr: addr, t: mt}
}

// compoundLit gives a compound literal storage.
//
// §6.5.2.5p5–6: at file scope it has static duration and becomes a module
// global; inside a function it has automatic duration and the enclosing
// block's lifetime, which is an alloca in the entry block like any other
// automatic object.
func (u *unit) compoundLit(e *ast.CompoundLit) lval {
	t := u.compoundLitType(e)
	if u.fn == nil {
		return lval{addr: u.blk().Ptr.GetAddr(u.staticCompoundLit(e, t)), t: t}
	}
	addr := u.alloca(t, "cl", e)
	u.initObject(addr, t, e.Init, e)
	return lval{addr: addr, t: t}
}

// staticCompoundLit is the module global a file-scope compound literal
// becomes.
//
// §6.5.2.5p5: outside a function the unnamed object has static storage
// duration, which makes its address an address constant — so it may
// initialize another object with static duration, and the fold in const.go
// needs the symbol without a block to hang a getaddr off.
func (u *unit) staticCompoundLit(e *ast.CompoundLit, t types.Type) ir.Symbol {
	if g, ok := u.clits[e]; ok {
		return g
	}
	g := u.mod.Global(u.sym(u.uniq("cl")), ir.RW, u.types.ftype(t)).Internal()
	g.Init(u.staticInit(t, e.Init))
	u.clits[e] = g
	return g
}

// compoundLitType completes an array compound literal's type from its
// initializer, which is the one thing its type name may leave open.
func (u *unit) compoundLitType(e *ast.CompoundLit) types.Type {
	return u.completeArray(u.typeOf(e.Type), e.Init)
}

// assign is = and the compound assignments.
//
// §6.5.16.2p3 says the compound form evaluates its left operand once. That is
// the whole reason this shares a single lvalue between the load and the
// store rather than lowering `a[i()] += 1` as `a[i()] = a[i()] + 1`.
func (u *unit) assign(e *ast.AssignExpr) value {
	l := u.lvalue(e.Lhs)
	atomic := types.QualsOf(l.t)&types.QAtomic != 0

	if e.Op == token.ASSIGN {
		v := u.expr(e.Rhs)
		u.storeLval(v, l, e)
		if atomic {
			// The result is the value assigned, not a second read of the
			// object: reading it again would be another atomic access, and
			// another thread may have written between the two.
			return u.convert(v, types.Unqualify(l.t), e)
		}
		return u.load(l, e)
	}

	op := compoundOp(e.Op)
	if atomic {
		// One indivisible read-modify-write; the value of the expression is
		// the new one (§6.5.16.2p3).
		_, updated := u.atomicUpdate(op, l, u.expr(e.Rhs), e)
		return updated
	}
	old := u.load(l, e)
	rhs := u.expr(e.Rhs)
	b := u.blk()

	// Pointer += integer keeps the pointer's type rather than converting to
	// a common one.
	if p, ok := asPointer(types.Unqualify(l.t)); ok {
		switch op {
		case token.ADD:
			u.storeLval(u.ptrAdd(old, rhs, p.Elem, e), l, e)
		case token.SUB:
			neg := value{u.emitNeg(b, u.convert(rhs, u.diffType(), e).v, e), u.diffType()}
			u.storeLval(u.ptrAdd(old, neg, p.Elem, e), l, e)
		default:
			u.errorf(e, "operator %s does not apply to pointers", e.Op)
		}
		return u.load(l, e)
	}

	res := u.arith(op, old, rhs, e)
	u.storeLval(res, l, e)
	return u.load(l, e)
}

// arith applies one binary operator to two already-evaluated operands, doing
// the conversions §6.3.1.8 asks for. It is the arithmetic half of a compound
// assignment, shared with the atomic read-modify-write in atomic.go, which has
// to compute the same thing in a different order.
func (u *unit) arith(op token.Kind, lhs, rhs value, at ast.Node) value {
	b := u.blk()
	if op == token.SHL || op == token.SHR {
		// A shift converts each operand on its own: the result's type is the
		// promoted left operand's, whatever the right operand is.
		ct := u.promote(lhs.t)
		x := u.convert(lhs, ct, at)
		amt := u.convert(rhs, ct, at)
		return value{u.emitShift(b, x.v, amt.v, ct, op == token.SHL, at), ct}
	}
	ct := u.usual(lhs.t, rhs.t)
	x := u.convert(lhs, ct, at)
	y := u.convert(rhs, ct, at)
	switch op {
	case token.ADD:
		return value{u.emitAdd(b, x.v, y.v, ct, at), ct}
	case token.SUB:
		return value{u.emitSub(b, x.v, y.v, ct, at), ct}
	case token.MUL:
		return value{u.emitMul(b, x.v, y.v, ct, at), ct}
	case token.QUO:
		return value{u.emitDiv(b, x.v, y.v, ct, at), ct}
	case token.REM:
		return value{u.emitRem(b, x.v, y.v, ct, at), ct}
	}
	return value{u.emitBitwise(b, x.v, y.v, op, at), ct}
}

// compoundOp maps `+=` to `+`, and so on.
func compoundOp(k token.Kind) token.Kind {
	switch k {
	case token.ADD_ASSIGN:
		return token.ADD
	case token.SUB_ASSIGN:
		return token.SUB
	case token.MUL_ASSIGN:
		return token.MUL
	case token.QUO_ASSIGN:
		return token.QUO
	case token.REM_ASSIGN:
		return token.REM
	case token.SHL_ASSIGN:
		return token.SHL
	case token.SHR_ASSIGN:
		return token.SHR
	case token.AND_ASSIGN:
		return token.AND
	case token.OR_ASSIGN:
		return token.OR
	case token.XOR_ASSIGN:
		return token.XOR
	}
	return k
}

// staticType computes an expression's type without emitting anything.
//
// It exists for the three places C needs a type before it needs a value:
// sizeof's expression form, _Generic's controlling expression, and the result
// type of ?:. It mirrors expr's typing rules and must stay in step with them
// — the alternative being an analyzer that records expression types, which is
// the fix rather than this function.
func (u *unit) staticType(e ast.Expr) types.Type {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return u.staticType(e.X)
	case *ast.Ident:
		if o := u.lookup(u.name(e)); o != nil {
			if o.isEnum {
				return types.Typ(types.Int)
			}
			return o.typ
		}
	case *ast.BasicLit:
		switch e.Kind {
		case token.INT_LIT:
			return u.decodeInt(e).Type
		case token.CHAR_LIT:
			// §6.4.4.4: plain is int, but L'x' is wchar_t, u'x' is char16_t
			// and U'x' is char32_t — and sizeof asks this function, so
			// answering int for all four makes sizeof(u'x') four bytes.
			// Reports are silenced: literal() decodes the same text and
			// diagnoses it once.
			return analyzer.DecodeCharConst(string(u.src.Slice(e.Pos(), e.End())),
				u.model, func(string) {}).Type
		case token.FLOAT_LIT:
			_, t := analyzer.DecodeFloatConst(string(u.src.Slice(e.Pos(), e.End())), func(string) {})
			return t
		}
	case *ast.StringLit:
		return u.stringType(e)
	case *ast.CastExpr:
		return u.typeOf(e.Type)
	case *ast.CompoundLit:
		return u.typeOf(e.Type)
	case *ast.SizeofExpr, *ast.AlignofExpr:
		return u.sizeType()
	case *ast.UnaryExpr:
		switch e.Op {
		case token.AND:
			return &types.Pointer{Elem: u.staticType(e.X)}
		case token.MUL:
			if p, ok := asPointer(u.decayType(u.staticType(e.X))); ok {
				return p.Elem
			}
		case token.NOT:
			return types.Typ(types.Int)
		default:
			return u.promote(u.staticType(e.X))
		}
	case *ast.IncDecExpr:
		return u.staticType(e.X)
	case *ast.IndexExpr:
		if p, ok := asPointer(u.decayType(u.staticType(e.X))); ok {
			return p.Elem
		}
		if p, ok := asPointer(u.decayType(u.staticType(e.Index))); ok {
			return p.Elem
		}
	case *ast.SelectorExpr:
		rt := u.staticType(e.X)
		if e.Op == token.ARROW {
			if p, ok := asPointer(u.decayType(rt)); ok {
				rt = p.Elem
			}
		}
		if r, ok := asRecord(rt); ok {
			if path := u.types.member(r, u.name(e.Sel)); path != nil {
				last := path[len(path)-1]
				return last.rec.Fields[last.index].Type
			}
		}
	case *ast.CallExpr:
		ft := u.decayType(u.staticType(e.Fun))
		if p, ok := asPointer(ft); ok {
			ft = p.Elem
		}
		if f, ok := asFunc(ft); ok {
			return f.Ret
		}
	case *ast.AssignExpr:
		return types.Unqualify(u.staticType(e.Lhs))
	case *ast.CondExpr:
		return u.condType(e)
	case *ast.BinaryExpr:
		return u.staticBinary(e)
	case *ast.GenericExpr:
		ct := u.decayType(u.staticType(e.Ctrl))
		for _, a := range e.Assocs {
			if a.Type != nil && u.compatible(ct, u.typeOf(a.Type)) {
				return u.staticType(a.Value)
			}
		}
		for _, a := range e.Assocs {
			if a.Type == nil {
				return u.staticType(a.Value)
			}
		}
	}
	return types.Typ(types.Int)
}

func (u *unit) staticBinary(e *ast.BinaryExpr) types.Type {
	switch e.Op {
	case token.LAND, token.LOR, token.EQL, token.NEQ,
		token.LSS, token.GTR, token.LEQ, token.GEQ:
		return types.Typ(types.Int)
	case token.COMMA:
		return u.staticType(e.Y)
	}
	x := u.decayType(u.staticType(e.X))
	y := u.decayType(u.staticType(e.Y))
	switch e.Op {
	case token.SHL, token.SHR:
		return u.promote(x)
	case token.ADD:
		if isPtrType(x) {
			return x
		}
		if isPtrType(y) {
			return y
		}
	case token.SUB:
		if isPtrType(x) && isPtrType(y) {
			return u.diffType()
		}
		if isPtrType(x) {
			return x
		}
	}
	return u.usual(x, y)
}

func (u *unit) decayType(t types.Type) types.Type {
	return u.decay(value{nil, t}).t
}