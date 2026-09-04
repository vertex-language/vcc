package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// _Atomic objects.
//
// §6.7.3p3 makes every access to an _Atomic object atomic, and §5.1.2.4 gives
// it sequentially consistent ordering unless a <stdatomic.h> generic function
// asked for another one. So a read is an atomic load, a write is an atomic
// store, and a compound assignment or ++ is one indivisible read-modify-write
// rather than a load, an operation, and a store.
//
// This covers the language feature: the _Atomic qualifier on a scalar. It does
// not implement <stdatomic.h>, whose generic functions carry an explicit
// ordering and need a builtin of their own.
//
// An aggregate is left out deliberately rather than lowered wrongly. C admits
// _Atomic struct, but no machine has an instruction for it: it is a lock in
// libatomic, and a lock this compiler does not link is worse than a
// diagnostic.

// atomicPlan is how one _Atomic scalar is accessed: the width to use, and
// whether the value has to be moved between an integer register and a float
// one on the way.
type atomicPlan struct {
	store  ir.StoreType // the width of the access
	wide   bool         // the integer verb lives in the i64 namespace
	isPtr  bool         // the value is a pointer, and ptr's own verbs apply
	isFlt  bool         // the value is a float and travels through a bitcast
	bits   int64        // significant bits, for a narrow integer
	signed bool
}

// atomicPlanFor decides how to access an _Atomic lvalue, and reports why it
// cannot where it cannot.
func (u *unit) atomicPlanFor(l lval, at ast.Node) (atomicPlan, bool) {
	t := types.Unqualify(l.t)
	if _, ok := asRecord(t); ok {
		u.errorf(at, "an atomic access to %s would need a lock; vcc lowers _Atomic scalars only", l.t)
		return atomicPlan{}, false
	}
	if l.bit {
		// §6.7.2.1p5 forbids an _Atomic bit-field outright; say so rather
		// than emitting a read-modify-write that is not atomic.
		u.errorf(at, "a bit-field may not be _Atomic")
		return atomicPlan{}, false
	}
	st, ok := u.types.storeType(t)
	if !ok {
		u.errorf(at, "cannot atomically access a value of type %s", l.t)
		return atomicPlan{}, false
	}
	p := atomicPlan{store: st}
	switch st {
	case ir.StorePtr:
		p.isPtr = true
	case ir.StoreF32:
		p.isFlt, p.store, p.bits = true, ir.StoreI32, 32
	case ir.StoreF64:
		p.isFlt, p.wide, p.store, p.bits = true, true, ir.StoreI64, 64
	case ir.StoreF80, ir.StoreF128:
		// No target this tree addresses has an atomic access that wide.
		u.errorf(at, "%s is wider than any atomic access this target has", l.t)
		return atomicPlan{}, false
	case ir.StoreI8, ir.StoreI16, ir.StoreI32, ir.StoreI64:
		p.bits, p.signed = u.model.IntBits(t)
		if isBoolType(t) {
			// _Bool has no register width of its own; it is one byte holding
			// 0 or 1, and the 8-bit verbs are what reach it.
			p.store, p.bits, p.signed = ir.StoreI8, 8, false
		}
		p.wide = st == ir.StoreI64
	default:
		u.errorf(at, "cannot atomically access a value of type %s", l.t)
		return atomicPlan{}, false
	}
	return p, true
}

// atomicLoad reads an _Atomic object.
func (u *unit) atomicLoad(l lval, at ast.Node) value {
	p, ok := u.atomicPlanFor(l, at)
	if !ok {
		return u.poison(l.t)
	}
	b := u.blk()
	switch {
	case p.isPtr:
		return value{b.Ptr.AtomicLoad(l.addr, ir.SeqCst), l.t}
	case p.wide:
		x := b.I64.AtomicLoad(l.addr, ir.SeqCst)
		if p.isFlt {
			return value{b.F64.BitcastI64(x), l.t}
		}
		return value{x, l.t}
	}
	var x ir.I32
	switch p.store {
	case ir.StoreI8:
		x = b.I32.AtomicULoad8(l.addr, ir.SeqCst)
	case ir.StoreI16:
		x = b.I32.AtomicULoad16(l.addr, ir.SeqCst)
	default:
		x = b.I32.AtomicLoad(l.addr, ir.SeqCst)
	}
	if p.isFlt {
		return value{b.F32.BitcastI32(x), l.t}
	}
	// The narrow verbs zero-extend; a value narrower than its register is
	// held the way its own signedness says, so a signed one is put back.
	if p.bits < 32 {
		return value{u.narrow(b, x, p.bits, p.signed), l.t}
	}
	return value{x, l.t}
}

// atomicStore writes an _Atomic object.
func (u *unit) atomicStore(v value, l lval, at ast.Node) {
	p, ok := u.atomicPlanFor(l, at)
	if !ok {
		return
	}
	t := types.Unqualify(l.t)
	cv := u.convert(v, t, at)
	if cv.v == nil {
		return
	}
	b := u.blk()
	switch {
	case p.isPtr:
		b.Ptr.AtomicStore(u.ptr(cv.v, at), l.addr, ir.SeqCst)
	case p.wide:
		x := cv.v
		if p.isFlt {
			x = b.I64.BitcastF64(cv.v.(ir.F64))
		}
		b.I64.AtomicStore(u.i64(x, at), l.addr, ir.SeqCst)
	default:
		var x ir.I32
		if p.isFlt {
			x = b.I32.BitcastF32(cv.v.(ir.F32))
		} else {
			x = u.i32(cv.v, at)
		}
		switch p.store {
		case ir.StoreI8:
			b.I32.AtomicStore8(x, l.addr, ir.SeqCst)
		case ir.StoreI16:
			b.I32.AtomicStore16(x, l.addr, ir.SeqCst)
		default:
			b.I32.AtomicStore(x, l.addr, ir.SeqCst)
		}
	}
}

// atomicRmwVerb names the read-modify-write a compound assignment maps to,
// where one exists. Everything else — multiplication, division, the shifts —
// has no such instruction and goes round the compare-and-swap loop instead.
func atomicRmwVerb(op token.Kind) (token.Kind, bool) {
	switch op {
	case token.ADD, token.SUB, token.AND, token.OR, token.XOR:
		return op, true
	case token.ASSIGN:
		// Not a compound assignment: this is <stdatomic.h>'s exchange, an
		// unconditional swap that still has to report what was there.
		return op, true
	}
	return op, false
}

// atomicUpdate performs one indivisible read-modify-write and returns the old
// and new values, both typed as the object.
//
// §6.5.16.2p3: a compound assignment to an _Atomic object is a single atomic
// operation, not a load followed by a store. Doing it as three steps would let
// another thread's write land between them and be lost, which is the whole
// thing the qualifier is asked for.
func (u *unit) atomicUpdate(op token.Kind, l lval, rhs value, at ast.Node) (old, updated value) {
	p, ok := u.atomicPlanFor(l, at)
	if !ok {
		return u.poison(l.t), u.poison(l.t)
	}
	t := types.Unqualify(l.t)

	// A pointer's += counts in elements, and the scale belongs to the delta
	// rather than to the pointer, so it is applied before the exchange.
	if p.isPtr {
		if op == token.ASSIGN {
			b := u.blk()
			cv := u.convert(rhs, t, at)
			if cv.v == nil {
				return u.poison(l.t), u.poison(l.t)
			}
			old := b.Ptr.AtomicRmwXchg(u.ptr(cv.v, at), l.addr, ir.SeqCst)
			return value{old, l.t}, cv
		}
		return u.atomicPtrUpdate(op, l, rhs, at)
	}
	if v, direct := atomicRmwVerb(op); direct && !p.isFlt {
		return u.atomicRmw(v, p, l, rhs, at)
	}
	if op == token.ASSIGN {
		// A float exchange: swap the bits, then read them back as a float.
		return u.atomicFloatXchg(p, l, rhs, at)
	}
	return u.atomicCasLoop(op, p, l, rhs, at, t)
}

// atomicRmw emits the one-instruction form.
func (u *unit) atomicRmw(op token.Kind, p atomicPlan, l lval, rhs value, at ast.Node) (value, value) {
	b := u.blk()
	t := types.Unqualify(l.t)
	cv := u.convert(rhs, t, at)
	if cv.v == nil {
		return u.poison(l.t), u.poison(l.t)
	}
	var oldv ir.Value
	if p.wide {
		x := u.i64(cv.v, at)
		switch op {
		case token.ADD:
			oldv = b.I64.AtomicRmwAdd(x, l.addr, ir.SeqCst)
		case token.SUB:
			oldv = b.I64.AtomicRmwSub(x, l.addr, ir.SeqCst)
		case token.AND:
			oldv = b.I64.AtomicRmwAnd(x, l.addr, ir.SeqCst)
		case token.OR:
			oldv = b.I64.AtomicRmwOr(x, l.addr, ir.SeqCst)
		case token.XOR:
			oldv = b.I64.AtomicRmwXor(x, l.addr, ir.SeqCst)
		case token.ASSIGN:
			oldv = b.I64.AtomicRmwXchg(x, l.addr, ir.SeqCst)
		}
	} else {
		x := u.i32(cv.v, at)
		oldv = u.atomicRmw32(op, p, x, l, at)
	}
	if oldv == nil {
		return u.poison(l.t), u.poison(l.t)
	}
	oldVal := value{oldv, l.t}
	if !p.wide && p.bits < 32 {
		oldVal = value{u.narrow(b, oldv, p.bits, p.signed), l.t}
	}
	if op == token.ASSIGN {
		return oldVal, cv
	}
	// The new value is the operation applied to the old one. It is recomputed
	// rather than read back: reading again would be a second access, and a
	// second access is not part of this one.
	nv := u.convert(u.arith(op, oldVal, cv, at), t, at)
	return oldVal, nv
}

// atomicFloatXchg swaps a float object's bits and reads the old ones back as
// a float. The integer exchange is the only atomic swap a machine has.
func (u *unit) atomicFloatXchg(p atomicPlan, l lval, rhs value, at ast.Node) (value, value) {
	b := u.blk()
	t := types.Unqualify(l.t)
	cv := u.convert(rhs, t, at)
	if cv.v == nil {
		return u.poison(l.t), u.poison(l.t)
	}
	if p.wide {
		bits := b.I64.BitcastF64(cv.v.(ir.F64))
		old := b.I64.AtomicRmwXchg(bits, l.addr, ir.SeqCst)
		return value{b.F64.BitcastI64(old), l.t}, cv
	}
	bits := b.I32.BitcastF32(cv.v.(ir.F32))
	old := b.I32.AtomicRmwXchg(bits, l.addr, ir.SeqCst)
	return value{b.F32.BitcastI32(old), l.t}, cv
}

// atomicRmw32 picks the narrow verb for an 8-, 16- or 32-bit access.
func (u *unit) atomicRmw32(op token.Kind, p atomicPlan, x ir.I32, l lval, at ast.Node) ir.Value {
	b := u.blk()
	switch p.store {
	case ir.StoreI8:
		switch op {
		case token.ADD:
			return b.I32.AtomicRmwAdd8(x, l.addr, ir.SeqCst)
		case token.SUB:
			return b.I32.AtomicRmwSub8(x, l.addr, ir.SeqCst)
		case token.AND:
			return b.I32.AtomicRmwAnd8(x, l.addr, ir.SeqCst)
		case token.OR:
			return b.I32.AtomicRmwOr8(x, l.addr, ir.SeqCst)
		case token.XOR:
			return b.I32.AtomicRmwXor8(x, l.addr, ir.SeqCst)
		case token.ASSIGN:
			return b.I32.AtomicRmwXchg8(x, l.addr, ir.SeqCst)
		}
	case ir.StoreI16:
		switch op {
		case token.ADD:
			return b.I32.AtomicRmwAdd16(x, l.addr, ir.SeqCst)
		case token.SUB:
			return b.I32.AtomicRmwSub16(x, l.addr, ir.SeqCst)
		case token.AND:
			return b.I32.AtomicRmwAnd16(x, l.addr, ir.SeqCst)
		case token.OR:
			return b.I32.AtomicRmwOr16(x, l.addr, ir.SeqCst)
		case token.XOR:
			return b.I32.AtomicRmwXor16(x, l.addr, ir.SeqCst)
		case token.ASSIGN:
			return b.I32.AtomicRmwXchg16(x, l.addr, ir.SeqCst)
		}
	default:
		switch op {
		case token.ADD:
			return b.I32.AtomicRmwAdd(x, l.addr, ir.SeqCst)
		case token.SUB:
			return b.I32.AtomicRmwSub(x, l.addr, ir.SeqCst)
		case token.AND:
			return b.I32.AtomicRmwAnd(x, l.addr, ir.SeqCst)
		case token.OR:
			return b.I32.AtomicRmwOr(x, l.addr, ir.SeqCst)
		case token.XOR:
			return b.I32.AtomicRmwXor(x, l.addr, ir.SeqCst)
		case token.ASSIGN:
			return b.I32.AtomicRmwXchg(x, l.addr, ir.SeqCst)
		}
	}
	return nil
}

// atomicPtrUpdate is += and -= on an _Atomic pointer, where the delta is in
// elements and the instruction counts bytes.
func (u *unit) atomicPtrUpdate(op token.Kind, l lval, rhs value, at ast.Node) (value, value) {
	b := u.blk()
	if op != token.ADD && op != token.SUB {
		u.errorf(at, "%s is not an operation on a pointer", op)
		return u.poison(l.t), u.poison(l.t)
	}
	pt, _ := asPointer(types.Unqualify(l.t))
	esz := int64(1)
	if pt != nil {
		if n, ok := u.model.Sizeof(types.Unqualify(pt.Elem)); ok && n > 0 {
			esz = n
		}
	}
	n := u.i64(u.convert(rhs, types.Typ(types.LongLong), at).v, at)
	if n.IsZero() {
		return u.poison(l.t), u.poison(l.t)
	}
	delta := b.I64.Mul(n, b.I64.Const(esz))
	var oldp ir.Ptr
	if op == token.ADD {
		oldp = b.Ptr.AtomicRmwAdd(delta, l.addr, ir.SeqCst)
	} else {
		oldp = b.Ptr.AtomicRmwSub(delta, l.addr, ir.SeqCst)
	}
	old := value{oldp, l.t}
	nv := value{b.Ptr.Add(oldp, signedDelta(b, delta, op)), l.t}
	return old, nv
}

// signedDelta is the byte displacement a pointer update adds, with subtraction
// expressed as adding the negation.
func signedDelta(b *ir.Block, delta ir.I64, op token.Kind) ir.I64 {
	if op == token.SUB {
		return b.I64.Sub(b.I64.Const(0), delta)
	}
	return delta
}

// atomicCasLoop is the general read-modify-write: read, compute, and swap the
// result in only if nothing changed underneath, retrying until it does.
//
// This is what an operation with no instruction of its own gets — *=, /=, the
// shifts, and anything on a float — and it has the same observable behaviour
// as the single-instruction form.
func (u *unit) atomicCasLoop(op token.Kind, p atomicPlan, l lval, rhs value,
	at ast.Node, t types.Type) (value, value) {

	f := u.fn
	if f == nil {
		u.errorf(at, "internal: an atomic update outside a function")
		return u.poison(l.t), u.poison(l.t)
	}
	// The operand is evaluated once, before the loop: §6.5.16.2p3 evaluates
	// each operand once however many times the swap has to be retried.
	cv := u.convert(rhs, t, at)
	if cv.v == nil {
		return u.poison(l.t), u.poison(l.t)
	}

	// The two slots hold the loop's own state. They are unqualified on
	// purpose: they are this thread's frame, nothing else can see them, and
	// typing them _Atomic would put a load-acquire and a store-release on
	// every turn of a loop that needs neither.
	oldSlot := u.alloca(t, "atomic_old", at)
	newSlot := u.alloca(t, "atomic_new", at)

	loop := f.fn.Block(u.uniq("atomic_loop"))
	done := f.fn.Block(u.uniq("atomic_done"))

	// Seed the loop with one ordinary atomic read.
	seed := u.atomicLoad(l, at)
	u.store(seed, oldSlot, t, at)
	u.blk().Br(loop.To())

	u.enter(loop)
	cur := u.load(lval{addr: oldSlot, t: t}, at)
	next := u.convert(u.arith(op, cur, cv, at), t, at)
	u.store(next, newSlot, t, at)

	seen := u.atomicCas(p, l, cur, next, at)
	if seen.v == nil {
		u.blk().Br(done.To())
		u.enter(done)
		return u.poison(l.t), u.poison(l.t)
	}
	// The swap took if what the instruction read is what it was told to
	// expect. Otherwise that value is the new starting point, which is why it
	// is stored back rather than re-read.
	u.store(seen, oldSlot, t, at)
	again := u.emitCompare(u.blk(), seen.v, cur.v, t, token.NEQ, at)
	u.blk().BrIf(again, loop.To(), done.To())

	u.enter(done)
	return u.load(lval{addr: oldSlot, t: t}, at), u.load(lval{addr: newSlot, t: t}, at)
}

// atomicCas emits one compare-and-swap and returns the value it read.
func (u *unit) atomicCas(p atomicPlan, l lval, expect, next value, at ast.Node) value {
	b := u.blk()
	switch {
	case p.isPtr:
		return value{b.Ptr.AtomicCas(u.ptr(expect.v, at), u.ptr(next.v, at), l.addr,
			ir.SeqCst, ir.SeqCst), l.t}
	case p.wide:
		e, n := expect.v, next.v
		if p.isFlt {
			e = b.I64.BitcastF64(e.(ir.F64))
			n = b.I64.BitcastF64(n.(ir.F64))
		}
		x := b.I64.AtomicCas(u.i64(e, at), u.i64(n, at), l.addr, ir.SeqCst, ir.SeqCst)
		if p.isFlt {
			return value{b.F64.BitcastI64(x), l.t}
		}
		return value{x, l.t}
	}
	var e, n ir.I32
	if p.isFlt {
		e = b.I32.BitcastF32(expect.v.(ir.F32))
		n = b.I32.BitcastF32(next.v.(ir.F32))
	} else {
		e, n = u.i32(expect.v, at), u.i32(next.v, at)
	}
	var x ir.I32
	switch p.store {
	case ir.StoreI8:
		x = b.I32.AtomicCas8(e, n, l.addr, ir.SeqCst, ir.SeqCst)
	case ir.StoreI16:
		x = b.I32.AtomicCas16(e, n, l.addr, ir.SeqCst, ir.SeqCst)
	default:
		x = b.I32.AtomicCas(e, n, l.addr, ir.SeqCst, ir.SeqCst)
	}
	if p.isFlt {
		return value{b.F32.BitcastI32(x), l.t}
	}
	if p.bits < 32 {
		return value{u.narrow(b, x, p.bits, p.signed), l.t}
	}
	return value{x, l.t}
}

// ---- gcc's __sync_* family ----
//
// The dispatch is in gnu.go, which says why these exist. The three helpers
// are here because what they do is what this file does: one indivisible
// read-modify-write, one compare-and-swap, one release store.

// syncFetch is __sync_fetch_and_OP and __sync_OP_and_fetch — the same
// operation twice, differing only in which of the two values it answers
// with. token.ASSIGN is __sync_lock_test_and_set, the exchange.
func (u *unit) syncFetch(e *ast.CallExpr, op token.Kind, returnsNew bool) value {
	if len(e.Args) < 2 {
		u.errorf(e, "this builtin takes a pointer and a value")
		return u.poison(types.Typ(types.Int))
	}
	l, ok := u.atomicTarget(e, e.Args[0])
	if !ok {
		return u.poison(types.Typ(types.Int))
	}
	t := types.Unqualify(l.t)
	old, updated := u.atomicUpdate(op, l, u.expr(e.Args[1]), e)
	if returnsNew {
		return value{updated.v, t}
	}
	return value{old.v, t}
}

// syncCas is __sync_val_compare_and_swap, which answers with the value the
// object held, and __sync_bool_compare_and_swap, which answers with whether
// that value was the one asked for. One compare-and-swap either way: the
// bool form is the comparison the swap already made, read back.
func (u *unit) syncCas(e *ast.CallExpr, asBool bool) value {
	intT := types.Typ(types.Int)
	if len(e.Args) < 3 {
		u.errorf(e, "a compare-and-swap takes a pointer, the value expected, and the value to write")
		return u.poison(intT)
	}
	l, ok := u.atomicTarget(e, e.Args[0])
	if !ok {
		return u.poison(intT)
	}
	p, ok := u.atomicPlanFor(l, e)
	if !ok {
		return u.poison(intT)
	}
	t := types.Unqualify(l.t)
	expect := u.convert(u.expr(e.Args[1]), t, e)
	next := u.convert(u.expr(e.Args[2]), t, e)
	if expect.v == nil || next.v == nil {
		return u.poison(t)
	}
	read := u.atomicCas(p, l, expect, next, e)
	if !asBool {
		return value{read.v, t}
	}
	b := u.blk()
	c := u.emitCompare(b, read.v, expect.v, t, token.EQL, e)
	if c.IsZero() {
		return u.poison(intT)
	}
	return value{b.I32.ZExtI1(c), intT}
}

// syncRelease is __sync_lock_release: store zero, indivisibly. It is
// documented as a release barrier; vcc's store is sequentially consistent,
// which is stronger and correct.
func (u *unit) syncRelease(e *ast.CallExpr) value {
	void := types.Typ(types.Void)
	if len(e.Args) < 1 {
		u.errorf(e, "__sync_lock_release takes a pointer")
		return u.poison(void)
	}
	l, ok := u.atomicTarget(e, e.Args[0])
	if !ok {
		return u.poison(void)
	}
	u.atomicStore(u.intConst(0, types.Unqualify(l.t)), l, e)
	return value{nil, void}
}
