package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/analyzer"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// value is an emitted expression: the register holding it and the C type it
// has. The type is the whole reason this is a struct — VIR's registers are
// signless, so `%0 i32` alone cannot say whether a shift is arithmetic.
type value struct {
	v ir.Value
	t types.Type
}

func (u *unit) poison(t types.Type) value { return value{nil, t} }

// expr emits an expression as an rvalue, with the lvalue conversion of
// §6.3.2.1 and the array/function decay already applied. This is what every
// operand position wants; a caller needing an address asks lvalue instead.
func (u *unit) expr(e ast.Expr) value {
	v := u.exprNoDecay(e)
	return u.decay(v)
}

func (u *unit) exprNoDecay(e ast.Expr) value {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return u.exprNoDecay(e.X)

	case *ast.Ident:
		return u.ident(e)

	case *ast.BasicLit:
		return u.literal(e)

	case *ast.StringLit:
		return u.stringLit(e)

	case *ast.UnaryExpr:
		return u.unary(e)

	case *ast.IncDecExpr:
		return u.incDec(e.X, e.Op, true, e)

	case *ast.BinaryExpr:
		return u.binary(e)

	case *ast.AssignExpr:
		return u.assign(e)

	case *ast.CondExpr:
		return u.conditional(e)

	case *ast.StmtExpr:
		return u.stmtExpr(e)

	case *ast.LabelAddrExpr:
		return u.labelAddr(e)

	case *ast.OffsetofExpr:
		return u.offsetofExpr(e)

	case *ast.TypesCompatibleExpr:
		return u.typesCompatibleExpr(e)

	case *ast.VaArgExpr:
		return u.vaArgExpr(e)

	case *ast.CallExpr:
		return u.call(e)

	case *ast.CastExpr:
		return u.cast(e)

	case *ast.SizeofExpr:
		return u.sizeofExpr(e)

	case *ast.AlignofExpr:
		return u.intConst(u.alignof(u.typeOf(e.Type), e), u.sizeType())

	case *ast.IndexExpr, *ast.SelectorExpr, *ast.CompoundLit:
		return u.load(u.lvalue(e), e)

	case *ast.GenericExpr:
		return u.generic(e)

	case *ast.InitList:
		u.errorf(e, "internal: a braced initializer is not an expression here")
		return u.poison(types.Typ(types.Int))

	case *ast.BadExpr:
		return u.poison(types.Typ(types.Int))
	}
	u.errorf(e, "internal: %T is not lowered", e)
	return u.poison(types.Typ(types.Int))
}

// ident resolves an ordinary identifier.
//
// Three outcomes, and the scope entry says which: an enumeration constant is
// a literal, a function is its own address, and an object is a load from
// wherever it lives — except an array, which stays an address because decay
// is about to turn it into one anyway.
func (u *unit) ident(id *ast.Ident) value {
	o := u.resolve(id)
	if o == nil {
		return u.poison(types.Typ(types.Int))
	}
	if o.isEnum {
		return u.intConst(o.val, o.typ)
	}
	if _, ok := asFunc(o.typ); ok {
		return value{u.addrOf(o, id), o.typ}
	}
	lv := lval{addr: u.addrOf(o, id), t: o.typ}
	if _, ok := asArray(o.typ); ok {
		return value{lv.addr, o.typ}
	}
	return u.load(lv, id)
}

// addrOf yields the address of an object, wherever it lives.
func (u *unit) addrOf(o *object, at ast.Node) ir.Ptr {
	b := u.blk()
	switch {
	case o.isStatic():
		sym := u.imported(o)
		if isThreadLocal(sym) {
			return b.Ptr.TLSAddr(sym)
		}
		return b.Ptr.GetAddr(sym)
	case !o.addr.IsZero():
		return o.addr
	}
	u.errorf(at, "internal: %s has no address", o.name)
	return ir.Ptr{}
}

func (u *unit) literal(e *ast.BasicLit) value {
	b := u.blk()
	switch e.Kind {
	case token.INT_LIT:
		iv := u.decodeInt(e)
		return u.intConst(int64(iv.Value), iv.Type)
	case token.CHAR_LIT:
		iv := analyzer.DecodeCharConst(string(u.src.Slice(e.Pos(), e.End())), u.model,
			func(msg string) { u.errorf(e, "%s", msg) })
		return u.intConst(int64(iv.Value), iv.Type)
	case token.FLOAT_LIT:
		f, t := analyzer.DecodeFloatConst(string(u.src.Slice(e.Pos(), e.End())),
			func(msg string) { u.errorf(e, "%s", msg) })
		rt, _ := u.types.regType(t)
		switch rt {
		case ir.TypeF32:
			return value{b.F32.Const(f), t}
		case ir.TypeF64:
			return value{b.F64.Const(f), t}
		case ir.TypeF80:
			return value{b.F80().Const(f), t}
		case ir.TypeF128:
			return value{b.F128().Const(f), t}
		}
	}
	u.errorf(e, "internal: literal kind %s is not lowered", e.Kind)
	return u.poison(types.Typ(types.Int))
}

func (u *unit) decodeInt(e *ast.BasicLit) analyzer.IntValue {
	return analyzer.DecodeIntConst(string(u.src.Slice(e.Pos(), e.End())), u.model,
		func(msg string) { u.errorf(e, "%s", msg) })
}

// intConst materializes an integer constant in the register its type lives in.
func (u *unit) intConst(v int64, t types.Type) value {
	b := u.blk()
	if rt, _ := u.types.regType(t); rt == ir.TypeI64 {
		return value{b.I64.Const(v), t}
	}
	return value{b.I32.Const(v), t}
}

// load reads an object out of memory.
//
// A record or an array is not loaded: it has no register type, and every
// operation C admits on one — assignment, argument passing, member access —
// wants the address. Those callers get the address back with the aggregate
// type still attached, and the sub-width verbs handle everything narrower
// than a register.
func (u *unit) load(l lval, at ast.Node) value {
	if l.addr.IsZero() {
		return u.poison(l.t)
	}
	t := types.Unqualify(l.t)
	// A vector record is the exception: it is loaded into a register,
	// which is what makes the intrinsics over it instructions. Every
	// other record is carried by address.
	if r, ok := asRecord(t); ok && !r.Vector {
		return value{l.addr, l.t}
	}
	if _, ok := asArray(t); ok {
		return value{l.addr, l.t}
	}
	if _, ok := t.(*types.Func); ok {
		// `*fp` designates the function, and a function designator's value
		// is its address — §6.3.2.1p4 turns it straight back into a pointer,
		// which is why `(*fp)(x)` and `fp(x)` are the same call. Nothing is
		// loaded: there is no object here, only a place in .text.
		return value{l.addr, l.t}
	}
	if l.bit {
		return u.loadBits(l, at)
	}
	if types.QualsOf(l.t)&types.QAtomic != 0 {
		return u.atomicLoad(l, at)
	}
	b := u.blk()
	var attrs []ir.MemAttr
	if types.QualsOf(l.t)&types.QVolatile != 0 {
		attrs = append(attrs, ir.Volatile)
	}
	st, ok := u.types.storeType(t)
	if !ok {
		if !u.unsupported128(at, t) {
			u.errorf(at, "cannot load a value of type %s", l.t)
		}
		return u.poison(l.t)
	}
	rt, _ := u.types.regType(t)
	_, signed := u.model.IntBits(t)
	if isBoolType(t) {
		// _Bool is one byte holding 0 or 1; nothing else may be observed
		// through it, so a plain zero-extending load is exact.
		return value{b.I32.ULoad8(l.addr, attrs...), l.t}
	}
	switch st {
	case ir.StoreI8:
		if rt == ir.TypeI64 {
			if signed {
				return value{b.I64.SLoad8(l.addr, attrs...), l.t}
			}
			return value{b.I64.ULoad8(l.addr, attrs...), l.t}
		}
		if signed {
			return value{b.I32.SLoad8(l.addr, attrs...), l.t}
		}
		return value{b.I32.ULoad8(l.addr, attrs...), l.t}
	case ir.StoreI16:
		if rt == ir.TypeI64 {
			if signed {
				return value{b.I64.SLoad16(l.addr, attrs...), l.t}
			}
			return value{b.I64.ULoad16(l.addr, attrs...), l.t}
		}
		if signed {
			return value{b.I32.SLoad16(l.addr, attrs...), l.t}
		}
		return value{b.I32.ULoad16(l.addr, attrs...), l.t}
	case ir.StoreI32:
		if rt == ir.TypeI64 {
			if signed {
				return value{b.I64.SLoad32(l.addr, attrs...), l.t}
			}
			return value{b.I64.ULoad32(l.addr, attrs...), l.t}
		}
		return value{b.I32.Load(l.addr, attrs...), l.t}
	case ir.StoreI64:
		return value{b.I64.Load(l.addr, attrs...), l.t}
	case ir.StoreF32:
		return value{b.F32.Load(l.addr, attrs...), l.t}
	case ir.StoreF64:
		return value{b.F64.Load(l.addr, attrs...), l.t}
	case ir.StoreF80:
		return value{b.F80().Load(l.addr, attrs...), l.t}
	case ir.StoreF128:
		return value{b.F128().Load(l.addr, attrs...), l.t}
	case ir.StoreV128:
		return value{b.V128().Load(l.addr, attrs...), l.t}
	case ir.StorePtr:
		return value{b.Ptr.Load(l.addr, attrs...), l.t}
	}
	if !u.unsupported128(at, t) {
		u.errorf(at, "cannot load a value of type %s", l.t)
	}
	return u.poison(l.t)
}

// store writes v into the object at addr, converting first.
func (u *unit) store(v value, addr ir.Ptr, t types.Type, at ast.Node) {
	if addr.IsZero() {
		return
	}
	u.storeLval(v, lval{addr: addr, t: t}, at)
}

func (u *unit) storeLval(v value, l lval, at ast.Node) {
	b := u.blk()
	t := types.Unqualify(l.t)

	if r, ok := asRecord(t); ok && !r.Vector {
		src := u.ptr(v.v, at)
		if src.IsZero() || l.addr.IsZero() {
			return
		}
		n := u.types.layout(r).Size
		b.MemCpy(l.addr, src, b.I64.Const(n))
		return
	}
	if l.bit {
		u.storeBits(v, l, at)
		return
	}
	if types.QualsOf(l.t)&types.QAtomic != 0 {
		u.atomicStore(v, l, at)
		return
	}
	cv := u.convert(v, t, at)
	if cv.v == nil {
		return
	}
	var attrs []ir.MemAttr
	if types.QualsOf(l.t)&types.QVolatile != 0 {
		attrs = append(attrs, ir.Volatile)
	}
	st, ok := u.types.storeType(t)
	if !ok {
		u.errorf(at, "cannot store a value of type %s", l.t)
		return
	}
	switch st {
	case ir.StoreI8:
		if x, ok := cv.v.(ir.I64); ok {
			b.I64.Store8(x, l.addr, attrs...)
		} else {
			b.I32.Store8(u.i32(cv.v, at), l.addr, attrs...)
		}
	case ir.StoreI16:
		if x, ok := cv.v.(ir.I64); ok {
			b.I64.Store16(x, l.addr, attrs...)
		} else {
			b.I32.Store16(u.i32(cv.v, at), l.addr, attrs...)
		}
	case ir.StoreI32:
		if x, ok := cv.v.(ir.I64); ok {
			b.I64.Store32(x, l.addr, attrs...)
		} else {
			b.I32.Store(u.i32(cv.v, at), l.addr, attrs...)
		}
	case ir.StoreI64:
		b.I64.Store(u.i64(cv.v, at), l.addr, attrs...)
	case ir.StoreF32:
		b.F32.Store(cv.v.(ir.F32), l.addr, attrs...)
	case ir.StoreF64:
		b.F64.Store(cv.v.(ir.F64), l.addr, attrs...)
	case ir.StoreF80:
		b.F80().Store(cv.v.(ir.F80), l.addr, attrs...)
	case ir.StoreF128:
		b.F128().Store(cv.v.(ir.F128), l.addr, attrs...)
	case ir.StoreV128:
		b.V128().Store(cv.v.(ir.V128), l.addr, attrs...)
	case ir.StorePtr:
		b.Ptr.Store(u.ptr(cv.v, at), l.addr, attrs...)
	}
}

// loadBits extracts a bit-field: read the storage unit, shift the field down,
// and mask or sign-extend it to the field's width.
func (u *unit) loadBits(l lval, at ast.Node) value {
	unit := u.unitType(l.p.Unit)
	raw := u.load(lval{addr: l.addr, t: unit}, at)
	if raw.v == nil {
		return u.poison(l.t)
	}
	b := u.blk()
	_, signed := u.model.IntBits(types.Unqualify(l.t))

	// Shift the field to the top of the *register* and back down: one shift
	// pair gives both the mask and the sign extension, and the arithmetic
	// form picks which.
	//
	// The register, not the storage unit. A unit narrower than the register —
	// a short or a char bit-field — sits in the register's low bits, so
	// shifting by the unit's width leaves the field's sign bit at bit 15 or
	// bit 7 and the arithmetic shift replicates whatever the load left above
	// it instead.
	//
	// Nor is the result converted afterwards. A W-bit field extracted this
	// way is already held the way its declared type is held, and converting
	// it to that type would truncate to the type's full width: a 4-bit
	// signed char holding 7 would come back as the 8-bit pattern 0xf7.
	regBits := int64(32)
	isWide := false
	if _, ok := raw.v.(ir.I64); ok {
		regBits, isWide = 64, true
	}
	hi := regBits - (l.p.BitOff + l.p.Width)
	lo := regBits - l.p.Width
	if isWide {
		x := raw.v.(ir.I64)
		x = b.I64.Shl(x, b.I64.Const(hi))
		if signed {
			x = b.I64.SShr(x, b.I64.Const(lo))
		} else {
			x = b.I64.UShr(x, b.I64.Const(lo))
		}
		return u.fitBits(x, p32(64, signed), l.t, at)
	}
	x := u.i32(raw.v, at)
	x = b.I32.Shl(x, b.I32.Const(hi))
	if signed {
		x = b.I32.SShr(x, b.I32.Const(lo))
	} else {
		x = b.I32.UShr(x, b.I32.Const(lo))
	}
	if isBoolType(types.Unqualify(l.t)) {
		// _Bool observes only 0 and 1, whatever the bit holds.
		return u.convert(value{x, unit}, l.t, at)
	}
	return u.fitBits(x, p32(regBits, signed), l.t, at)
}

// p32 names an integer type of a given register width and signedness, for
// describing a value that has been extracted but not yet converted.
func p32(regBits int64, signed bool) types.Type {
	switch {
	case regBits >= 64 && signed:
		return types.Typ(types.LongLong)
	case regBits >= 64:
		return types.Typ(types.ULongLong)
	case signed:
		return types.Typ(types.Int)
	}
	return types.Typ(types.UInt)
}

// fitBits moves an extracted bit-field into the register its declared type
// uses.
//
// A packed bit-field's storage unit is chosen to cover its bits, not to match
// its declared type: three bits of an unsigned char can land in an eight-byte
// window. The value then comes out of an i64 while the type says i32, and the
// two have to be reconciled — numerically it is the same value, so this is a
// change of register and nothing else.
func (u *unit) fitBits(x ir.Value, from, to types.Type, at ast.Node) value {
	want, _ := u.types.regType(types.Unqualify(to))
	have := ir.TypeI32
	if _, ok := x.(ir.I64); ok {
		have = ir.TypeI64
	}
	if want == have {
		return value{x, to}
	}
	return value{u.intToInt(u.blk(), x, from, to), to}
}

// storeBits inserts a bit-field: read-modify-write of the storage unit.
//
// The read is unconditional even when the field covers the whole unit,
// because the unit may hold neighbouring fields and C says a store to one
// bit-field does not disturb another — except that adjacent fields in the
// same unit are one memory location for the purposes of §5.1.2.4, which is
// why this is a plain load and store and not an atomic sequence.
func (u *unit) storeBits(v value, l lval, at ast.Node) {
	unit := u.unitType(l.p.Unit)
	cv := u.convert(v, unit, at)
	if cv.v == nil {
		return
	}
	old := u.load(lval{addr: l.addr, t: unit}, at)
	if old.v == nil {
		return
	}
	b := u.blk()

	mask := int64(1)<<uint(l.p.Width) - 1
	if x, ok := old.v.(ir.I64); ok {
		nv := b.I64.And(u.i64(cv.v, at), b.I64.Const(mask))
		nv = b.I64.Shl(nv, b.I64.Const(l.p.BitOff))
		keep := b.I64.And(x, b.I64.Const(^(mask << uint(l.p.BitOff))))
		u.store(value{b.I64.Or(keep, nv), unit}, l.addr, unit, at)
		return
	}
	nv := b.I32.And(u.i32(cv.v, at), b.I32.Const(mask))
	nv = b.I32.Shl(nv, b.I32.Const(l.p.BitOff))
	// Narrowed to the register the mask is used in. mask and BitOff are
	// int64 arithmetic, so the complement of a field high in a 32-bit unit
	// is a 64-bit value with every bit above 31 set — 127<<25 complemented
	// is -4261412865, which is not an i32 and which the backend rejects when
	// it tries to materialize one.
	keep := b.I32.And(u.i32(old.v, at), b.I32.Const(int64(int32(^(mask << uint(l.p.BitOff))))))
	u.store(value{b.I32.Or(keep, nv), unit}, l.addr, unit, at)
}

// unitType names the unsigned integer type of a bit-field's storage unit.
func (u *unit) unitType(size int64) types.Type {
	switch {
	case size >= 8:
		return types.Typ(types.ULongLong)
	case size >= 4:
		return types.Typ(types.UInt)
	case size >= 2:
		return types.Typ(types.UShort)
	default:
		return types.Typ(types.UChar)
	}
}

func (u *unit) unary(e *ast.UnaryExpr) value {
	// The block is read after the operand, never before. Lowering an operand
	// can move the cursor — a conditional, a || , a statement expression all
	// end the block they started in — and emitting the operator into the
	// block that was current when the operator was reached puts it after
	// that block's terminator.
	switch e.Op {
	case token.AND:
		return u.addressOf(e.X, e)

	case token.MUL:
		return u.load(u.lvalue(e), e)

	case token.ADD:
		v := u.expr(e.X)
		t := u.promote(v.t)
		return u.convert(v, t, e)

	case token.SUB:
		v := u.expr(e.X)
		t := u.promote(v.t)
		v = u.convert(v, t, e)
		return value{u.emitNeg(u.blk(), v.v, e), t}

	case token.TILDE:
		v := u.expr(e.X)
		t := u.promote(v.t)
		v = u.convert(v, t, e)
		return value{u.emitNot(u.blk(), v.v, e), t}

	case token.NOT:
		c := u.truth(u.expr(e.X), e)
		if c.IsZero() {
			return u.poison(types.Typ(types.Int))
		}
		b := u.blk()
		return value{b.I32.ZExtI1(b.I1.Not(c)), types.Typ(types.Int)}

	case token.INC, token.DEC:
		return u.incDec(e.X, e.Op, false, e)
	}
	u.errorf(e, "internal: unary %s is not lowered", e.Op)
	return u.poison(types.Typ(types.Int))
}

// addressOf is &x. It is the one place an lvalue escapes as a value, and the
// one place a function designator is not decayed but named directly.
func (u *unit) addressOf(x ast.Expr, at ast.Node) value {
	if id, ok := stripParens(x).(*ast.Ident); ok {
		if o := u.lookup(u.name(id)); o != nil {
			if _, isFn := asFunc(o.typ); isFn {
				return value{u.addrOf(o, at), &types.Pointer{Elem: o.typ}}
			}
		}
	}
	l := u.lvalue(x)
	if l.bit {
		u.errorf(at, "cannot take the address of a bit-field (§6.5.3.2p1)")
		return u.poison(&types.Pointer{Elem: l.t})
	}
	return value{l.addr, &types.Pointer{Elem: l.t}}
}

// incDec is ++x, --x, x++ and x--, on arithmetic and on pointers alike.
func (u *unit) incDec(x ast.Expr, op token.Kind, postfix bool, at ast.Node) value {
	l := u.lvalue(x)
	if types.QualsOf(l.t)&types.QAtomic != 0 {
		// §6.5.2.4p2 and §6.5.3.1p2 define ++ as += 1, which on an _Atomic
		// object is one read-modify-write rather than a read and a write.
		add := token.ADD
		if op == token.DEC {
			add = token.SUB
		}
		before, after := u.atomicUpdate(add, l, u.intConst(1, types.Typ(types.Int)), at)
		if postfix {
			return before
		}
		return after
	}
	old := u.load(l, at)
	if old.v == nil {
		return u.poison(l.t)
	}
	t := types.Unqualify(l.t)

	var next value
	if p, ok := asPointer(t); ok {
		step := int64(1)
		if op == token.DEC {
			step = -1
		}
		next = u.ptrAdd(old, u.intConst(step, u.diffType()), p.Elem, at)
	} else {
		pt := u.promote(t)
		cur := u.convert(old, pt, at)
		one := u.convert(u.intConst(1, types.Typ(types.Int)), pt, at)
		b := u.blk()
		if op == token.INC {
			next = value{u.emitAdd(b, cur.v, one.v, pt, at), pt}
		} else {
			next = value{u.emitSub(b, cur.v, one.v, pt, at), pt}
		}
	}
	u.storeLval(next, l, at)
	if postfix {
		return old
	}
	return u.convert(next, t, at)
}

func (u *unit) binary(e *ast.BinaryExpr) value {
	switch e.Op {
	case token.LAND, token.LOR:
		return u.logical(e)
	case token.COMMA:
		u.discard(e.X)
		return u.expr(e.Y)
	}

	x, y := u.expr(e.X), u.expr(e.Y)
	b := u.blk()

	// Pointer arithmetic first: it is the case where the operands are not
	// converted to a common type at all.
	switch e.Op {
	case token.ADD:
		if p, ok := asPointer(x.t); ok {
			return u.ptrAdd(x, y, p.Elem, e)
		}
		if p, ok := asPointer(y.t); ok {
			return u.ptrAdd(y, x, p.Elem, e)
		}
	case token.SUB:
		if p, ok := asPointer(x.t); ok {
			if _, both := asPointer(y.t); both {
				return u.ptrDiff(x, y, p.Elem, e)
			}
			neg := value{u.emitNeg(b, u.convert(y, u.diffType(), e).v, e), u.diffType()}
			return u.ptrAdd(x, neg, p.Elem, e)
		}
	case token.SHL, token.SHR:
		// §6.5.7p3: each operand is promoted separately; there is no common
		// type, and the result type is the left operand's.
		xt := u.promote(x.t)
		lhs := u.convert(x, xt, e)
		rhs := u.convert(y, xt, e)
		return value{u.emitShift(b, lhs.v, rhs.v, xt, e.Op == token.SHL, e), xt}
	}

	if isPtrType(x.t) || isPtrType(y.t) {
		return u.pointerCompare(e, x, y)
	}

	ct := u.usual(x.t, y.t)
	lhs := u.convert(x, ct, e)
	rhs := u.convert(y, ct, e)

	switch e.Op {
	case token.ADD:
		return value{u.emitAdd(b, lhs.v, rhs.v, ct, e), ct}
	case token.SUB:
		return value{u.emitSub(b, lhs.v, rhs.v, ct, e), ct}
	case token.MUL:
		return value{u.emitMul(b, lhs.v, rhs.v, ct, e), ct}
	case token.QUO:
		return value{u.emitDiv(b, lhs.v, rhs.v, ct, e), ct}
	case token.REM:
		return value{u.emitRem(b, lhs.v, rhs.v, ct, e), ct}
	case token.AND:
		return value{u.emitBitwise(b, lhs.v, rhs.v, token.AND, e), ct}
	case token.OR:
		return value{u.emitBitwise(b, lhs.v, rhs.v, token.OR, e), ct}
	case token.XOR:
		return value{u.emitBitwise(b, lhs.v, rhs.v, token.XOR, e), ct}
	case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ:
		c := u.emitCompare(b, lhs.v, rhs.v, ct, e.Op, e)
		if c.IsZero() {
			return u.poison(types.Typ(types.Int))
		}
		return value{b.I32.ZExtI1(c), types.Typ(types.Int)}
	}
	u.errorf(e, "internal: binary %s is not lowered", e.Op)
	return u.poison(ct)
}

// pointerCompare handles the mixed pointer comparisons, including a null
// constant on either side.
func (u *unit) pointerCompare(e *ast.BinaryExpr, x, y value) value {
	b := u.blk()
	pt := x.t
	if !isPtrType(pt) {
		pt = y.t
	}
	lhs := u.convert(x, pt, e)
	rhs := u.convert(y, pt, e)
	if lhs.v == nil || rhs.v == nil {
		return u.poison(types.Typ(types.Int))
	}
	p, q := u.ptr(lhs.v, e), u.ptr(rhs.v, e)

	var c ir.I1
	switch e.Op {
	case token.EQL:
		c = b.Ptr.Eq(p, q)
	case token.NEQ:
		c = b.Ptr.Ne(p, q)
	case token.LSS:
		c = b.Ptr.Lt(p, q)
	case token.LEQ:
		c = b.Ptr.Le(p, q)
	case token.GTR:
		c = b.Ptr.Lt(q, p) // no gt: swap
	case token.GEQ:
		c = b.Ptr.Le(q, p)
	default:
		u.errorf(e, "operator %s does not apply to pointers", e.Op)
		return u.poison(types.Typ(types.Int))
	}
	return value{b.I32.ZExtI1(c), types.Typ(types.Int)}
}

func (u *unit) sizeofTypeVal(t types.Type, at ast.Node) value {
	if a, ok := asArray(t); ok && a.Form == types.VLA {
		return u.vlaSizeof(t, at)
	}
	return value{u.blk().I64.Const(u.sizeof(t, at)), types.Typ(types.LongLong)}
}

// ptrAdd is p + n, scaled by the element size.
func (u *unit) ptrAdd(p, n value, elem types.Type, at ast.Node) value {
	b := u.blk()
	off := u.convert(n, types.Typ(types.LongLong), at)
	if p.v == nil || off.v == nil {
		return value{nil, p.t}
	}
	szVal := u.sizeofTypeVal(elem, at)
	i := u.i64(off.v, at)

	isOne := false
	if a, ok := asArray(elem); !ok || a.Form != types.VLA {
		if u.sizeof(elem, at) == 1 {
			isOne = true
		}
	}
	if !isOne {
		szI := u.i64(szVal.v, at)
		i = b.I64.Mul(i, szI)
	}
	return value{b.Ptr.Add(u.ptr(p.v, at), i), p.t}
}

// ptrDiff is p - q in units of the element type. §6.5.6p9 makes a difference
// that does not divide evenly undefined; VIR's sdiv is exact and traps on
// nothing here, so the quotient is simply what the standard says it is when
// the pointers are in the same object.
func (u *unit) ptrDiff(p, q value, elem types.Type, at ast.Node) value {
	if p.v == nil || q.v == nil {
		return u.poison(u.diffType())
	}
	b := u.blk()
	d := b.Ptr.Diff(u.ptr(p.v, at), u.ptr(q.v, at))

	isOneOrZero := false
	if a, ok := asArray(elem); !ok || a.Form != types.VLA {
		sz := u.sizeof(elem, at)
		if sz == 1 || sz == 0 {
			isOneOrZero = true
		}
	}
	if !isOneOrZero {
		szVal := u.sizeofTypeVal(elem, at)
		szI := u.i64(szVal.v, at)
		d = b.I64.SDiv(d, szI)
	}
	return u.convert(value{d, types.Typ(types.LongLong)}, u.diffType(), at)
}

// logical is && and ||, which short-circuit and therefore branch.
//
// select is not usable here: §F evaluates both arms, and C guarantees the
// right operand is not evaluated at all when the left decides the answer.
// The result is materialized through a one-slot frame temporary rather than a
// block parameter, for the same reason every other object is: promotion is a
// pass's job, and doing it here would mean doing it only here.
func (u *unit) logical(e *ast.BinaryExpr) value {
	f := u.fn
	slot := u.allocaBytes(4, 4, "logical")
	rhs := f.fn.Block(u.uniq("land_rhs"))
	end := f.fn.Block(u.uniq("land_end"))

	c := u.truth(u.expr(e.X), e)
	b := u.blk()
	if c.IsZero() {
		return u.poison(types.Typ(types.Int))
	}
	b.I32.Store(b.I32.ZExtI1(c), slot)
	if e.Op == token.LAND {
		b.BrIf(c, rhs.To(), end.To())
	} else {
		b.BrIf(c, end.To(), rhs.To())
	}

	u.enter(rhs)
	c2 := u.truth(u.expr(e.Y), e)
	rb := u.blk()
	if !c2.IsZero() {
		rb.I32.Store(rb.I32.ZExtI1(c2), slot)
	}
	rb.Br(end.To())

	u.enter(end)
	eb := u.blk()
	return value{eb.I32.Load(slot), types.Typ(types.Int)}
}

// conditional is ?:.
func (u *unit) conditional(e *ast.CondExpr) value {
	f := u.fn
	rt := u.condType(e)

	var slot ir.Ptr
	if !isVoid(types.Unqualify(rt)) {
		slot = u.alloca(rt, "cond", e)
	}
	thenB := f.fn.Block(u.uniq("cond_then"))
	elseB := f.fn.Block(u.uniq("cond_else"))
	end := f.fn.Block(u.uniq("cond_end"))

	c := u.truth(u.expr(e.Cond), e)
	if c.IsZero() {
		return u.poison(rt)
	}
	u.blk().BrIf(c, thenB.To(), elseB.To())

	u.enter(thenB)
	if slot.IsZero() {
		u.discard(e.Then)
	} else {
		u.storeInto(u.expr(e.Then), slot, rt, e)
	}
	u.blk().Br(end.To())

	u.enter(elseB)
	if slot.IsZero() {
		u.discard(e.Else)
	} else {
		u.storeInto(u.expr(e.Else), slot, rt, e)
	}
	u.blk().Br(end.To())

	u.enter(end)
	if slot.IsZero() {
		return value{nil, types.Typ(types.Void)}
	}
	return u.load(lval{addr: slot, t: rt}, e)
}

// storeInto writes an expression result into a slot, copying aggregates.
func (u *unit) storeInto(v value, addr ir.Ptr, t types.Type, at ast.Node) {
	u.store(v, addr, t, at)
}

// condType is §6.5.15's result type: the usual arithmetic conversions for two
// arithmetic arms, the composite for two compatible pointers, and void where
// either arm is void.
func (u *unit) condType(e *ast.CondExpr) types.Type {
	// Both arms must be typed to decide, and typing an arm means emitting it.
	// So the type is derived syntactically where it can be and from the then
	// arm otherwise; the analyzer already rejected the combinations this
	// would get wrong.
	// §6.3.2.1p3-4: an arm that is an array or a function designator is a
	// pointer here, as everywhere but sizeof, _Alignof and unary &. Deciding
	// the result type before that conversion makes `c ? f : g` an expression
	// of function type, which has no size, no alignment, and nothing to store
	// into.
	t1 := u.decayType(u.staticType(e.Then))
	t2 := u.decayType(u.staticType(e.Else))
	switch {
	case isVoid(types.Unqualify(t1)) || isVoid(types.Unqualify(t2)):
		return types.Typ(types.Void)
	case isArith(t1) && isArith(t2):
		return u.usual(t1, t2)
	case isPtrType(t1):
		if u.isNullConstant(e.Else) {
			return t1
		}
		if p1, _ := asPointer(t1); p1 != nil {
			if p2, _ := asPointer(t2); p2 != nil && isVoid(types.Unqualify(p2.Elem)) {
				return t2
			}
		}
		return t1
	case isPtrType(t2):
		return t2
	}
	return t1
}

func (u *unit) cast(e *ast.CastExpr) value {
	to := u.typeOf(e.Type)
	if isVoid(types.Unqualify(to)) {
		u.discard(e.X)
		return value{nil, to}
	}
	return u.convert(u.expr(e.X), to, e)
}

// sizeofExpr is sizeof, in both spellings.
//
// The operand of the expression form is not evaluated (§6.5.3.4p2) — unless
// its type is variably modified, where it is, exactly once, because the size
// depends on it.
func (u *unit) sizeofExpr(e *ast.SizeofExpr) value {
	st := u.sizeType()
	var t types.Type
	if e.Type != nil {
		t = u.typeOf(e.Type)
		u.recordVLAExprs(t, e.Type)
	} else {
		t = u.staticType(e.X)
	}
	if a, ok := asArray(t); ok && a.Form == types.VLA {
		return u.vlaSizeof(t, e)
	}
	return u.intConst(u.sizeof(t, e), st)
}

// generic selects a _Generic association by type compatibility.
//
// Info records no choice, so lower makes it — which means lower's notion of
// compatibility and the analyzer's must agree, and this is the expression
// where a disagreement is silent rather than a diagnostic.
func (u *unit) generic(e *ast.GenericExpr) value {
	ct := u.staticType(e.Ctrl)
	ct = types.Unqualify(u.decay(value{nil, ct}).t)

	var dflt *ast.GenericAssoc
	for _, a := range e.Assocs {
		if a.Type == nil {
			dflt = a
			continue
		}
		if u.compatible(ct, u.typeOf(a.Type)) {
			return u.expr(a.Value)
		}
	}
	if dflt != nil {
		return u.expr(dflt.Value)
	}
	u.errorf(e, "no association in this _Generic selection matches %s", ct)
	return u.poison(types.Typ(types.Int))
}

// discard evaluates an expression for its effects and drops the result.
func (u *unit) discard(e ast.Expr) {
	if e == nil {
		return
	}
	u.expr(e)
}

// ---- arithmetic dispatch -------------------------------------------------
//
// One function per verb family, each a switch over the register type. There
// is no shared interface over the namespaces — F32NS.Add and F64NS.Add have
// different signatures by construction, which is the point of the design —
// so the switch is the price of "a mnemonic the spec does not have has no
// method". Both operands are guaranteed to share a register type by the
// conversion that produced them.

func (u *unit) emitAdd(b *ir.Block, x, y ir.Value, t types.Type, at ast.Node) ir.Value {
	if x == nil || y == nil {
		return nil
	}
	switch x := x.(type) {
	case ir.I32:
		return b.I32.Add(x, y.(ir.I32))
	case ir.I64:
		return b.I64.Add(x, y.(ir.I64))
	case ir.F32:
		return b.F32.Add(x, y.(ir.F32))
	case ir.F64:
		return b.F64.Add(x, y.(ir.F64))
	case ir.F80:
		return b.F80().Add(x, y.(ir.F80))
	case ir.F128:
		return b.F128().Add(x, y.(ir.F128))
	}
	return u.badOp(at, "+", t)
}

func (u *unit) emitSub(b *ir.Block, x, y ir.Value, t types.Type, at ast.Node) ir.Value {
	if x == nil || y == nil {
		return nil
	}
	switch x := x.(type) {
	case ir.I32:
		return b.I32.Sub(x, y.(ir.I32))
	case ir.I64:
		return b.I64.Sub(x, y.(ir.I64))
	case ir.F32:
		return b.F32.Sub(x, y.(ir.F32))
	case ir.F64:
		return b.F64.Sub(x, y.(ir.F64))
	case ir.F80:
		return b.F80().Sub(x, y.(ir.F80))
	case ir.F128:
		return b.F128().Sub(x, y.(ir.F128))
	}
	return u.badOp(at, "-", t)
}

func (u *unit) emitMul(b *ir.Block, x, y ir.Value, t types.Type, at ast.Node) ir.Value {
	if x == nil || y == nil {
		return nil
	}
	switch x := x.(type) {
	case ir.I32:
		return b.I32.Mul(x, y.(ir.I32))
	case ir.I64:
		return b.I64.Mul(x, y.(ir.I64))
	case ir.F32:
		return b.F32.Mul(x, y.(ir.F32))
	case ir.F64:
		return b.F64.Mul(x, y.(ir.F64))
	case ir.F80:
		return b.F80().Mul(x, y.(ir.F80))
	case ir.F128:
		return b.F128().Mul(x, y.(ir.F128))
	}
	return u.badOp(at, "*", t)
}

func (u *unit) emitDiv(b *ir.Block, x, y ir.Value, t types.Type, at ast.Node) ir.Value {
	if x == nil || y == nil {
		return nil
	}
	_, signed := u.model.IntBits(t)
	switch x := x.(type) {
	case ir.I32:
		if signed {
			return b.I32.SDiv(x, y.(ir.I32))
		}
		return b.I32.UDiv(x, y.(ir.I32))
	case ir.I64:
		if signed {
			return b.I64.SDiv(x, y.(ir.I64))
		}
		return b.I64.UDiv(x, y.(ir.I64))
	case ir.F32:
		return b.F32.Div(x, y.(ir.F32))
	case ir.F64:
		return b.F64.Div(x, y.(ir.F64))
	case ir.F80:
		return b.F80().Div(x, y.(ir.F80))
	case ir.F128:
		return b.F128().Div(x, y.(ir.F128))
	}
	return u.badOp(at, "/", t)
}

// emitRem has no float case: C's fmod is a library call and §L declines to
// give it a verb, so `%` on floating operands never reaches here — the
// analyzer rejected it as a constraint violation.
func (u *unit) emitRem(b *ir.Block, x, y ir.Value, t types.Type, at ast.Node) ir.Value {
	if x == nil || y == nil {
		return nil
	}
	_, signed := u.model.IntBits(t)
	switch x := x.(type) {
	case ir.I32:
		if signed {
			return b.I32.SRem(x, y.(ir.I32))
		}
		return b.I32.URem(x, y.(ir.I32))
	case ir.I64:
		if signed {
			return b.I64.SRem(x, y.(ir.I64))
		}
		return b.I64.URem(x, y.(ir.I64))
	}
	return u.badOp(at, "%", t)
}

func (u *unit) emitNeg(b *ir.Block, x ir.Value, at ast.Node) ir.Value {
	if x == nil {
		return nil
	}
	switch x := x.(type) {
	case ir.I32:
		return b.I32.Neg(x)
	case ir.I64:
		return b.I64.Neg(x)
	case ir.F32:
		return b.F32.Neg(x)
	case ir.F64:
		return b.F64.Neg(x)
	case ir.F80:
		return b.F80().Neg(x)
	case ir.F128:
		return b.F128().Neg(x)
	}
	return u.badOp(at, "unary -", nil)
}

func (u *unit) emitNot(b *ir.Block, x ir.Value, at ast.Node) ir.Value {
	if x == nil {
		return nil
	}
	switch x := x.(type) {
	case ir.I32:
		return b.I32.Not(x)
	case ir.I64:
		return b.I64.Not(x)
	}
	return u.badOp(at, "~", nil)
}

func (u *unit) emitBitwise(b *ir.Block, x, y ir.Value, op token.Kind, at ast.Node) ir.Value {
	if x == nil || y == nil {
		return nil
	}
	switch x := x.(type) {
	case ir.I32:
		switch op {
		case token.AND:
			return b.I32.And(x, y.(ir.I32))
		case token.OR:
			return b.I32.Or(x, y.(ir.I32))
		case token.XOR:
			return b.I32.Xor(x, y.(ir.I32))
		}
	case ir.I64:
		switch op {
		case token.AND:
			return b.I64.And(x, y.(ir.I64))
		case token.OR:
			return b.I64.Or(x, y.(ir.I64))
		case token.XOR:
			return b.I64.Xor(x, y.(ir.I64))
		}
	}
	return u.badOp(at, op.String(), nil)
}

// emitShift picks the arithmetic or logical right shift by the left operand's
// signedness — the one place where a signless register type needs the C type
// to decide which machine instruction is meant.
//
// §6.5.7p3 leaves a shift amount at or beyond the operand width undefined.
// VIR masks it, which is defined and is what every target does; the program
// gets a number instead of a fiction.
func (u *unit) emitShift(b *ir.Block, x, y ir.Value, t types.Type, left bool, at ast.Node) ir.Value {
	if x == nil || y == nil {
		return nil
	}
	_, signed := u.model.IntBits(t)
	switch x := x.(type) {
	case ir.I32:
		amt := y.(ir.I32)
		if left {
			return b.I32.Shl(x, amt)
		}
		if signed {
			return b.I32.SShr(x, amt)
		}
		return b.I32.UShr(x, amt)
	case ir.I64:
		amt := y.(ir.I64)
		if left {
			return b.I64.Shl(x, amt)
		}
		if signed {
			return b.I64.SShr(x, amt)
		}
		return b.I64.UShr(x, amt)
	}
	return u.badOp(at, "shift", t)
}

// emitCompare yields the i1 every comparison produces. There is no gt or ge
// in any namespace, so those swap their operands here and nowhere else.
func (u *unit) emitCompare(b *ir.Block, x, y ir.Value, t types.Type, op token.Kind, at ast.Node) ir.I1 {
	if x == nil || y == nil {
		return ir.I1{}
	}
	_, signed := u.model.IntBits(t)
	if op == token.GTR || op == token.GEQ {
		x, y = y, x
		if op == token.GTR {
			op = token.LSS
		} else {
			op = token.LEQ
		}
	}
	switch x := x.(type) {
	case ir.I32:
		y := y.(ir.I32)
		switch op {
		case token.EQL:
			return b.I32.Eq(x, y)
		case token.NEQ:
			return b.I32.Ne(x, y)
		case token.LSS:
			if signed {
				return b.I32.SLt(x, y)
			}
			return b.I32.ULt(x, y)
		case token.LEQ:
			if signed {
				return b.I32.SLe(x, y)
			}
			return b.I32.ULe(x, y)
		}
	case ir.I64:
		y := y.(ir.I64)
		switch op {
		case token.EQL:
			return b.I64.Eq(x, y)
		case token.NEQ:
			return b.I64.Ne(x, y)
		case token.LSS:
			if signed {
				return b.I64.SLt(x, y)
			}
			return b.I64.ULt(x, y)
		case token.LEQ:
			if signed {
				return b.I64.SLe(x, y)
			}
			return b.I64.ULe(x, y)
		}
	case ir.F32:
		y := y.(ir.F32)
		switch op {
		case token.EQL:
			return b.F32.Eq(x, y)
		case token.NEQ:
			return b.F32.Ne(x, y)
		case token.LSS:
			return b.F32.Lt(x, y)
		case token.LEQ:
			return b.F32.Le(x, y)
		}
	case ir.F64:
		y := y.(ir.F64)
		switch op {
		case token.EQL:
			return b.F64.Eq(x, y)
		case token.NEQ:
			return b.F64.Ne(x, y)
		case token.LSS:
			return b.F64.Lt(x, y)
		case token.LEQ:
			return b.F64.Le(x, y)
		}
	case ir.F80:
		y := y.(ir.F80)
		switch op {
		case token.EQL:
			return b.F80().Eq(x, y)
		case token.NEQ:
			return b.F80().Ne(x, y)
		case token.LSS:
			return b.F80().Lt(x, y)
		case token.LEQ:
			return b.F80().Le(x, y)
		}
	case ir.F128:
		y := y.(ir.F128)
		switch op {
		case token.EQL:
			return b.F128().Eq(x, y)
		case token.NEQ:
			return b.F128().Ne(x, y)
		case token.LSS:
			return b.F128().Lt(x, y)
		case token.LEQ:
			return b.F128().Le(x, y)
		}
	}
	u.errorf(at, "internal: no comparison verb for %s", t)
	return ir.I1{}
}

func (u *unit) badOp(at ast.Node, op string, t types.Type) ir.Value {
	if t != nil {
		u.errorf(at, "internal: no verb for %s on %s", op, t)
	} else {
		u.errorf(at, "internal: no verb for %s", op)
	}
	return nil
}

// ---- register accessors --------------------------------------------------

func (u *unit) i32(v ir.Value, at ast.Node) ir.I32 {
	if v == nil {
		return ir.I32{}
	}
	if x, ok := v.(ir.I32); ok {
		return x
	}
	u.errorf(at, "internal: expected i32")
	return ir.I32{}
}

func (u *unit) i64(v ir.Value, at ast.Node) ir.I64 {
	if v == nil {
		return ir.I64{}
	}
	if x, ok := v.(ir.I64); ok {
		return x
	}
	u.errorf(at, "internal: expected i64")
	return ir.I64{}
}

func (u *unit) ptr(v ir.Value, at ast.Node) ir.Ptr {
	if v == nil {
		return ir.Ptr{}
	}
	if x, ok := v.(ir.Ptr); ok {
		return x
	}
	u.errorf(at, "internal: expected ptr")
	return ir.Ptr{}
}

func stripParens(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// stmtExpr lowers gcc's ({ ... }).
//
// The block is emitted where it stands, and the value is the last statement's
// — which is why the last item is evaluated as an expression rather than
// discarded as a statement. Everything else about it is an ordinary block: it
// gets a scope, a declaration inside it is scoped to it, and a jump out of it
// is a jump out of a block.
func (u *unit) stmtExpr(e *ast.StmtExpr) value {
	if e.Body == nil || len(e.Body.Items) == 0 {
		return value{nil, types.Typ(types.Void)}
	}
	u.push()
	defer u.pop()

	last := len(e.Body.Items) - 1
	for i, item := range e.Body.Items {
		if i == last {
			if es, ok := item.(*ast.ExprStmt); ok {
				return u.expr(es.X)
			}
		}
		u.stmt(item)
	}
	// A block ending in anything but an expression statement has no value,
	// which is gcc's rule and not an error.
	return value{nil, types.Typ(types.Void)}
}

// isThreadLocal reports whether a symbol names a thread-local, whether this
// unit defined it or imported it.
//
// A thread-local has no address of its own. It has an offset into a block
// the calling thread owns a copy of, so reaching one is ptr.tlsaddr and
// never ptr.getaddr — getaddr would answer with the template every thread's
// copy is made from, which is a real address holding the initial value and
// belonging to no thread.
//
// A definition says it is thread-local with its domain and an import with
// its model, an import having no storage here to place. The two questions
// are what ir.PtrNS.TLSAddr itself asks, so asking them here keeps the
// caller and the check from disagreeing.
func isThreadLocal(s ir.Symbol) bool {
	switch g := s.(type) {
	case *ir.Global:
		return g.Domain() == ir.TLS
	case *ir.GlobalImport:
		return g.TLSModelAttr() != ir.NoTLSModel
	}
	return false
}
