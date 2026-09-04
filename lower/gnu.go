package lower

import (
	"math"
	"math/bits"
	"strings"

	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/analyzer"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// The builtins gcc and clang provide, which C written for either one uses and
// a platform's own headers reach for first.
//
// Each is here because it cannot be written in C. __builtin_fabs is the
// machine's absolute value rather than a call to libm, __builtin_inf names a
// value no literal spells, __builtin_constant_p asks a question only the
// compiler can answer, and the byte swaps are one instruction that no
// expression reliably becomes.
//
// A builtin whose meaning is a library call is not here: __builtin_memcpy and
// kin lower to the calls they name, and the header that declares them is the
// library's business.

// gnuBuiltin lowers a call to one of the GCC builtins. The bool is false for a
// name this does not implement, which the caller then treats as an ordinary
// call.
func (u *unit) gnuBuiltin(name string, e *ast.CallExpr) (value, bool) {
	switch name {
	// —— absolute value ——
	case "__builtin_fabsf":
		return u.floatUnary(e, types.Typ(types.Float), floatAbs), true
	case "__builtin_fabs":
		return u.floatUnary(e, types.Typ(types.Double), floatAbs), true
	case "__builtin_fabsl":
		return u.floatUnary(e, types.Typ(types.LongDouble), floatAbs), true

	// —— the values no literal spells ——
	case "__builtin_inff", "__builtin_huge_valf":
		return u.floatValue(math.Inf(1), types.Typ(types.Float), e), true
	case "__builtin_inf", "__builtin_huge_val":
		return u.floatValue(math.Inf(1), types.Typ(types.Double), e), true
	case "__builtin_infl", "__builtin_huge_vall":
		return u.floatValue(math.Inf(1), types.Typ(types.LongDouble), e), true
	case "__builtin_nanf":
		return u.floatValue(math.NaN(), types.Typ(types.Float), e), true
	case "__builtin_nan":
		return u.floatValue(math.NaN(), types.Typ(types.Double), e), true
	case "__builtin_nanl":
		return u.floatValue(math.NaN(), types.Typ(types.LongDouble), e), true

	// —— the quiet comparisons ——
	//
	// §7.12.14 has these raise no invalid-operation exception on a NaN, which
	// is the only way they differ from the ordinary operators. vcc models no
	// floating-point exception state, so the ordinary operator is exact.
	case "__builtin_isgreater":
		return u.floatCompare(e, token.GTR), true
	case "__builtin_isgreaterequal":
		return u.floatCompare(e, token.GEQ), true
	case "__builtin_isless":
		return u.floatCompare(e, token.LSS), true
	case "__builtin_islessequal":
		return u.floatCompare(e, token.LEQ), true
	case "__builtin_islessgreater":
		return u.floatLessGreater(e), true
	case "__builtin_isunordered":
		return u.floatUnordered(e), true

	// —— byte swaps ——
	case "__builtin_bswap16":
		return u.byteSwap(e, 16), true
	case "__builtin_bswap32":
		return u.byteSwap(e, 32), true
	case "__builtin_bswap64":
		return u.byteSwap(e, 64), true

	// —— questions about the program rather than about a value ——
	case "__builtin_constant_p":
		return u.constantP(e), true
	case "__builtin_object_size":
		return u.objectSize(e), true

	// —— bit counting ——
	case "__builtin_clz", "__builtin_clzl", "__builtin_clzll":
		return u.bitCount(e, "clz"), true
	case "__builtin_ctz", "__builtin_ctzl", "__builtin_ctzll":
		return u.bitCount(e, "ctz"), true
	case "__builtin_popcount", "__builtin_popcountl", "__builtin_popcountll":
		return u.bitCount(e, "popcount"), true

	// —— the fortified library calls ——
	//
	// clang answers __has_builtin for these, so a platform's <string.h>
	// uses them whenever _FORTIFY_SOURCE is on — and a .i file produced by
	// clang contains them whether or not the compiler reading it back knows
	// them. Each is the plain library call plus a size the caller believes
	// the destination has. vcc drops the size, which is what the unfortified
	// build does and what __builtin_object_size answering "unknown" already
	// selects everywhere else.
	case "__builtin___memcpy_chk", "__builtin___memmove_chk",
		"__builtin___memset_chk", "__builtin___memccpy_chk",
		"__builtin___mempcpy_chk", "__builtin___strcpy_chk",
		"__builtin___stpcpy_chk", "__builtin___strncpy_chk",
		"__builtin___stpncpy_chk", "__builtin___strcat_chk",
		"__builtin___strncat_chk", "__builtin___strlcpy_chk",
		"__builtin___strlcat_chk":
		return u.chkCall(name, e, 1, 0), true
	case "__builtin___sprintf_chk", "__builtin___vsprintf_chk",
		"__builtin___printf_chk", "__builtin___vprintf_chk",
		"__builtin___fprintf_chk", "__builtin___vfprintf_chk":
		// (dst, flag, objsize, fmt, …) — two extra after the destination.
		return u.chkCall(name, e, 0, 2), true
	case "__builtin___snprintf_chk", "__builtin___vsnprintf_chk":
		// (dst, len, flag, objsize, fmt, …) — two extra after two kept.
		return u.chkCall(name, e, 0, 2), true

	// —— libc under another name ——
	//
	// gcc documents these as equivalent to the library function, and a
	// freestanding program that calls one is asking for the library
	// function. Lowering them as the call is what both of the compilers vcc
	// follows do when they do not expand the operation inline.
	case "__builtin_memcpy", "__builtin_memmove", "__builtin_memset",
		"__builtin_memcmp", "__builtin_strlen", "__builtin_strcpy",
		"__builtin_strncpy", "__builtin_strcmp", "__builtin_strncmp",
		"__builtin_abort", "__builtin_exit", "__builtin_malloc",
		"__builtin_free", "__builtin_calloc", "__builtin_realloc",
		"__builtin_printf", "__builtin_puts",
		"__builtin_sqrt", "__builtin_sqrtf", "__builtin_pow", "__builtin_powf":
		return u.builtinAsCall(name, e), true

	// The integer absolute values are two instructions, and gcc answers them
	// without <stdlib.h> — a program that calls one has not necessarily
	// included anything.
	case "__builtin_abs":
		return u.intAbs(e, types.Typ(types.Int)), true
	case "__builtin_labs":
		return u.intAbs(e, types.Typ(types.Long)), true
	case "__builtin_llabs":
		return u.intAbs(e, types.Typ(types.LongLong)), true

	// alloca is not a library function. gcc and clang both answer it in the
	// compiler, because the storage has to come from the caller's own frame
	// — a call could only return storage from its own, which is gone. §L's
	// ptr.alloca is the same instruction a VLA uses, and the same rule about
	// when it is released applies.
	case "__builtin_alloca", "alloca":
		return u.allocaBuiltin(e), true

	// —— the legacy atomics ——
	//
	// gcc's __sync_* family predates <stdatomic.h> and predates __atomic_*,
	// and the C that uses it is the C written before either. It is reachable
	// because vcc says __GNUC__ (see preprocessor/predefined.go, which says
	// why): SQLite's memory barrier is __sync_synchronize() for any compiler
	// that does, so claiming the name and not answering it is a program that
	// compiles everywhere except here.
	//
	// They are the operations vcc already performs for _Atomic, and they
	// cannot be written in C for the reason nothing else in this file can.
	// Every one is sequentially consistent, which is what the family
	// documents — it is specified as a full barrier, and the weaker
	// __atomic_* family is where an order can be asked for.
	//
	// __sync_*_nand_* is absent: nand is not one of the machine's
	// read-modify-writes, so it is a compare-and-swap loop rather than an
	// instruction, and no header vcc has met reaches for it.
	case "__sync_synchronize":
		u.blk().Fence(ir.SeqCst)
		return value{nil, types.Typ(types.Void)}, true
	case "__sync_fetch_and_add":
		return u.syncFetch(e, token.ADD, false), true
	case "__sync_fetch_and_sub":
		return u.syncFetch(e, token.SUB, false), true
	case "__sync_fetch_and_and":
		return u.syncFetch(e, token.AND, false), true
	case "__sync_fetch_and_or":
		return u.syncFetch(e, token.OR, false), true
	case "__sync_fetch_and_xor":
		return u.syncFetch(e, token.XOR, false), true
	case "__sync_add_and_fetch":
		return u.syncFetch(e, token.ADD, true), true
	case "__sync_sub_and_fetch":
		return u.syncFetch(e, token.SUB, true), true
	case "__sync_and_and_fetch":
		return u.syncFetch(e, token.AND, true), true
	case "__sync_or_and_fetch":
		return u.syncFetch(e, token.OR, true), true
	case "__sync_xor_and_fetch":
		return u.syncFetch(e, token.XOR, true), true
	case "__sync_val_compare_and_swap":
		return u.syncCas(e, false), true
	case "__sync_bool_compare_and_swap":
		return u.syncCas(e, true), true
	case "__sync_lock_test_and_set":
		// An exchange, documented as an acquire barrier rather than a full
		// one. vcc emits the full one, which is stronger and correct.
		return u.syncFetch(e, token.ASSIGN, false), true
	case "__sync_lock_release":
		return u.syncRelease(e), true
	}

	// The analyzer lets every reserved builtin spelling through as declared,
	// because a program cannot declare one itself and the list of the ones
	// this compiler answers belongs here rather than there. So an
	// unimplemented one arrives as a call to a name nothing defines, and
	// this is where it is named — rather than at the IR, which would see a
	// call with no callee and report a fault of its own.
	if analyzer.IsCompilerBuiltin(name) {
		u.errorf(e, "%s is not implemented by vcc", name)
		return u.poison(types.Typ(types.Int)), true
	}
	return value{}, false
}

// allocaBuiltin lowers alloca: storage from the caller's frame, released at
// function exit.
//
// gcc releases it at function exit too, which is what makes alloca in a loop
// a leak rather than a reuse. The warning a VLA gets for the same reason is
// not repeated here: a program that writes alloca has asked for exactly this.
func (u *unit) allocaBuiltin(e *ast.CallExpr) value {
	ret := &types.Pointer{Elem: types.Typ(types.Void)}
	if len(e.Args) != 1 {
		u.errorf(e, "alloca takes one argument, the size in bytes")
		return u.poison(ret)
	}
	n := u.convert(u.expr(e.Args[0]), types.Typ(types.ULongLong), e)
	size, ok := n.v.(ir.I64)
	if !ok {
		u.errorf(e, "alloca's size is not an integer")
		return u.poison(ret)
	}
	// No stack mark and no warning. A VLA gets one because C scopes its
	// storage to the block and this compiler does not; alloca is scoped to
	// the *function* by its own definition, so releasing it at function exit
	// is not an approximation of anything — it is the specified behaviour,
	// and gcc's.
	b := u.blk()
	return value{b.Ptr.Alloca(size, 16), ret}
}

// floatAbs and the rest of the unary float operations, named so floatUnary
// can take one.
type floatOp int

const floatAbs floatOp = iota

// floatUnary lowers a one-argument float builtin at a fixed width.
func (u *unit) floatUnary(e *ast.CallExpr, t types.Type, op floatOp) value {
	if len(e.Args) < 1 {
		u.errorf(e, "this builtin takes one argument")
		return u.poison(t)
	}
	v := u.convert(u.expr(e.Args[0]), t, e)
	if v.v == nil {
		return u.poison(t)
	}
	b := u.blk()
	switch x := v.v.(type) {
	case ir.F32:
		return value{b.F32.Abs(x), t}
	case ir.F64:
		return value{b.F64.Abs(x), t}
	case ir.F80:
		return value{b.F80().Abs(x), t}
	case ir.F128:
		return value{b.F128().Abs(x), t}
	}
	u.errorf(e, "internal: no absolute value for %s", t)
	return u.poison(t)
}

// floatValue materializes a constant of a given float type.
func (u *unit) floatValue(f float64, t types.Type, at ast.Node) value {
	b := u.blk()
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
	u.errorf(at, "internal: %s has no float register type", t)
	return u.poison(t)
}

// floatCompare lowers the quiet comparisons, which compare.
func (u *unit) floatCompare(e *ast.CallExpr, op token.Kind) value {
	intT := types.Typ(types.Int)
	if len(e.Args) < 2 {
		u.errorf(e, "this builtin takes two arguments")
		return u.poison(intT)
	}
	x, y := u.expr(e.Args[0]), u.expr(e.Args[1])
	ct := u.usual(x.t, y.t)
	b := u.blk()
	c := u.emitCompare(b, u.convert(x, ct, e).v, u.convert(y, ct, e).v, ct, op, e)
	if c.IsZero() {
		return u.poison(intT)
	}
	return value{b.I32.ZExtI1(c), intT}
}

// floatLessGreater is x < y || x > y, which is false for a NaN either side.
func (u *unit) floatLessGreater(e *ast.CallExpr) value {
	intT := types.Typ(types.Int)
	if len(e.Args) < 2 {
		u.errorf(e, "this builtin takes two arguments")
		return u.poison(intT)
	}
	x, y := u.expr(e.Args[0]), u.expr(e.Args[1])
	ct := u.usual(x.t, y.t)
	cx, cy := u.convert(x, ct, e), u.convert(y, ct, e)
	b := u.blk()
	lt := u.emitCompare(b, cx.v, cy.v, ct, token.LSS, e)
	gt := u.emitCompare(b, cx.v, cy.v, ct, token.GTR, e)
	if lt.IsZero() || gt.IsZero() {
		return u.poison(intT)
	}
	return value{b.I32.ZExtI1(b.I1.Or(lt, gt)), intT}
}

// floatUnordered is true when either operand is a NaN, which is the one thing
// a value that compares unequal to itself tells you.
func (u *unit) floatUnordered(e *ast.CallExpr) value {
	intT := types.Typ(types.Int)
	if len(e.Args) < 2 {
		u.errorf(e, "this builtin takes two arguments")
		return u.poison(intT)
	}
	x, y := u.expr(e.Args[0]), u.expr(e.Args[1])
	ct := u.usual(x.t, y.t)
	cx, cy := u.convert(x, ct, e), u.convert(y, ct, e)
	b := u.blk()
	nx := u.emitCompare(b, cx.v, cx.v, ct, token.NEQ, e)
	ny := u.emitCompare(b, cy.v, cy.v, ct, token.NEQ, e)
	if nx.IsZero() || ny.IsZero() {
		return u.poison(intT)
	}
	return value{b.I32.ZExtI1(b.I1.Or(nx, ny)), intT}
}

// byteSwap reverses the bytes of an integer.
//
// Built from shifts and masks rather than from an ir verb: there is no swap
// in §5, and a backend that has the instruction is free to recognize the
// pattern. What matters here is that the value is right at every width.
func (u *unit) byteSwap(e *ast.CallExpr, width int) value {
	t := types.Typ(types.UInt)
	if width == 64 {
		t = types.Typ(types.ULongLong)
	} else if width == 16 {
		t = types.Typ(types.UShort)
	}
	if len(e.Args) < 1 {
		u.errorf(e, "__builtin_bswap takes one argument")
		return u.poison(t)
	}
	// The swap is done at the width it names, in a register wide enough.
	work := types.Typ(types.UInt)
	if width == 64 {
		work = types.Typ(types.ULongLong)
	}
	v := u.convert(u.expr(e.Args[0]), work, e)
	if v.v == nil {
		return u.poison(t)
	}
	b := u.blk()
	var acc ir.Value
	for i := 0; i < width/8; i++ {
		// byte i of the input becomes byte (width/8-1-i) of the result.
		from, to := int64(i*8), int64((width/8-1-i)*8)
		part := u.shiftMaskByte(b, v.v, from, to, work, e)
		if part == nil {
			return u.poison(t)
		}
		if acc == nil {
			acc = part
		} else {
			acc = u.emitBitwise(b, acc, part, token.OR, e)
		}
	}
	return u.convert(value{acc, work}, t, e)
}

// shiftMaskByte moves one byte of v from bit offset `from` to bit offset
// `to`, masking everything else off.
func (u *unit) shiftMaskByte(b *ir.Block, v ir.Value, from, to int64, t types.Type, at ast.Node) ir.Value {
	// The shift amount is in the value's own register, not in an int:
	// emitShift takes both operands from one namespace, and a 64-bit shift
	// by an i32 is a type assertion that fails.
	x := v
	if from > 0 {
		x = u.emitShift(b, x, u.intConst(from, t).v, t, false, at)
	}
	x = u.emitBitwise(b, x, u.intConst(0xff, t).v, token.AND, at)
	if to > 0 {
		x = u.emitShift(b, x, u.intConst(to, t).v, t, true, at)
	}
	return x
}

// constantP answers §gcc's __builtin_constant_p: 1 when the argument folds to
// a constant here, 0 when it does not.
//
// Answering 0 is always allowed — gcc documents the result as false for
// anything the compiler cannot prove constant — so what this affects is which
// arm of a header's conditional is taken, never whether the program is
// correct. Darwin's byte swaps are written around it.
func (u *unit) constantP(e *ast.CallExpr) value {
	intT := types.Typ(types.Int)
	if len(e.Args) < 1 {
		return u.intConst(0, intT)
	}
	if _, ok := u.constInt(e.Args[0]); ok {
		return u.intConst(1, intT)
	}
	// The argument is not evaluated: gcc guarantees it has no side effects,
	// and a header that wrote one there is relying on that.
	return u.intConst(0, intT)
}

// objectSize answers __builtin_object_size, which asks how many bytes are
// reachable from a pointer.
//
// The answer is "unknown", which the interface spells as all-ones for types 0
// and 1 and as zero for 2 and 3. Every use of it in a header is a fortified
// library call choosing between a checked and an unchecked form, and
// "unknown" selects the unchecked one — the same code an unfortified build
// gets.
func (u *unit) objectSize(e *ast.CallExpr) value {
	st := u.sizeType()
	kind := int64(0)
	if len(e.Args) > 1 {
		if v, ok := u.constInt(e.Args[1]); ok {
			kind = v
		}
	}
	if kind >= 2 {
		return u.intConst(0, st)
	}
	return u.intConst(-1, st)
}

// bitCount lowers the leading-zero, trailing-zero and population-count
// builtins.
//
// gcc leaves clz and ctz undefined for a zero argument, which is what lets
// them be one instruction. This computes them without one, so it answers the
// width for zero rather than leaving the result unspecified — a definition
// where there was none, which no program relying on the documented behaviour
// can tell apart.
func (u *unit) bitCount(e *ast.CallExpr, op string) value {
	intT := types.Typ(types.Int)
	if len(e.Args) < 1 {
		u.errorf(e, "this builtin takes one argument")
		return u.poison(intT)
	}
	v := u.expr(e.Args[0])
	width, _ := u.model.IntBits(u.promote(v.t))
	if n, ok := u.constInt(e.Args[0]); ok {
		x := uint64(n)
		if width < 64 {
			x &= (uint64(1) << uint(width)) - 1
		}
		switch op {
		case "clz":
			return u.intConst(int64(bits.LeadingZeros64(x)-(64-int(width))), intT)
		case "ctz":
			if x == 0 {
				return u.intConst(width, intT)
			}
			return u.intConst(int64(bits.TrailingZeros64(x)), intT)
		default:
			return u.intConst(int64(bits.OnesCount64(x)), intT)
		}
	}
	return u.bitCountRuntime(e, op, v, width)
}

// builtinAsCall lowers a builtin that names a library function to a call to
// that function.
func (u *unit) builtinAsCall(name string, e *ast.CallExpr) value {
	lib := name[len("__builtin_"):]
	o := u.lookup(lib)
	if o == nil {
		u.errorf(e, "%s needs %s to be declared; include the header that declares it", name, lib)
		return u.poison(types.Typ(types.Int))
	}
	callee := u.callable(o)
	if callee == nil {
		u.errorf(e, "%s: %s is not a function", name, lib)
		return u.poison(types.Typ(types.Int))
	}
	ft, _ := asFunc(o.typ)
	if ft == nil {
		return u.poison(types.Typ(types.Int))
	}
	args, sret, retTy := u.callArgs(e, ft)
	res := u.blk().Call(callee, args...)
	switch {
	case !sret.IsZero():
		return value{sret, retTy}
	case isVoid(types.Unqualify(retTy)):
		return value{nil, retTy}
	case res.Len() == 0:
		return u.poison(retTy)
	default:
		return value{res.Value(0), retTy}
	}
}

// offsetofExpr computes __builtin_offsetof(type, member).
//
// The member designator is a chain of selections and subscripts against the
// type, not an expression: nothing in it names anything in scope, so it is
// walked structurally here rather than typed and emitted. vcc's own
// <stddef.h> writes offsetof as an address computation, which needs none of
// this; the builtin is here because headers written for gcc use it directly.
func (u *unit) offsetofExpr(e *ast.OffsetofExpr) value {
	st := u.sizeType()
	if e.Type == nil || e.Member == nil {
		u.errorf(e, "__builtin_offsetof takes a type and a member")
		return u.poison(st)
	}
	off, ok := u.offsetOfPath(u.typeOf(e.Type), e.Member)
	if !ok {
		return u.poison(st)
	}
	return u.intConst(off, st)
}

// typesCompatibleExpr computes __builtin_types_compatible_p(type, type).
//
// The analyzer already folds this — it has to, because the result is usable
// in an integer constant expression — so this is the same answer reached the
// same way, for the case where the builtin is written somewhere no constant
// was demanded and lower is simply emitting an expression.
func (u *unit) typesCompatibleExpr(e *ast.TypesCompatibleExpr) value {
	t := types.Typ(types.Int)
	if e.A == nil || e.B == nil {
		u.errorf(e, "__builtin_types_compatible_p takes two type names")
		return u.poison(t)
	}
	a, b := u.typeOf(e.A), u.typeOf(e.B)
	if a == nil || b == nil {
		return u.poison(t)
	}
	var n int64
	if types.CompatibleIgnoringQuals(a, b) {
		n = 1
	}
	return u.intConst(n, t)
}

// offsetOfPath walks a designator chain and returns the byte offset it names.
func (u *unit) offsetOfPath(base types.Type, e ast.Expr) (int64, bool) {
	switch x := e.(type) {
	case *ast.Ident:
		return u.memberOffset(base, u.name(x), x)

	case *ast.SelectorExpr:
		off, ok := u.offsetOfPath(base, x.X)
		if !ok {
			return 0, false
		}
		inner, ok := u.pathType(base, x.X)
		if !ok {
			return 0, false
		}
		n, ok := u.memberOffset(inner, u.name(x.Sel), x.Sel)
		return off + n, ok

	case *ast.IndexExpr:
		off, ok := u.offsetOfPath(base, x.X)
		if !ok {
			return 0, false
		}
		at, ok := u.pathType(base, x.X)
		if !ok {
			return 0, false
		}
		a, isArr := asArray(at)
		if !isArr {
			u.errorf(x, "cannot subscript %s in an offsetof designator", at)
			return 0, false
		}
		i, ok := u.constInt(x.Index)
		if !ok {
			u.errorf(x, "an offsetof subscript must be a constant expression")
			return 0, false
		}
		esz, _ := u.model.Sizeof(types.Unqualify(a.Elem))
		return off + i*esz, true
	}
	u.errorf(e, "this is not a member designator")
	return 0, false
}

// pathType is the type a designator chain arrives at.
func (u *unit) pathType(base types.Type, e ast.Expr) (types.Type, bool) {
	switch x := e.(type) {
	case *ast.Ident:
		return u.memberType(base, u.name(x), x)
	case *ast.SelectorExpr:
		inner, ok := u.pathType(base, x.X)
		if !ok {
			return nil, false
		}
		return u.memberType(inner, u.name(x.Sel), x.Sel)
	case *ast.IndexExpr:
		at, ok := u.pathType(base, x.X)
		if !ok {
			return nil, false
		}
		a, isArr := asArray(at)
		if !isArr {
			return nil, false
		}
		return a.Elem, true
	}
	return nil, false
}

func (u *unit) memberOffset(rec types.Type, name string, at ast.Node) (int64, bool) {
	r, ok := asRecord(types.Unqualify(rec))
	if !ok {
		u.errorf(at, "%s is not a structure or union", rec)
		return 0, false
	}
	path := u.types.member(r, name)
	if path == nil {
		u.errorf(at, "%s has no member named '%s'", rec, name)
		return 0, false
	}
	off, place := u.types.offsetOf(path)
	if place.Bit {
		u.errorf(at, "offsetof of the bit-field '%s' has no answer in bytes", name)
		return 0, false
	}
	return off, true
}

func (u *unit) memberType(rec types.Type, name string, at ast.Node) (types.Type, bool) {
	r, ok := asRecord(types.Unqualify(rec))
	if !ok {
		return nil, false
	}
	path := u.types.member(r, name)
	if path == nil {
		u.errorf(at, "%s has no member named '%s'", rec, name)
		return nil, false
	}
	last := path[len(path)-1]
	return last.rec.Fields[last.index].Type, true
}

// intAbs is |x| for a signed integer: negate it where it is negative, which
// is what the machine does and what a call to abs would have cost.
//
// abs(INT_MIN) is undefined in C, and this answers INT_MIN for it — the
// two's-complement negation, and the same answer every implementation gives.
func (u *unit) intAbs(e *ast.CallExpr, t types.Type) value {
	if len(e.Args) < 1 {
		u.errorf(e, "this builtin takes one argument")
		return u.poison(t)
	}
	v := u.convert(u.expr(e.Args[0]), t, e)
	if v.v == nil {
		return u.poison(t)
	}
	b := u.blk()
	zero := u.intConst(0, t)
	neg := value{u.emitNeg(b, v.v, e), t}
	c := u.emitCompare(b, v.v, zero.v, t, token.LSS, e)
	if c.IsZero() {
		return u.poison(t)
	}
	return u.selectValue(b, c, neg, v, t, e)
}

// selectValue picks one of two values without branching, where the register
// namespace has a select; otherwise it falls back to a conditional.
func (u *unit) selectValue(b *ir.Block, c ir.I1, whenTrue, whenFalse value, t types.Type, at ast.Node) value {
	switch x := whenTrue.v.(type) {
	case ir.I32:
		return value{b.I32.Select(c, x, whenFalse.v.(ir.I32)), t}
	case ir.I64:
		return value{b.I64.Select(c, x, whenFalse.v.(ir.I64)), t}
	}
	u.errorf(at, "internal: no select for %s", t)
	return u.poison(t)
}

// chkCall lowers one of the fortified builtins to the library call it wraps.
//
// dropTail is how many arguments to drop from the end — the object size a
// _chk call carries — and dropMid how many to drop before the format string,
// counted from the position the printf family puts them in: two arguments
// (a flag and the size) sitting after the ones the plain call keeps.
func (u *unit) chkCall(name string, e *ast.CallExpr, dropTail, dropMid int) value {
	lib := strings.TrimSuffix(strings.TrimPrefix(name, "__builtin___"), "_chk")
	args := e.Args
	switch {
	case dropMid > 0:
		// The kept prefix is everything before the flag, which is the whole
		// argument list minus the format and what follows minus dropMid.
		keep := len(args) - dropMid
		if keep < 0 {
			u.errorf(e, "%s takes more arguments than this", name)
			return u.poison(types.Typ(types.Int))
		}
		// Find where the dropped pair starts: for snprintf it is after two
		// kept arguments, for sprintf after one.
		at := 1
		if strings.Contains(lib, "snprintf") {
			at = 2
		}
		if at+dropMid > len(args) {
			u.errorf(e, "%s takes more arguments than this", name)
			return u.poison(types.Typ(types.Int))
		}
		args = append(append([]ast.Expr{}, args[:at]...), args[at+dropMid:]...)
	case dropTail > 0:
		if len(args) < dropTail {
			u.errorf(e, "%s takes more arguments than this", name)
			return u.poison(types.Typ(types.Int))
		}
		args = args[:len(args)-dropTail]
	}
	plain := *e
	plain.Args = args
	return u.builtinAsCall("__builtin_"+lib, &plain)
}

// bitCountRuntime is the three bit-counting builtins on a value that is not a
// constant.
//
// Each is the baseline sequence rather than the instruction the name suggests.
// LZCNT, TZCNT and POPCNT each carry a CPUID bit, and gcc expands these
// builtins on a target without them rather than refusing — a program that
// writes __builtin_popcount is asking for the count, not for the instruction,
// which is exactly the distinction intrin.go's header comment draws. An
// intrinsic that asks for POPCNT by name gets POPCNT.
//
// BSR and BSF are 386 instructions, so clz and ctz are one instruction plus
// the zero case the hardware leaves undefined and C leaves undefined too —
// answered here rather than left to whatever the register held, since a
// defined answer costs one select. popcount is the usual SWAR: five steps,
// no table, no branch.
func (u *unit) bitCountRuntime(e *ast.CallExpr, op string, v value, width int64) value {
	intT := types.Typ(types.Int)
	pt := u.promote(v.t)
	cv := u.convert(v, pt, e)
	if cv.v == nil {
		return u.poison(intT)
	}
	b := u.blk()
	wide := width > 32

	if op == "popcount" {
		var r ir.Value
		if wide {
			x, _ := cv.v.(ir.I64)
			r = u.popcount64(b, x)
		} else {
			x, _ := cv.v.(ir.I32)
			r = u.popcount32(b, x)
		}
		if r == nil {
			return u.poison(intT)
		}
		return u.convert(value{r, pt}, intT, e)
	}

	suffix, reg := "l", ir.TypeI32
	if wide {
		suffix, reg = "q", ir.TypeI64
	}
	name := "bsf" + suffix
	if op == "clz" {
		name = "bsr" + suffix
	}
	idx := u.asm1(name+" %1, %0", reg, "=r", asmIn{cv.v, "r"})
	if idx == nil {
		return u.poison(intT)
	}
	if wide {
		x, _ := cv.v.(ir.I64)
		n, _ := idx.(ir.I64)
		if op == "clz" {
			n = b.I64.Sub(b.I64.Const(width-1), n)
		}
		zero := b.I64.Eq(x, b.I64.Const(0))
		n = b.I64.Select(zero, b.I64.Const(width), n)
		return u.convert(value{n, pt}, intT, e)
	}
	x, _ := cv.v.(ir.I32)
	n, _ := idx.(ir.I32)
	if op == "clz" {
		n = b.I32.Sub(b.I32.Const(width-1), n)
	}
	zero := b.I32.Eq(x, b.I32.Const(0))
	n = b.I32.Select(zero, b.I32.Const(width), n)
	return u.convert(value{n, pt}, intT, e)
}

// popcount32 is the SWAR sequence: pairs, then nibbles, then bytes, then one
// multiply that sums the bytes into the top one.
func (u *unit) popcount32(b *ir.Block, x ir.I32) ir.Value {
	if x.Def() == nil {
		return nil
	}
	k := b.I32.Const
	x = b.I32.Sub(x, b.I32.And(b.I32.UShr(x, k(1)), k(0x55555555)))
	x = b.I32.Add(b.I32.And(x, k(0x33333333)), b.I32.And(b.I32.UShr(x, k(2)), k(0x33333333)))
	x = b.I32.And(b.I32.Add(x, b.I32.UShr(x, k(4))), k(0x0f0f0f0f))
	return b.I32.UShr(b.I32.Mul(x, k(0x01010101)), k(24))
}

func (u *unit) popcount64(b *ir.Block, x ir.I64) ir.Value {
	if x.Def() == nil {
		return nil
	}
	k := b.I64.Const
	x = b.I64.Sub(x, b.I64.And(b.I64.UShr(x, k(1)), k(0x5555555555555555)))
	x = b.I64.Add(b.I64.And(x, k(0x3333333333333333)), b.I64.And(b.I64.UShr(x, k(2)), k(0x3333333333333333)))
	x = b.I64.And(b.I64.Add(x, b.I64.UShr(x, k(4))), k(0x0f0f0f0f0f0f0f0f))
	return b.I64.UShr(b.I64.Mul(x, k(0x0101010101010101)), k(56))
}
