package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/types"
)

// The <intrin.h> family: MSVC's compiler intrinsics.
//
// They are here for the reason the Interlocked family and the _mm_* family
// are, and it is the same reason each time: the platform's header declares
// them, `#pragma intrinsic` hands the name to the compiler, and no library
// defines them. ucrt exports no __cpuid. A compiler that lowers one as an
// ordinary call compiles a program that does not link, which is what a
// program including <intrin.h> got here until now.
//
// Two kinds live in this file and they lower differently on purpose.
//
// An intrinsic that *names an instruction* — __cpuid, __rdtsc, _BitScanForward,
// __popcnt — is lowered to that instruction through inline assembly. That is
// what the programmer asked for, and it is what MSVC emits: it gates none of
// them on a feature flag, because writing __popcnt is the statement that the
// target has POPCNT. Going through the IR's own popcnt verb instead would put
// the question to Options.Features, which is the right question for
// __builtin_popcount and the wrong one here.
//
// An intrinsic that *names a value* — _rotl, _byteswap_ulong, __umulh — is
// lowered to the IR verb that computes it, because a rotate is a rotate on
// every target and the backend already knows the cheapest way to write one.

// intrinsic reports whether name is one of these, which is what builtin()
// asks before looking anywhere else.
func intrinsic(name string) bool {
	switch name {
	case "_rotl", "_rotr", "_lrotl", "_lrotr", "_rotl64", "_rotr64",
		"_rotl8", "_rotr8", "_rotl16", "_rotr16",
		"_byteswap_ushort", "_byteswap_ulong", "_byteswap_uint64",
		"_BitScanForward", "_BitScanReverse", "_BitScanForward64", "_BitScanReverse64",
		"__popcnt", "__popcnt16", "__popcnt64", "__lzcnt", "__lzcnt16", "__lzcnt64",
		"__mulh", "__umulh", "_umul128", "_mul128",
		"__shiftleft128", "__shiftright128",
		"_addcarry_u32", "_addcarry_u64", "_subborrow_u32", "_subborrow_u64",
		"__cpuid", "__cpuidex", "__rdtsc",
		"__readgsbyte", "__readgsword", "__readgsdword", "__readgsqword",
		"_ReadWriteBarrier", "_ReadBarrier", "_WriteBarrier",
		"_mm_mfence", "_mm_sfence", "_mm_lfence",
		"_mm_pause", "__nop", "__debugbreak", "__halt", "_mm_prefetch",
		"_ReturnAddress", "_alloca",
		"__movsb", "__stosb",
		"_bittest", "_bittest64", "_bittestandset", "_bittestandset64",
		"_bittestandreset", "_bittestandreset64",
		"_bittestandcomplement", "_bittestandcomplement64",
		"_interlockedbittestandset", "_interlockedbittestandreset":
		return true
	}
	return false
}

// intrinCall lowers one of them.
func (u *unit) intrinCall(name string, e *ast.CallExpr) value {
	i32 := types.Typ(types.Int)
	u32 := types.Typ(types.UInt)
	u64 := types.Typ(types.ULongLong)
	void := types.Typ(types.Void)
	b := u.blk()

	// arg reads argument i converted to t, and reports through poison so a
	// wrong call is one diagnostic and not a cascade.
	arg := func(i int, t types.Type) (ir.Value, bool) {
		if i >= len(e.Args) {
			u.errorf(e, "%s: too few arguments", name)
			return nil, false
		}
		v := u.convert(u.expr(e.Args[i]), t, e.Args[i])
		return v.v, v.v != nil
	}
	arg32 := func(i int) (ir.I32, bool) {
		v, ok := arg(i, u32)
		x, _ := v.(ir.I32)
		return x, ok && x.Def() != nil
	}
	arg64 := func(i int) (ir.I64, bool) {
		v, ok := arg(i, u64)
		x, _ := v.(ir.I64)
		return x, ok && x.Def() != nil
	}
	argPtr := func(i int) (ir.Ptr, bool) {
		if i >= len(e.Args) {
			u.errorf(e, "%s: too few arguments", name)
			return ir.Ptr{}, false
		}
		p := u.ptr(u.expr(e.Args[i]).v, e.Args[i])
		return p, !p.IsZero()
	}

	switch name {

	// ---- rotates: the IR verb, at the width the name gives ----------------

	case "_rotl", "_lrotl", "_rotr", "_lrotr":
		x, ok1 := arg32(0)
		n, ok2 := arg32(1)
		if !ok1 || !ok2 {
			return u.poison(u32)
		}
		if name == "_rotr" || name == "_lrotr" {
			return value{b.I32.RotR(x, n), u32}
		}
		return value{b.I32.RotL(x, n), u32}

	case "_rotl64", "_rotr64":
		x, ok1 := arg64(0)
		n, ok2 := arg32(1)
		if !ok1 || !ok2 {
			return u.poison(u64)
		}
		amt := b.I64.ZExtI32(n)
		if name == "_rotr64" {
			return value{b.I64.RotR(x, amt), u64}
		}
		return value{b.I64.RotL(x, amt), u64}

	// The narrow rotates have no instruction and no IR verb: eight and
	// sixteen are storage widths, not register ones. Two shifts and an OR,
	// with the count masked, which is what the wide rotate does in silicon
	// and what a caller would have written.
	case "_rotl8", "_rotr8", "_rotl16", "_rotr16":
		w := int64(8)
		rt := types.Typ(types.UChar)
		if name == "_rotl16" || name == "_rotr16" {
			w, rt = 16, types.Typ(types.UShort)
		}
		x, ok1 := arg32(0)
		n, ok2 := arg32(1)
		if !ok1 || !ok2 {
			return u.poison(rt)
		}
		mask := b.I32.Const((1 << uint(w)) - 1)
		x = b.I32.And(x, mask)
		n = b.I32.And(n, b.I32.Const(w-1))
		other := b.I32.Sub(b.I32.Const(w), n)
		// A count of zero would shift the other way by the full width,
		// which is the one case the two-shift form gets wrong. Masking the
		// result is not enough — the shift itself is taken modulo 32 — so
		// the far half is masked to zero when the count is zero.
		other = b.I32.And(other, b.I32.Const(w-1))
		var lo, hi ir.I32
		if name == "_rotl8" || name == "_rotl16" {
			lo, hi = b.I32.Shl(x, n), b.I32.UShr(x, other)
		} else {
			lo, hi = b.I32.UShr(x, n), b.I32.Shl(x, other)
		}
		zero := b.I32.Eq(n, b.I32.Const(0))
		hi = b.I32.Select(zero, b.I32.Const(0), hi)
		return u.convert(value{b.I32.And(b.I32.Or(lo, hi), mask), u32}, rt, e)

	// ---- byte swaps: §A6's bswap ------------------------------------------

	case "_byteswap_ushort":
		x, ok := arg32(0)
		if !ok {
			return u.poison(types.Typ(types.UShort))
		}
		// bswap is a whole-register operation, so a sixteen-bit swap is the
		// thirty-two-bit one with the halves it moved into the top brought
		// back down.
		s := b.I32.UShr(b.I32.Bswap(x), b.I32.Const(16))
		return u.convert(value{s, u32}, types.Typ(types.UShort), e)

	case "_byteswap_ulong":
		x, ok := arg32(0)
		if !ok {
			return u.poison(u32)
		}
		return value{b.I32.Bswap(x), u32}

	case "_byteswap_uint64":
		x, ok := arg64(0)
		if !ok {
			return u.poison(u64)
		}
		return value{b.I64.Bswap(x), u64}

	// ---- bit scan: BSF and BSR, which are baseline ------------------------
	//
	// Both are undefined for a zero mask and MSVC says so: the index is not
	// written and the intrinsic answers zero. That is the shape here — the
	// store is conditional on the mask, not on the instruction.

	case "_BitScanForward", "_BitScanReverse", "_BitScanForward64", "_BitScanReverse64":
		wide := name == "_BitScanForward64" || name == "_BitScanReverse64"
		forward := name == "_BitScanForward" || name == "_BitScanForward64"
		p, ok1 := argPtr(0)
		if !ok1 {
			return u.poison(types.Typ(types.UChar))
		}
		var found ir.I1
		var idx ir.Value
		if wide {
			m, ok := arg64(1)
			if !ok {
				return u.poison(types.Typ(types.UChar))
			}
			op := "bsfq"
			if !forward {
				op = "bsrq"
			}
			idx = u.asm1(op+" %1, %0", ir.TypeI64, "=r", asmIn{m, "r"})
			found = b.I64.Ne(m, b.I64.Const(0))
		} else {
			m, ok := arg32(1)
			if !ok {
				return u.poison(types.Typ(types.UChar))
			}
			op := "bsfl"
			if !forward {
				op = "bsrl"
			}
			idx = u.asm1(op+" %1, %0", ir.TypeI32, "=r", asmIn{m, "r"})
			found = b.I32.Ne(m, b.I32.Const(0))
		}
		if idx == nil {
			return u.poison(types.Typ(types.UChar))
		}
		// The index is written only where there was a bit to find, which is
		// what "undefined" means here in practice: MSVC leaves the caller's
		// variable alone.
		// The index is an unsigned long — thirty-two bits on this model —
		// whichever width the mask was.
		it := u32
		if wide {
			it = u64
		}
		u.storeIf(found, u.convert(value{idx, it}, u32, e), p, u32, e)
		return u.convert(value{b.I32.ZExtI1(found), u32}, types.Typ(types.UChar), e)

	// ---- counting: the instruction the name asks for -----------------------

	case "__popcnt", "__popcnt16", "__popcnt64",
		"__lzcnt", "__lzcnt16", "__lzcnt64":
		return u.namedBitCount(name, e)

	// ---- wide multiply: §A's high-half verbs -------------------------------

	case "__mulh", "__umulh":
		a, ok1 := arg64(0)
		c, ok2 := arg64(1)
		if !ok1 || !ok2 {
			return u.poison(u64)
		}
		if name == "__mulh" {
			return value{b.I64.SMulHi(a, c), types.Typ(types.LongLong)}
		}
		return value{b.I64.UMulHi(a, c), u64}

	case "_umul128", "_mul128":
		a, ok1 := arg64(0)
		c, ok2 := arg64(1)
		p, ok3 := argPtr(2)
		if !ok1 || !ok2 || !ok3 {
			return u.poison(u64)
		}
		var hi ir.I64
		if name == "_mul128" {
			hi = b.I64.SMulHi(a, c)
		} else {
			hi = b.I64.UMulHi(a, c)
		}
		u.store(value{hi, u64}, p, u64, e)
		return value{b.I64.Mul(a, c), u64}

	// ---- the 128-bit shifts ------------------------------------------------
	//
	// SHLD and SHRD, written out rather than named: the instruction takes
	// its count in CL or an immediate and the operand order is the reverse
	// of the intrinsic's, and the arithmetic below is short enough that the
	// instruction buys nothing a caller can measure.

	case "__shiftleft128", "__shiftright128":
		lo, ok1 := arg64(0)
		hi, ok2 := arg64(1)
		n, ok3 := arg32(2)
		if !ok1 || !ok2 || !ok3 {
			return u.poison(u64)
		}
		amt := b.I64.And(b.I64.ZExtI32(n), b.I64.Const(63))
		far := b.I64.And(b.I64.Sub(b.I64.Const(64), amt), b.I64.Const(63))
		var near, other ir.I64
		if name == "__shiftleft128" {
			near, other = b.I64.Shl(hi, amt), b.I64.UShr(lo, far)
		} else {
			near, other = b.I64.UShr(lo, amt), b.I64.Shl(hi, far)
		}
		// A count of zero: the far shift would be by sixty-four, which the
		// hardware takes modulo the width and so would contribute the whole
		// of the other half.
		zero := b.I64.Eq(amt, b.I64.Const(0))
		other = b.I64.Select(zero, b.I64.Const(0), other)
		return value{b.I64.Or(near, other), u64}

	// ---- carry chains ------------------------------------------------------
	//
	// §A2's predicates are exactly the flag these produce: the sum and the
	// overflow bit together are the widened answer. Two adds, because a
	// carry-in is an add of its own, and either one of them can overflow.

	case "_addcarry_u32", "_addcarry_u64", "_subborrow_u32", "_subborrow_u64":
		wide := name == "_addcarry_u64" || name == "_subborrow_u64"
		add := name == "_addcarry_u32" || name == "_addcarry_u64"
		cin, ok0 := arg32(0)
		if !ok0 {
			return u.poison(types.Typ(types.UChar))
		}
		p, ok3 := argPtr(3)
		if !ok3 {
			return u.poison(types.Typ(types.UChar))
		}
		cin = b.I32.And(cin, b.I32.Const(1))
		if wide {
			x, ok1 := arg64(1)
			y, ok2 := arg64(2)
			if !ok1 || !ok2 {
				return u.poison(types.Typ(types.UChar))
			}
			c64 := b.I64.ZExtI32(cin)
			var s1, s2 ir.I64
			var o1, o2 ir.I1
			if add {
				s1, o1 = b.I64.Add(x, y), b.I64.UAddO(x, y)
				s2, o2 = b.I64.Add(s1, c64), b.I64.UAddO(s1, c64)
			} else {
				s1, o1 = b.I64.Sub(x, y), b.I64.ULt(x, y)
				s2, o2 = b.I64.Sub(s1, c64), b.I64.ULt(s1, c64)
			}
			u.store(value{s2, u64}, p, u64, e)
			return u.convert(value{b.I32.ZExtI1(b.I1.Or(o1, o2)), u32}, types.Typ(types.UChar), e)
		}
		x, ok1 := arg32(1)
		y, ok2 := arg32(2)
		if !ok1 || !ok2 {
			return u.poison(types.Typ(types.UChar))
		}
		var s1, s2 ir.I32
		var o1, o2 ir.I1
		if add {
			s1, o1 = b.I32.Add(x, y), b.I32.UAddO(x, y)
			s2, o2 = b.I32.Add(s1, cin), b.I32.UAddO(s1, cin)
		} else {
			s1, o1 = b.I32.Sub(x, y), b.I32.ULt(x, y)
			s2, o2 = b.I32.Sub(s1, cin), b.I32.ULt(s1, cin)
		}
		u.store(value{s2, u32}, p, u32, e)
		return u.convert(value{b.I32.ZExtI1(b.I1.Or(o1, o2)), u32}, types.Typ(types.UChar), e)

	// ---- the processor -----------------------------------------------------

	case "__cpuid", "__cpuidex":
		p, ok1 := argPtr(0)
		leaf, ok2 := arg32(1)
		if !ok1 || !ok2 {
			return u.poison(void)
		}
		sub := b.I32.Const(0)
		if name == "__cpuidex" {
			s, ok := arg32(2)
			if !ok {
				return u.poison(void)
			}
			sub = s
		}
		// Four outputs in four named registers, which is the one shape the
		// instruction has. RBX is callee-saved on both conventions and the
		// allocator has to know it was written, which naming it as an
		// output is how.
		st := u.blk().Asm("cpuid").Volatile().
			Out(ir.TypeI32, ir.CStr("=a")).
			Out(ir.TypeI32, ir.CStr("=b")).
			Out(ir.TypeI32, ir.CStr("=c")).
			Out(ir.TypeI32, ir.CStr("=d")).
			In(leaf, ir.CStr("a")).
			In(sub, ir.CStr("c"))
		res := st.Emit()
		if res.Len() != 4 {
			return u.poison(void)
		}
		for i := 0; i < 4; i++ {
			at := b.Ptr.Add(p, b.I64.Const(int64(i)*4))
			u.store(value{res.Value(i), u32}, at, i32, e)
		}
		return value{nil, void}

	case "__rdtsc":
		st := u.blk().Asm("rdtsc").Volatile().
			Out(ir.TypeI64, ir.CStr("=a")).
			Out(ir.TypeI64, ir.CStr("=d"))
		res := st.Emit()
		if res.Len() != 2 {
			return u.poison(u64)
		}
		lo, _ := res.Value(0).(ir.I64)
		hi, _ := res.Value(1).(ir.I64)
		// EDX:EAX, and the halves arrive zero-extended because the
		// instruction writes the 32-bit views.
		return value{b.I64.Or(lo, b.I64.Shl(hi, b.I64.Const(32))), u64}

	// ---- the thread environment block --------------------------------------

	case "__readgsbyte", "__readgsword", "__readgsdword", "__readgsqword":
		off, ok := arg32(0)
		if !ok {
			return u.poison(u64)
		}
		var tmpl string
		rt, out := ir.TypeI32, u32
		switch name {
		case "__readgsbyte":
			tmpl = "movzbl %%gs:(%1), %0"
		case "__readgsword":
			tmpl = "movzwl %%gs:(%1), %0"
		case "__readgsdword":
			tmpl = "movl %%gs:(%1), %0"
		default:
			tmpl, rt, out = "movq %%gs:(%1), %0", ir.TypeI64, u64
		}
		// The offset is an address within the segment, so it is a 64-bit
		// operand however narrow the value read is.
		r := u.asm1v(tmpl, rt, "=r", asmIn{b.I64.ZExtI32(off), "r"})
		if r == nil {
			return u.poison(out)
		}
		return value{r, out}

	// ---- barriers and fences -----------------------------------------------
	//
	// The three _Read/_Write ones are compiler barriers: they order nothing
	// in the machine and exist to stop a compiler reordering across them. A
	// single-thread fence is exactly that.

	case "_ReadWriteBarrier", "_ReadBarrier", "_WriteBarrier":
		b.Fence(ir.SeqCst, ir.SingleThread)
		return value{nil, void}

	case "_mm_mfence", "_mm_sfence", "_mm_lfence":
		// A full fence for all three. SFENCE and LFENCE order less, and a
		// stronger barrier is a correct implementation of a weaker one —
		// the reverse would not be.
		b.Fence(ir.SeqCst)
		return value{nil, void}

	// ---- instructions with no value ----------------------------------------

	case "_mm_pause", "__nop", "__debugbreak", "__halt":
		tmpl := map[string]string{
			"_mm_pause": "pause", "__nop": "nop",
			"__debugbreak": "int3", "__halt": "hlt",
		}[name]
		u.blk().Asm(tmpl).Volatile().Emit()
		return value{nil, void}

	case "_mm_prefetch":
		p, ok1 := argPtr(0)
		if len(e.Args) < 2 {
			u.errorf(e, "_mm_prefetch takes two arguments")
			return u.poison(void)
		}
		hint, ok2 := u.constInt(e.Args[1])
		if !ok1 || !ok2 {
			if ok1 {
				u.errorf(e, "_mm_prefetch: the hint must be a constant")
			}
			return u.poison(void)
		}
		// _MM_HINT_T0 is 1, T1 2, T2 3, NTA 0 — Intel's numbering, which is
		// not the instruction's suffix order.
		tmpl := "prefetchnta (%0)"
		switch hint {
		case 1:
			tmpl = "prefetcht0 (%0)"
		case 2:
			tmpl = "prefetcht1 (%0)"
		case 3:
			tmpl = "prefetcht2 (%0)"
		}
		u.blk().Asm(tmpl).Volatile().In(p, ir.CStr("r")).Emit()
		return value{nil, void}

	// ---- the frame ---------------------------------------------------------

	case "_ReturnAddress":
		return value{b.Ptr.ReturnAddr(), &types.Pointer{Elem: types.Typ(types.Void)}}

	case "_alloca":
		return u.allocaBuiltin(e)

	// ---- block moves -------------------------------------------------------
	//
	// REP MOVSB and REP STOSB are a byte copy and a byte fill, which is what
	// §E's verbs are. The instruction is not named here because nothing about
	// these intrinsics asks for it: a caller writes __movsb to move bytes.

	case "__movsb":
		d, ok1 := argPtr(0)
		s, ok2 := argPtr(1)
		n, ok3 := arg64(2)
		if !ok1 || !ok2 || !ok3 {
			return u.poison(void)
		}
		b.MemCpy(d, s, n)
		return value{nil, void}

	case "__stosb":
		d, ok1 := argPtr(0)
		v, ok2 := arg32(1)
		n, ok3 := arg64(2)
		if !ok1 || !ok2 || !ok3 {
			return u.poison(void)
		}
		b.MemSet(d, v, n)
		return value{nil, void}

	// ---- bit test ----------------------------------------------------------

	case "_bittest", "_bittest64",
		"_bittestandset", "_bittestandset64",
		"_bittestandreset", "_bittestandreset64",
		"_bittestandcomplement", "_bittestandcomplement64",
		"_interlockedbittestandset", "_interlockedbittestandreset":
		return u.bitTest(name, e)
	}

	u.errorf(e, "internal: %s is listed as an intrinsic and not lowered", name)
	return u.poison(i32)
}

// bitTest is the BT family: read one bit of the word at a pointer, and for
// the four that modify, write it back.
//
// The word is indexed by the bit number, not just shifted: MSVC's operand is
// a LONG* and a bit offset that may run past thirty-two, exactly as the
// instruction's does. A caller passing a bit within the first word — which is
// almost every caller — pays one add of zero for that.
func (u *unit) bitTest(name string, e *ast.CallExpr) value {
	uchar := types.Typ(types.UChar)
	b := u.blk()

	wide := len(name) > 2 && name[len(name)-2:] == "64"
	atomic := name == "_interlockedbittestandset" || name == "_interlockedbittestandreset"

	if len(e.Args) < 2 {
		u.errorf(e, "%s takes two arguments", name)
		return u.poison(uchar)
	}
	base := u.ptr(u.expr(e.Args[0]).v, e.Args[0])
	if base.IsZero() {
		return u.poison(uchar)
	}
	word := types.Typ(types.Long)
	bits := int64(32)
	if wide {
		word, bits = types.Typ(types.LongLong), 64
	}
	nv := u.convert(u.expr(e.Args[1]), types.Typ(types.LongLong), e.Args[1])
	n, _ := nv.v.(ir.I64)
	if n.Def() == nil {
		return u.poison(uchar)
	}

	// The word this bit lives in, and the bit within it.
	idx := b.I64.SShr(n, b.I64.Const(shiftFor(bits)))
	at := b.Ptr.Add(base, b.I64.Mul(idx, b.I64.Const(bits/8)))
	sh := b.I64.And(n, b.I64.Const(bits-1))

	if atomic {
		// LOCK BTS and LOCK BTR are an OR and an AND-NOT of one bit, which
		// §H already has as read-modify-write verbs answering with the old
		// value — which is the bit this intrinsic returns.
		one := b.I32.Shl(b.I32.Const(1), b.I32.WrapI64(sh))
		var old ir.I32
		if name == "_interlockedbittestandset" {
			old = b.I32.AtomicRmwOr(one, at, ir.SeqCst)
		} else {
			old = b.I32.AtomicRmwAnd(b.I32.Not(one), at, ir.SeqCst)
		}
		bit := b.I32.And(b.I32.UShr(old, b.I32.WrapI64(sh)), b.I32.Const(1))
		return u.convert(value{bit, types.Typ(types.UInt)}, uchar, e)
	}

	old := u.load(lval{addr: at, t: word}, e)
	if old.v == nil {
		return u.poison(uchar)
	}

	var bit, next ir.Value
	if wide {
		o, _ := old.v.(ir.I64)
		one := b.I64.Shl(b.I64.Const(1), sh)
		bit = b.I64.And(b.I64.UShr(o, sh), b.I64.Const(1))
		switch {
		case name == "_bittestandset64":
			next = b.I64.Or(o, one)
		case name == "_bittestandreset64":
			next = b.I64.And(o, b.I64.Not(one))
		case name == "_bittestandcomplement64":
			next = b.I64.Xor(o, one)
		}
	} else {
		o, _ := old.v.(ir.I32)
		s := b.I32.WrapI64(sh)
		one := b.I32.Shl(b.I32.Const(1), s)
		bit = b.I32.And(b.I32.UShr(o, s), b.I32.Const(1))
		switch {
		case name == "_bittestandset":
			next = b.I32.Or(o, one)
		case name == "_bittestandreset":
			next = b.I32.And(o, b.I32.Not(one))
		case name == "_bittestandcomplement":
			next = b.I32.Xor(o, one)
		}
	}
	if next != nil {
		u.store(value{next, word}, at, word, e)
	}
	return u.convert(value{bit, word}, uchar, e)
}

// shiftFor is the log2 of a power of two, for turning a bit index into a word
// index.
func shiftFor(bits int64) int64 {
	n := int64(0)
	for bits > 1 {
		bits >>= 1
		n++
	}
	return n
}

// asmIn is one input to the one-output asm helpers below.
type asmIn struct {
	v   ir.Value
	con string
}

// asm1 emits an asm statement with one output and the given inputs, and
// returns the output.
//
// It exists because an intrinsic that names an instruction is a one-line asm
// statement, and writing that out six times would bury what each one is. The
// template is GCC's spelling, which is what §G4 carries.
func (u *unit) asm1(template string, out ir.RegType, con string, ins ...asmIn) ir.Value {
	return u.asm1v(template, out, con, ins...)
}

func (u *unit) asm1v(template string, out ir.RegType, con string, ins ...asmIn) ir.Value {
	st := u.blk().Asm(template).Out(out, ir.CStr(con))
	for _, in := range ins {
		if in.v == nil {
			return nil
		}
		st = st.In(in.v, ir.CStr(in.con))
	}
	res := st.Emit()
	if res.Len() != 1 {
		return nil
	}
	return res.Value(0)
}

// storeIf writes v through p only where cond holds.
//
// Branch-free: the old contents are read back and selected against, so the
// store happens either way and writes what was already there when it should
// not have happened. That is safe because the address is one the caller
// handed over as a destination — it is writable, and nothing else may be
// racing on it, which is the same assumption the store itself makes.
func (u *unit) storeIf(cond ir.I1, v value, p ir.Ptr, t types.Type, at ast.Node) {
	old := u.load(lval{addr: p, t: t}, at)
	if old.v == nil || v.v == nil {
		return
	}
	b := u.blk()
	nv, _ := v.v.(ir.I32)
	ov, _ := old.v.(ir.I32)
	if nv.Def() == nil || ov.Def() == nil {
		return
	}
	u.store(value{b.I32.Select(cond, nv, ov), t}, p, t, at)
}

// namedBitCount is __popcnt and __lzcnt, at each of their widths.
//
// They are the one place this file's rule bends. Both name an instruction —
// POPCNT and LZCNT — and MSVC emits it whichever architecture level is set,
// leaving the CPUID check to the programmer. vcc computes the answer instead,
// with the same baseline sequences __builtin_popcount and __builtin_clz use.
//
// The reason is that the alternative is worse in the way that matters. An
// assembler that refuses POPCNT below x86-64-v2 is right to — the module's
// feature set is a fact about the whole object — so emitting the instruction
// here would mean a program that includes <intrin.h> and calls __popcnt does
// not build without -march. Not building is the one outcome a compatibility
// target cannot have. What is lost is a few instructions on a target that,
// by saying nothing, asked for the 2003 baseline.
func (u *unit) namedBitCount(name string, e *ast.CallExpr) value {
	u64 := types.Typ(types.ULongLong)
	u32 := types.Typ(types.UInt)
	wide := name == "__popcnt64" || name == "__lzcnt64"
	narrow := name == "__popcnt16" || name == "__lzcnt16"
	lz := name == "__lzcnt" || name == "__lzcnt16" || name == "__lzcnt64"

	out, width := u32, int64(32)
	if wide {
		out, width = u64, 64
	}
	if len(e.Args) < 1 {
		u.errorf(e, "%s takes one argument", name)
		return u.poison(out)
	}
	in := u32
	if wide {
		in = u64
	} else if narrow {
		in = types.Typ(types.UShort)
	}
	v := u.convert(u.expr(e.Args[0]), in, e.Args[0])
	if v.v == nil {
		return u.poison(out)
	}
	op := "popcount"
	if lz {
		op = "clz"
	}
	// The sixteen-bit forms count within sixteen bits: the value is widened
	// to a register, so the sixteen zeroes above it are not leading zeroes
	// and the count has to come back down.
	r := u.bitCountRuntime(e, op, value{v.v, u.promote(in)}, width)
	if r.v == nil {
		return u.poison(out)
	}
	if narrow && lz {
		b := u.blk()
		x, _ := u.convert(r, types.Typ(types.Int), e).v.(ir.I32)
		if x.Def() == nil {
			return u.poison(out)
		}
		r = value{b.I32.Sub(x, b.I32.Const(16)), types.Typ(types.Int)}
	}
	return u.convert(r, out, e)
}
