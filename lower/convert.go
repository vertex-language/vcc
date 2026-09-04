package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// §6.3 lives here, in one place, because every other file needs it and none
// of them should each have a slightly different opinion about it.

func isArith(t types.Type) bool  { return types.IsInteger(t) || isFloatType(t) }
func isScalar(t types.Type) bool { return isArith(t) || isPtrType(t) }

func isFloatType(t types.Type) bool {
	b, ok := types.Unqualify(t).(*types.Basic)
	if !ok {
		return false
	}
	switch b.K {
	case types.Float, types.Double, types.LongDouble:
		return true
	}
	return false
}

func isComplexType(t types.Type) bool {
	b, ok := types.Unqualify(t).(*types.Basic)
	if !ok {
		return false
	}
	switch b.K {
	case types.ComplexFloat, types.ComplexDouble, types.ComplexLongDouble:
		return true
	}
	return false
}

func isPtrType(t types.Type) bool {
	_, ok := types.Unqualify(t).(*types.Pointer)
	return ok
}

func isBoolType(t types.Type) bool {
	b, ok := types.Unqualify(t).(*types.Basic)
	return ok && b.K == types.Bool
}

func asRecord(t types.Type) (*types.Record, bool) {
	r, ok := types.Unqualify(t).(*types.Record)
	return r, ok
}

func asArray(t types.Type) (*types.Array, bool) {
	a, ok := types.Unqualify(t).(*types.Array)
	return a, ok
}

func asPointer(t types.Type) (*types.Pointer, bool) {
	p, ok := types.Unqualify(t).(*types.Pointer)
	return p, ok
}

func asFunc(t types.Type) (*types.Func, bool) {
	f, ok := types.Unqualify(t).(*types.Func)
	return f, ok
}

// sizeType is size_t: the unsigned integer of pointer width. There is no
// types.Kind for it, so it is named by width rather than by spelling — which
// is also how <stddef.h>'s typedef is generated, so the two agree.
func (u *unit) sizeType() types.Type { return u.model.SizeType() }

// diffType is ptrdiff_t: the signed integer of pointer width.
func (u *unit) diffType() types.Type { return u.model.PtrDiffType() }

// promote applies §6.3.1.1p2: anything of integer rank below int becomes int,
// or unsigned int where int cannot hold every value. Everything else is
// unchanged.
func (u *unit) promote(t types.Type) types.Type {
	t = types.Unqualify(t)
	if !types.IsInteger(t) {
		return t
	}
	intBits, _ := u.model.IntBits(types.Typ(types.Int))
	bits, signed := u.model.IntBits(t)
	if e, isEnum := t.(*types.Enum); isEnum {
		// An enumerated type promotes to the type it is compatible with:
		// int, unless an enumerator too large for one widened it.
		return types.Typ(e.Underlying())
	}
	if bits > intBits || (bits == intBits && !signed && !isSmall(t)) {
		return t
	}
	if bits < intBits || signed {
		return types.Typ(types.Int)
	}
	return types.Typ(types.UInt)
}

func isSmall(t types.Type) bool {
	b, ok := types.Unqualify(t).(*types.Basic)
	if !ok {
		return false
	}
	switch b.K {
	case types.Bool, types.Char, types.SChar, types.UChar, types.Short, types.UShort:
		return true
	}
	return false
}

// usual applies §6.3.1.8's usual arithmetic conversions and returns the common
// type. Callers convert both operands to it.
func (u *unit) usual(a, b types.Type) types.Type {
	a, b = types.Unqualify(a), types.Unqualify(b)
	if isFloatType(a) || isFloatType(b) {
		return u.widerFloat(a, b)
	}
	a, b = u.promote(a), u.promote(b)
	if a == b {
		return a
	}
	ab, as := u.model.IntBits(a)
	bb, bs := u.model.IntBits(b)
	switch {
	case as == bs:
		if ab >= bb {
			return a
		}
		return b
	case !as && ab >= bb:
		return a
	case !bs && bb >= ab:
		return b
	case as && ab > bb:
		return a
	case bs && bb > ab:
		return b
	}
	// Equal width, mixed signedness: the unsigned counterpart of the signed
	// type wins.
	if as {
		return unsignedOf(a)
	}
	return unsignedOf(b)
}

func (u *unit) widerFloat(a, b types.Type) types.Type {
	rank := func(t types.Type) int {
		bt, ok := types.Unqualify(t).(*types.Basic)
		if !ok {
			return 0
		}
		switch bt.K {
		case types.LongDouble:
			return 3
		case types.Double:
			return 2
		case types.Float:
			return 1
		}
		return 0
	}
	if rank(a) >= rank(b) {
		if rank(a) > 0 {
			return a
		}
		return b
	}
	return b
}

func unsignedOf(t types.Type) types.Type {
	b, ok := types.Unqualify(t).(*types.Basic)
	if !ok {
		return t
	}
	switch b.K {
	case types.Char, types.SChar:
		return types.Typ(types.UChar)
	case types.Short:
		return types.Typ(types.UShort)
	case types.Int:
		return types.Typ(types.UInt)
	case types.Long:
		return types.Typ(types.ULong)
	case types.LongLong:
		return types.Typ(types.ULongLong)
	}
	return t
}

// decay applies §6.3.2.1p3–4: an array becomes a pointer to its first element
// and a function becomes a pointer to itself. The value it is applied to must
// already be an address, which is what expr guarantees for both.
func (u *unit) decay(v value) value {
	switch t := types.Unqualify(v.t).(type) {
	case *types.Array:
		return value{v.v, &types.Pointer{Elem: t.Elem}}
	case *types.Func:
		return value{v.v, &types.Pointer{Elem: t}}
	}
	return v
}

// convert emits the conversion of v to type to, per §6.3.
//
// It is total over the conversions C admits and reports on the ones it does
// not, rather than emitting something plausible: a wrong conversion is the
// class of bug that survives every test that does not happen to look at the
// high bits.
func (u *unit) convert(v value, to types.Type, at ast.Node) value {
	b := u.blk()
	from := types.Unqualify(v.t)
	dst := types.Unqualify(to)

	if isVoid(dst) {
		return value{nil, dst}
	}
	if v.v == nil {
		return value{nil, dst}
	}
	if isComplexType(from) || isComplexType(dst) {
		u.errorf(at, "_Complex is not yet lowered")
		return u.poison(dst)
	}
	if isBoolType(dst) {
		return value{u.toBoolInt(v, at), dst}
	}
	if _, ok := asRecord(dst); ok {
		return value{v.v, dst} // an aggregate "conversion" is an identity
	}

	_, sok := u.types.regType(from)
	dr, dok := u.types.regType(dst)
	if !sok || !dok {
		// A 128-bit operand names itself; without that this reports
		// "cannot convert unsigned __int128 to unsigned __int128",
		// which describes a conversion that is not the problem.
		if !u.unsupported128(at, from) && !u.unsupported128(at, dst) {
			u.errorf(at, "cannot convert %s to %s", v.t, to)
		}
		return u.poison(dst)
	}

	switch {
	case types.IsInteger(from) && types.IsInteger(dst):
		return value{u.intToInt(b, v.v, from, dst), dst}

	case types.IsInteger(from) && isFloatType(dst):
		return value{u.intToFloat(b, v.v, from, dr, at), dst}

	case isFloatType(from) && types.IsInteger(dst):
		return value{u.floatToInt(b, v.v, dst, dr, at), dst}

	case isFloatType(from) && isFloatType(dst):
		return value{u.floatToFloat(b, v.v, dr, at), dst}

	case isPtrType(from) && isPtrType(dst):
		return value{v.v, dst}

	case isPtrType(from) && types.IsInteger(dst):
		i := b.I64.FromPtr(u.ptr(v.v, at))
		return value{u.intToInt(b, i, types.Typ(types.ULongLong), dst), dst}

	case types.IsInteger(from) && isPtrType(dst):
		i := u.intToInt(b, v.v, from, types.Typ(types.ULongLong))
		return value{b.Ptr.FromI64(u.i64(i, at)), dst}
	}
	if !u.unsupported128(at, from) && !u.unsupported128(at, dst) {
		u.errorf(at, "cannot convert %s to %s", v.t, to)
	}
	return u.poison(dst)
}

// intToInt handles the four integer cases: same register and same width is a
// no-op; same register and narrower destination is a mask or a
// shift-pair; different registers are the widening and wrapping verbs.
//
// Narrowing within i32 matters more than it looks. `(char)x` is a value the
// program may compare, pass, or store into a wider object, and a store8 that
// happens to truncate on the way out does not help any of those.
func (u *unit) intToInt(b *ir.Block, v ir.Value, from, to types.Type) ir.Value {
	if v == nil {
		return nil
	}
	sb, ss := u.model.IntBits(from)
	db, ds := u.model.IntBits(to)
	sr, _ := u.types.regType(from)
	dr, _ := u.types.regType(to)

	if sr == ir.TypeI32 && dr == ir.TypeI64 {
		x := v.(ir.I32)
		if ss {
			v = b.I64.SExtI32(x)
		} else {
			v = b.I64.ZExtI32(x)
		}
		sr, sb = ir.TypeI64, 64
	} else if sr == ir.TypeI64 && dr == ir.TypeI32 {
		v = b.I32.WrapI64(v.(ir.I64))
		sr, sb = ir.TypeI32, 32
	}
	// A type narrower than its register is held extended to the register's
	// width, and which extension it is depends on the type's signedness. So
	// the destination's representation is already correct only when it is at
	// least as wide as the source *and* agrees about the sign; a same-width
	// change of signedness is not a no-op. (unsigned char)c on a signed char
	// holding -1 is 255, and returning the sign-extended 0xffffffff makes it
	// -1 in every context that reads the register rather than a byte of
	// memory.
	regBits := int64(0)
	switch dr {
	case ir.TypeI32:
		regBits = 32
	case ir.TypeI64:
		regBits = 64
	}
	if regBits == 0 {
		// Not one of the general-purpose widths — i1, handled by the caller.
		if db >= sb {
			return v
		}
		return u.narrow(b, v, db, ds)
	}
	if db >= regBits || (db >= sb && ds == ss) {
		return v
	}
	return u.narrow(b, v, db, ds)
}

// narrow reduces a register value to bits significant bits, sign- or
// zero-extended back to the register width.
func (u *unit) narrow(b *ir.Block, v ir.Value, bits int64, signed bool) ir.Value {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case ir.I32:
		if bits >= 32 {
			return x
		}
		if signed {
			s := b.I32.Const(32 - bits)
			return b.I32.SShr(b.I32.Shl(x, s), s)
		}
		return b.I32.And(x, b.I32.Const(int64(1)<<uint(bits)-1))
	case ir.I64:
		if bits >= 64 {
			return x
		}
		if signed {
			s := b.I64.Const(64 - bits)
			return b.I64.SShr(b.I64.Shl(x, s), s)
		}
		return b.I64.And(x, b.I64.Const(int64(1)<<uint(bits)-1))
	}
	return v
}

func (u *unit) intToFloat(b *ir.Block, v ir.Value, from types.Type, dr ir.RegType, at ast.Node) ir.Value {
	if v == nil {
		return nil
	}
	_, signed := u.model.IntBits(from)
	switch x := v.(type) {
	case ir.I32:
		switch dr {
		case ir.TypeF32:
			if signed {
				return b.F32.SCvtI32(x)
			}
			return b.F32.UCvtI32(x)
		case ir.TypeF64:
			if signed {
				return b.F64.SCvtI32(x)
			}
			return b.F64.UCvtI32(x)
		case ir.TypeF80:
			if signed {
				return b.F80().SCvtI32(x)
			}
			return b.F80().UCvtI32(x)
		case ir.TypeF128:
			if signed {
				return b.F128().SCvtI32(x)
			}
			return b.F128().UCvtI32(x)
		}
	case ir.I64:
		switch dr {
		case ir.TypeF32:
			if signed {
				return b.F32.SCvtI64(x)
			}
			return b.F32.UCvtI64(x)
		case ir.TypeF64:
			if signed {
				return b.F64.SCvtI64(x)
			}
			return b.F64.UCvtI64(x)
		case ir.TypeF80:
			if signed {
				return b.F80().SCvtI64(x)
			}
			return b.F80().UCvtI64(x)
		case ir.TypeF128:
			if signed {
				return b.F128().SCvtI64(x)
			}
			return b.F128().UCvtI64(x)
		}
	}
	u.errorf(at, "internal: no int-to-float verb for %s", dr)
	return nil
}

// floatToInt uses the trapping verbs, never the saturating ones.
//
// §6.3.1.4 makes an out-of-range float-to-int conversion undefined, and VIR
// has no undefined. The _sat_ forms exist for a frontend that has proven the
// range or chosen to define the overflow; vcc has done neither, so the
// conversion traps and the program says so instead of continuing with a
// number nobody chose.
func (u *unit) floatToInt(b *ir.Block, v ir.Value, to types.Type, dr ir.RegType, at ast.Node) ir.Value {
	if v == nil {
		return nil
	}
	_, signed := u.model.IntBits(to)
	var out ir.Value
	switch x := v.(type) {
	case ir.F32:
		if dr == ir.TypeI64 {
			if signed {
				out = b.I64.SCvtF32(x)
			} else {
				out = b.I64.UCvtF32(x)
			}
		} else if signed {
			out = b.I32.SCvtF32(x)
		} else {
			out = b.I32.UCvtF32(x)
		}
	case ir.F64:
		if dr == ir.TypeI64 {
			if signed {
				out = b.I64.SCvtF64(x)
			} else {
				out = b.I64.UCvtF64(x)
			}
		} else if signed {
			out = b.I32.SCvtF64(x)
		} else {
			out = b.I32.UCvtF64(x)
		}
	case ir.F80:
		if dr == ir.TypeI64 {
			if signed {
				out = b.I64.SCvtF80(x)
			} else {
				out = b.I64.UCvtF80(x)
			}
		} else if signed {
			out = b.I32.SCvtF80(x)
		} else {
			out = b.I32.UCvtF80(x)
		}
	case ir.F128:
		if dr == ir.TypeI64 {
			if signed {
				out = b.I64.SCvtF128(x)
			} else {
				out = b.I64.UCvtF128(x)
			}
		} else if signed {
			out = b.I32.SCvtF128(x)
		} else {
			out = b.I32.UCvtF128(x)
		}
	default:
		u.errorf(at, "internal: no float-to-int verb")
		return nil
	}
	bits, s := u.model.IntBits(to)
	return u.narrow(b, out, bits, s)
}

func (u *unit) floatToFloat(b *ir.Block, v ir.Value, dr ir.RegType, at ast.Node) ir.Value {
	if v == nil {
		return nil
	}
	if v.RegType() == dr {
		return v
	}
	switch x := v.(type) {
	case ir.F32:
		switch dr {
		case ir.TypeF64:
			return b.F64.FCvtF32(x)
		case ir.TypeF80:
			return b.F80().FCvtF32(x)
		case ir.TypeF128:
			return b.F128().FCvtF32(x)
		}
	case ir.F64:
		switch dr {
		case ir.TypeF32:
			return b.F32.FCvtF64(x)
		case ir.TypeF80:
			return b.F80().FCvtF64(x)
		case ir.TypeF128:
			return b.F128().FCvtF64(x)
		}
	case ir.F80:
		switch dr {
		case ir.TypeF32:
			return b.F32.FCvtF80(x)
		case ir.TypeF64:
			return b.F64.FCvtF80(x)
		case ir.TypeF128:
			return b.F128().FCvtF80(x)
		}
	case ir.F128:
		switch dr {
		case ir.TypeF32:
			return b.F32.FCvtF128(x)
		case ir.TypeF64:
			return b.F64.FCvtF128(x)
		case ir.TypeF80:
			return b.F80().FCvtF128(x)
		}
	}
	u.errorf(at, "internal: no float-width verb to %s", dr)
	return nil
}

// toBoolInt is §6.3.1.2: a scalar becomes 0 or 1 by comparison against zero.
// The result lives in i32 like every other narrow integer, so _Bool needs no
// register type of its own.
func (u *unit) toBoolInt(v value, at ast.Node) ir.Value {
	b := u.blk()
	t := u.truth(v, at)
	if t.IsZero() {
		return nil
	}
	return b.I32.ZExtI1(t)
}

// truth reduces a scalar to the i1 every branch and logical operator wants.
func (u *unit) truth(v value, at ast.Node) ir.I1 {
	b := u.blk()
	v = u.decay(v)
	if v.v == nil {
		return ir.I1{}
	}
	switch x := v.v.(type) {
	case ir.I1:
		return x
	case ir.I32:
		return b.I32.Ne(x, b.I32.Const(0))
	case ir.I64:
		return b.I64.Ne(x, b.I64.Const(0))
	case ir.F32:
		return b.F32.Ne(x, b.F32.Const(0))
	case ir.F64:
		return b.F64.Ne(x, b.F64.Const(0))
	case ir.F80:
		return b.F80().Ne(x, b.F80().Const(0))
	case ir.F128:
		return b.F128().Ne(x, b.F128().Const(0))
	case ir.Ptr:
		return b.Ptr.Ne(x, b.Ptr.Const())
	}
	u.errorf(at, "value of type %s is not scalar", v.t)
	return ir.I1{}
}

// compatible is §6.2.7 to the depth _Generic and assignment need.
//
// Records compare by identity, which is what types documents: two *Record
// values are the same type iff they are the same pointer. Everything else is
// structural, with qualifiers significant only where §6.7.3p10 says they are.
func (u *unit) compatible(a, b types.Type) bool {
	if types.QualsOf(a) != types.QualsOf(b) {
		return false
	}
	x, y := types.Unqualify(a), types.Unqualify(b)
	if x == y {
		return true
	}
	switch x := x.(type) {
	case *types.Basic:
		y, ok := y.(*types.Basic)
		return ok && x.K == y.K
	case *types.Enum:
		// §6.7.2.2p4: an enum is compatible with its implementation type.
		if yb, ok := y.(*types.Basic); ok {
			return yb.K == x.Underlying()
		}
		return x == y
	case *types.Pointer:
		y, ok := y.(*types.Pointer)
		return ok && u.compatible(x.Elem, y.Elem)
	case *types.Array:
		y, ok := y.(*types.Array)
		if !ok || !u.compatible(x.Elem, y.Elem) {
			return false
		}
		if x.Form == types.FixedArray && y.Form == types.FixedArray {
			return x.Len == y.Len
		}
		return true
	case *types.Func:
		y, ok := y.(*types.Func)
		if !ok || !u.compatible(x.Ret, y.Ret) || x.Variadic != y.Variadic {
			return false
		}
		if !x.Proto || !y.Proto {
			return true // §6.7.6.3p15
		}
		if len(x.Params) != len(y.Params) {
			return false
		}
		for i := range x.Params {
			if !u.compatible(types.AdjustParam(x.Params[i].Type), types.AdjustParam(y.Params[i].Type)) {
				return false
			}
		}
		return true
	}
	return false
}

// isNullConstant is §6.3.2.3p3: an integer constant expression with value 0,
// or such an expression cast to void *.
func (u *unit) isNullConstant(e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.CastExpr:
			t := u.typeOf(x.Type)
			p, ok := asPointer(t)
			if !ok || !isVoid(types.Unqualify(p.Elem)) {
				return false
			}
			e = x.X
		default:
			v, ok := u.constOf(e)
			if ok {
				return v == 0
			}
			lit, ok := e.(*ast.BasicLit)
			if !ok || lit.Kind != token.INT_LIT {
				return false
			}
			iv := u.decodeInt(lit)
			return iv.Value == 0
		}
	}
}
