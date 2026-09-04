package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/types"
)

// The _mm_* intrinsics: SSE2's integer half, which is what <emmintrin.h>
// declares and what a workload's vector code is written in.
//
// They are here for the reason the Interlocked family is: they are not a
// dialect. The platform's header declares them, nothing defines them — MSVC
// hands the name to the compiler with `#pragma intrinsic` and ucrt stops at
// the prototype — so a compiler that lowers one as an ordinary call compiles
// a program that does not link. gcc and clang reach the same instructions
// through their own headers and their own builtins; the intrinsic names are
// the part all three agree on, and the part the source uses.
//
// The mapping is one line each, because §V was designed against this list.
// Where an intrinsic is not one instruction — the set family, the compares
// that read the other way round — the shape is spelled out below rather than
// hidden in a verb, so that what it costs is visible.

// vecBinary is the intrinsics that are one verb over two vectors. The name
// is the whole of the mapping; the lane shape is in both spellings and they
// agree.
var vecBinary = map[string]func(ir.V128NS, ir.V128, ir.V128) ir.V128{
	"_mm_add_epi8":  ir.V128NS.I8x16Add,
	"_mm_add_epi16": ir.V128NS.I16x8Add,
	"_mm_add_epi32": ir.V128NS.I32x4Add,
	"_mm_add_epi64": ir.V128NS.I64x2Add,
	"_mm_sub_epi8":  ir.V128NS.I8x16Sub,
	"_mm_sub_epi16": ir.V128NS.I16x8Sub,
	"_mm_sub_epi32": ir.V128NS.I32x4Sub,
	"_mm_sub_epi64": ir.V128NS.I64x2Sub,

	"_mm_adds_epi8":  ir.V128NS.I8x16AddSatS,
	"_mm_adds_epi16": ir.V128NS.I16x8AddSatS,
	"_mm_adds_epu8":  ir.V128NS.I8x16AddSatU,
	"_mm_adds_epu16": ir.V128NS.I16x8AddSatU,
	"_mm_subs_epi8":  ir.V128NS.I8x16SubSatS,
	"_mm_subs_epi16": ir.V128NS.I16x8SubSatS,
	"_mm_subs_epu8":  ir.V128NS.I8x16SubSatU,
	"_mm_subs_epu16": ir.V128NS.I16x8SubSatU,

	"_mm_mullo_epi16": ir.V128NS.I16x8Mul,
	"_mm_mulhi_epi16": ir.V128NS.I16x8MulHiS,
	"_mm_mulhi_epu16": ir.V128NS.I16x8MulHiU,
	"_mm_mul_epu32":   ir.V128NS.I32x4MulEvenU,
	"_mm_madd_epi16":  ir.V128NS.I16x8MaddS,
	"_mm_sad_epu8":    ir.V128NS.I8x16SadU,

	"_mm_min_epu8":  ir.V128NS.I8x16MinU,
	"_mm_max_epu8":  ir.V128NS.I8x16MaxU,
	"_mm_min_epi16": ir.V128NS.I16x8MinS,
	"_mm_max_epi16": ir.V128NS.I16x8MaxS,
	"_mm_avg_epu8":  ir.V128NS.I8x16AvgrU,
	"_mm_avg_epu16": ir.V128NS.I16x8AvgrU,

	"_mm_and_si128": ir.V128NS.And,
	"_mm_or_si128":  ir.V128NS.Or,
	"_mm_xor_si128": ir.V128NS.Xor,

	"_mm_cmpeq_epi8":  ir.V128NS.I8x16Eq,
	"_mm_cmpeq_epi16": ir.V128NS.I16x8Eq,
	"_mm_cmpeq_epi32": ir.V128NS.I32x4Eq,
	"_mm_cmpgt_epi8":  ir.V128NS.I8x16GtS,
	"_mm_cmpgt_epi16": ir.V128NS.I16x8GtS,
	"_mm_cmpgt_epi32": ir.V128NS.I32x4GtS,

	"_mm_packs_epi16":  ir.V128NS.I8x16NarrowS,
	"_mm_packus_epi16": ir.V128NS.I8x16NarrowU,
	"_mm_packs_epi32":  ir.V128NS.I16x8NarrowS,

	"_mm_unpacklo_epi8":  ir.V128NS.I8x16UnpackLow,
	"_mm_unpacklo_epi16": ir.V128NS.I16x8UnpackLow,
	"_mm_unpacklo_epi32": ir.V128NS.I32x4UnpackLow,
	"_mm_unpacklo_epi64": ir.V128NS.I64x2UnpackLow,
	"_mm_unpackhi_epi8":  ir.V128NS.I8x16UnpackHigh,
	"_mm_unpackhi_epi16": ir.V128NS.I16x8UnpackHigh,
	"_mm_unpackhi_epi32": ir.V128NS.I32x4UnpackHigh,
	"_mm_unpackhi_epi64": ir.V128NS.I64x2UnpackHigh,
}

// vecSwapped is the three compares written the other way round. There is no
// PCMPLT: the hardware has one direction and the assembler's other spelling
// is the same instruction with its operands exchanged, which is exactly what
// §V says about gt and lt everywhere else.
var vecSwapped = map[string]func(ir.V128NS, ir.V128, ir.V128) ir.V128{
	"_mm_cmplt_epi8":  ir.V128NS.I8x16GtS,
	"_mm_cmplt_epi16": ir.V128NS.I16x8GtS,
	"_mm_cmplt_epi32": ir.V128NS.I32x4GtS,
}

// vecShift is the lane shifts. Each takes a vector and a count that is an
// ordinary int — a literal in almost every use, which is the form the
// instruction has, and a register otherwise.
var vecShift = map[string]func(ir.V128NS, ir.V128, ir.I32) ir.V128{
	"_mm_slli_epi16": ir.V128NS.I16x8Shl,
	"_mm_slli_epi32": ir.V128NS.I32x4Shl,
	"_mm_slli_epi64": ir.V128NS.I64x2Shl,
	"_mm_srli_epi16": ir.V128NS.I16x8ShrU,
	"_mm_srli_epi32": ir.V128NS.I32x4ShrU,
	"_mm_srli_epi64": ir.V128NS.I64x2ShrU,
	"_mm_srai_epi16": ir.V128NS.I16x8ShrS,
	"_mm_srai_epi32": ir.V128NS.I32x4ShrS,
}

// vecByteShift is the two that shift the whole register, in bytes. The count
// has to be a literal because the instruction has no other form.
var vecByteShift = map[string]func(ir.V128NS, ir.V128, int64) ir.V128{
	"_mm_slli_si128":  ir.V128NS.ShlBytes,
	"_mm_bslli_si128": ir.V128NS.ShlBytes,
	"_mm_srli_si128":  ir.V128NS.ShrBytes,
	"_mm_bsrli_si128": ir.V128NS.ShrBytes,
}

// vecShuffle is the permutes, each taking a vector and a literal pattern.
var vecShuffle = map[string]func(ir.V128NS, ir.V128, int64) ir.V128{
	"_mm_shuffle_epi32":   ir.V128NS.I32x4Shuffle,
	"_mm_shufflelo_epi16": ir.V128NS.I16x8ShuffleLow,
	"_mm_shufflehi_epi16": ir.V128NS.I16x8ShuffleHigh,
}

// vecSet is the constructors, by the lane width they fill and whether the
// arguments run high-to-low. Intel wrote _mm_set_epi32 with the *highest*
// lane first, which reads like a written number and not like memory, and
// added the _setr_ forms once that had annoyed enough people.
var vecSet = map[string]struct {
	lane     int64
	reversed bool
}{
	"_mm_set_epi8":    {1, true},
	"_mm_set_epi16":   {2, true},
	"_mm_set_epi32":   {4, true},
	"_mm_set_epi64x":  {8, true},
	"_mm_setr_epi8":   {1, false},
	"_mm_setr_epi16":  {2, false},
	"_mm_setr_epi32":  {4, false},
	"_mm_setr_epi64x": {8, false},
}

// sseIntrinsic reports whether name is one of the intrinsics this file
// lowers, which is what builtin() asks before looking anywhere else.
func sseIntrinsic(name string) bool {
	if _, ok := vecBinary[name]; ok {
		return true
	}
	if _, ok := vecSwapped[name]; ok {
		return true
	}
	if _, ok := vecShift[name]; ok {
		return true
	}
	if _, ok := vecByteShift[name]; ok {
		return true
	}
	if _, ok := vecShuffle[name]; ok {
		return true
	}
	if _, ok := vecSet[name]; ok {
		return true
	}
	switch name {
	case "_mm_andnot_si128", "_mm_setzero_si128",
		"_mm_set1_epi8", "_mm_set1_epi16", "_mm_set1_epi32", "_mm_set1_epi64x",
		"_mm_load_si128", "_mm_loadu_si128", "_mm_lddqu_si128",
		"_mm_store_si128", "_mm_storeu_si128", "_mm_stream_si128",
		"_mm_loadl_epi64", "_mm_storel_epi64", "_mm_move_epi64",
		"_mm_movemask_epi8", "_mm_extract_epi16", "_mm_insert_epi16",
		"_mm_cvtsi32_si128", "_mm_cvtsi128_si32",
		"_mm_cvtsi64_si128", "_mm_cvtsi64x_si128",
		"_mm_cvtsi128_si64", "_mm_cvtsi128_si64x":
		return true
	}
	return false
}

// sseCall lowers one intrinsic.
func (u *unit) sseCall(name string, e *ast.CallExpr) value {
	// The result type comes from the prototype the header wrote, not from
	// this file: __m128i is the platform's declaration and vcc has no
	// spelling of its own for it. A void intrinsic answers void here, which
	// is what the store family wants.
	vt := u.intrinsicRet(name, e)
	b := u.blk()
	v := b.V128()

	// vec reads argument i as a vector, and int reads it as an int. Both
	// report through poison, so a wrong argument type is one diagnostic and
	// not a cascade.
	vec := func(i int) (ir.V128, bool) {
		if i >= len(e.Args) {
			u.errorf(e, "%s: too few arguments", name)
			return ir.V128{}, false
		}
		a := u.expr(e.Args[i])
		if !types.IsVector(a.t) {
			if a.v != nil {
				u.errorf(e.Args[i], "%s: argument %d is %s, not __m128i", name, i+1, a.t)
			}
			return ir.V128{}, false
		}
		x, _ := a.v.(ir.V128)
		return x, x.Def() != nil
	}
	intArg := func(i int) (ir.I32, bool) {
		if i >= len(e.Args) {
			u.errorf(e, "%s: too few arguments", name)
			return ir.I32{}, false
		}
		a := u.convert(u.expr(e.Args[i]), types.Typ(types.Int), e.Args[i])
		x, _ := a.v.(ir.I32)
		return x, x.Def() != nil
	}
	litArg := func(i int) (int64, bool) {
		if i >= len(e.Args) {
			u.errorf(e, "%s: too few arguments", name)
			return 0, false
		}
		k, ok := u.constInt(e.Args[i])
		if !ok {
			u.errorf(e.Args[i], "%s: argument %d must be a constant; the instruction's operand is an immediate", name, i+1)
			return 0, false
		}
		return k, true
	}
	ptrArg := func(i int) (ir.Ptr, bool) {
		if i >= len(e.Args) {
			u.errorf(e, "%s: too few arguments", name)
			return ir.Ptr{}, false
		}
		a := u.expr(e.Args[i])
		p := u.ptr(a.v, e.Args[i])
		return p, !p.IsZero()
	}

	if f, ok := vecBinary[name]; ok {
		x, ok1 := vec(0)
		y, ok2 := vec(1)
		if !ok1 || !ok2 {
			return u.poison(vt)
		}
		return value{f(v, x, y), vt}
	}
	if f, ok := vecSwapped[name]; ok {
		x, ok1 := vec(0)
		y, ok2 := vec(1)
		if !ok1 || !ok2 {
			return u.poison(vt)
		}
		return value{f(v, y, x), vt}
	}
	if f, ok := vecShift[name]; ok {
		x, ok1 := vec(0)
		n, ok2 := intArg(1)
		if !ok1 || !ok2 {
			return u.poison(vt)
		}
		return value{f(v, x, n), vt}
	}
	if f, ok := vecByteShift[name]; ok {
		x, ok1 := vec(0)
		k, ok2 := litArg(1)
		if !ok1 || !ok2 {
			return u.poison(vt)
		}
		return value{f(v, x, k), vt}
	}
	if f, ok := vecShuffle[name]; ok {
		x, ok1 := vec(0)
		k, ok2 := litArg(1)
		if !ok1 || !ok2 {
			return u.poison(vt)
		}
		return value{f(v, x, k&0xff), vt}
	}
	if s, ok := vecSet[name]; ok {
		return u.vecSetCall(name, e, s.lane, s.reversed)
	}

	switch name {
	// MSVC's andnot negates its *first* operand and §V's negates its
	// second, so the operands are exchanged here. Both spellings are
	// defensible and neither is a fact about the hardware, which negates
	// whichever one the destination holds.
	case "_mm_andnot_si128":
		x, ok1 := vec(0)
		y, ok2 := vec(1)
		if !ok1 || !ok2 {
			return u.poison(vt)
		}
		return value{v.AndNot(y, x), vt}

	case "_mm_setzero_si128":
		return value{v.Zero(), vt}

	case "_mm_set1_epi8", "_mm_set1_epi16", "_mm_set1_epi32":
		n, ok := intArg(0)
		if !ok {
			return u.poison(vt)
		}
		switch name {
		case "_mm_set1_epi8":
			return value{v.I8x16Splat(n), vt}
		case "_mm_set1_epi16":
			return value{v.I16x8Splat(n), vt}
		}
		return value{v.I32x4Splat(n), vt}

	case "_mm_set1_epi64x":
		if len(e.Args) < 1 {
			u.errorf(e, "%s: too few arguments", name)
			return u.poison(vt)
		}
		a := u.convert(u.expr(e.Args[0]), types.Typ(types.LongLong), e.Args[0])
		n, _ := a.v.(ir.I64)
		if n.Def() == nil {
			return u.poison(vt)
		}
		return value{v.I64x2Splat(n), vt}

	// The loads and stores differ in one thing: whether the address is
	// promised to be sixteen-byte aligned. That promise is the whole of
	// MOVDQA against MOVDQU, and it faults rather than being slow when it
	// is wrong, so it is stated on the access and carried to the encoder.
	case "_mm_load_si128":
		p, ok := ptrArg(0)
		if !ok {
			return u.poison(vt)
		}
		return value{v.Load(p), vt}

	case "_mm_loadu_si128", "_mm_lddqu_si128":
		p, ok := ptrArg(0)
		if !ok {
			return u.poison(vt)
		}
		return value{v.Load(p, ir.Align(1)), vt}

	case "_mm_store_si128", "_mm_stream_si128":
		// _mm_stream_si128 asks for a non-temporal store, which is a hint
		// about the cache and not about the result. An ordinary store is a
		// correct implementation of it, and is what this emits until there
		// is a memory attribute that carries the hint.
		p, ok1 := ptrArg(0)
		x, ok2 := vec(1)
		if !ok1 || !ok2 {
			return u.poison(types.Typ(types.Void))
		}
		v.Store(x, p)
		return value{nil, types.Typ(types.Void)}

	case "_mm_storeu_si128":
		p, ok1 := ptrArg(0)
		x, ok2 := vec(1)
		if !ok1 || !ok2 {
			return u.poison(types.Typ(types.Void))
		}
		v.Store(x, p, ir.Align(1))
		return value{nil, types.Typ(types.Void)}

	// Eight bytes in and out of the low quadword. The address carries no
	// alignment promise in either direction — Intel documents both as
	// unaligned — so the scalar access says so too.
	case "_mm_loadl_epi64":
		p, ok := ptrArg(0)
		if !ok {
			return u.poison(vt)
		}
		return value{v.ZExtI64(b.I64.Load(p, ir.Align(1))), vt}

	case "_mm_storel_epi64":
		p, ok1 := ptrArg(0)
		x, ok2 := vec(1)
		if !ok1 || !ok2 {
			return u.poison(types.Typ(types.Void))
		}
		b.I64.Store(v.I64x2ExtractLane(x, 0), p, ir.Align(1))
		return value{nil, types.Typ(types.Void)}

	// The low quadword kept and the high one zeroed, which is an unpack
	// against zero and not a move.
	case "_mm_move_epi64":
		x, ok := vec(0)
		if !ok {
			return u.poison(vt)
		}
		return value{v.I64x2UnpackLow(x, v.Zero()), vt}

	case "_mm_movemask_epi8":
		x, ok := vec(0)
		if !ok {
			return u.poison(types.Typ(types.Int))
		}
		return value{v.I8x16Bitmask(x), types.Typ(types.Int)}

	case "_mm_extract_epi16":
		x, ok1 := vec(0)
		k, ok2 := litArg(1)
		if !ok1 || !ok2 {
			return u.poison(types.Typ(types.Int))
		}
		return value{v.I16x8ExtractLaneU(x, k&7), types.Typ(types.Int)}

	case "_mm_insert_epi16":
		x, ok1 := vec(0)
		n, ok2 := intArg(1)
		k, ok3 := litArg(2)
		if !ok1 || !ok2 || !ok3 {
			return u.poison(vt)
		}
		return value{v.I16x8ReplaceLane(x, n, k&7), vt}

	case "_mm_cvtsi32_si128":
		n, ok := intArg(0)
		if !ok {
			return u.poison(vt)
		}
		return value{v.ZExtI32(n), vt}

	case "_mm_cvtsi128_si32":
		x, ok := vec(0)
		if !ok {
			return u.poison(types.Typ(types.Int))
		}
		return value{v.I32x4ExtractLane(x, 0), types.Typ(types.Int)}

	case "_mm_cvtsi64_si128", "_mm_cvtsi64x_si128":
		if len(e.Args) < 1 {
			u.errorf(e, "%s: too few arguments", name)
			return u.poison(vt)
		}
		a := u.convert(u.expr(e.Args[0]), types.Typ(types.LongLong), e.Args[0])
		n, _ := a.v.(ir.I64)
		if n.Def() == nil {
			return u.poison(vt)
		}
		return value{v.ZExtI64(n), vt}

	case "_mm_cvtsi128_si64", "_mm_cvtsi128_si64x":
		x, ok := vec(0)
		if !ok {
			return u.poison(types.Typ(types.LongLong))
		}
		return value{v.I64x2ExtractLane(x, 0), types.Typ(types.LongLong)}
	}

	u.errorf(e, "internal: %s is listed as an intrinsic and not lowered", name)
	return u.poison(vt)
}

// vecSetCall builds a vector from its lanes.
//
// Every argument a constant is a literal, which is sixteen bytes in .rodata
// and one load — or, when they are all zero, no load at all. Anything else
// is written into a frame temporary lane by lane and read back as a whole,
// which is what a general set costs on this hardware: there is no
// instruction that takes four registers.
func (u *unit) vecSetCall(name string, e *ast.CallExpr, lane int64, reversed bool) value {
	vt := u.intrinsicRet(name, e)
	n := int(16 / lane)
	if len(e.Args) != n {
		u.errorf(e, "%s takes %d arguments, not %d", name, n, len(e.Args))
		return u.poison(vt)
	}

	// lanes runs low to high whichever order the arguments were written in.
	lanes := make([]ast.Expr, n)
	for i, a := range e.Args {
		if reversed {
			lanes[n-1-i] = a
		} else {
			lanes[i] = a
		}
	}

	if bytes, ok := u.vecSetConst(lanes, lane); ok {
		return value{u.blk().V128().Const(bytes), vt}
	}

	elem := types.Typ(types.Int)
	switch lane {
	case 1:
		elem = types.Typ(types.SChar)
	case 2:
		elem = types.Typ(types.Short)
	case 8:
		elem = types.Typ(types.LongLong)
	}

	tmp := u.alloca(vt, "set", e)
	if tmp.IsZero() {
		return u.poison(vt)
	}
	for i, a := range lanes {
		b := u.blk()
		at := b.Ptr.Add(tmp, b.I64.Const(int64(i)*lane))
		u.store(u.convert(u.expr(a), elem, a), at, elem, a)
	}
	return value{u.blk().V128().Load(tmp), vt}
}

// vecSetConst folds a set whose every lane is an integer constant into the
// sixteen bytes it names, little-endian because every target with this
// register file is.
func (u *unit) vecSetConst(lanes []ast.Expr, lane int64) ([16]byte, bool) {
	var out [16]byte
	for i, a := range lanes {
		k, ok := u.constInt(a)
		if !ok {
			return out, false
		}
		for j := int64(0); j < lane; j++ {
			out[int64(i)*lane+j] = byte(k >> (8 * j))
		}
	}
	return out, true
}

// intrinsicRet is the type the header declared the intrinsic to return.
//
// vcc has no name for __m128i of its own — the platform's <emmintrin.h>
// declares it, as MSVC's does with __declspec(intrin_type) on a union — so
// the type of a result has to come from the prototype. A program that calls
// one of these without declaring it gets int, which is what §6.5.1 gives any
// undeclared call, and the argument checks below then say what is wrong.
// intrinsicRet is the type the header declared the intrinsic to return.
//
// vcc has no name for __m128i of its own — the platform's <emmintrin.h>
// declares it, as MSVC's does with __declspec(intrin_type) on a union — so
// the type of a result has to come from the prototype the program included.
// The declaration is looked up rather than the call's recorded type because
// a void intrinsic's call has no recorded type at all, and void is exactly
// what the store family should answer.
func (u *unit) intrinsicRet(name string, e *ast.CallExpr) types.Type {
	if o := u.lookup(name); o != nil {
		if ft, ok := asFunc(types.Unqualify(o.typ)); ok {
			return ft.Ret
		}
	}
	u.errorf(e, "%s: no declaration in scope; include <emmintrin.h>", name)
	return types.Typ(types.Int)
}
