package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// The builtins vcc's own headers need, and nothing else.
//
// This list grows only from <stdarg.h>, <stddef.h>, and kin — the headers the
// compiler ships because they describe the compiler rather than the library.
// A builtin that exists to make third-party code compile is a dialect, and
// vcc is not a dialect host.

// builtin recognizes and lowers a call to a compiler builtin. The bool is
// false for an ordinary call, which is then lowered normally.
func (u *unit) builtin(e *ast.CallExpr) (value, bool) {
	id, ok := stripParens(e.Fun).(*ast.Ident)
	if !ok {
		return value{}, false
	}
	name := u.name(id)

	// The platform's own intrinsics, which its headers declare and no
	// library defines. They are recognized before the shadowing rule below
	// because that declaration is exactly what they have: see msvc.go.
	if in, ok := interlockedOps[name]; ok && u.layout.ABI == "ms" {
		return u.interlockedCall(name, in, e), true
	}

	// The vector intrinsics, for the same reason and under the same rule:
	// <emmintrin.h> declares them, nothing defines them, and a compiler that
	// lowered one as an ordinary call would compile a program that does not
	// link. The layout block is what says the target has the register file
	// they name — see sse.go.
	if u.layout.Vector && sseIntrinsic(name) {
		return u.sseCall(name, e), true
	}

	// <intrin.h>'s family, on the same terms — see intrin.go. Gated on the
	// convention rather than on the architecture because that is what the
	// header is: a Windows program includes <intrin.h>, and a program
	// elsewhere that happens to define a function called _rotl means its own.
	if u.layout.ABI == "ms" && intrinsic(name) {
		return u.intrinCall(name, e), true
	}

	if o := u.lookup(name); o != nil && name != "__va_start" {
		return value{}, false // a real declaration shadows the builtin
	}
	switch name {
	case "__builtin_va_start", "__va_start":
		return u.vaStart(e), true
	case "__builtin_va_end":
		return u.vaEnd(e), true
	case "__builtin_va_copy":
		return u.vaCopy(e), true
	case "__builtin_va_arg_ref":
		return u.vaArgRef(e), true
	case "__builtin_trap":
		u.blk().Trap()
		u.leave()
		return value{nil, types.Typ(types.Void)}, true
	case "__builtin_unreachable":
		// §L: there is no unreachable, and a path a frontend believes cannot
		// be taken ends in trap. One instruction, and a defined outcome when
		// the belief is wrong.
		u.blk().Trap()
		u.leave()
		return value{nil, types.Typ(types.Void)}, true
	case "__assume":
		// MSVC's optimizer hint. __assume(0) is the MSVC spelling of
		// __builtin_unreachable — it tells the compiler the path cannot
		// be taken. Any other constant argument is a no-op hint: the
		// compiler may use it to improve optimization but need not.
		if len(e.Args) > 0 {
			if v, ok := u.constInt(e.Args[0]); ok && v == 0 {
				u.blk().Trap()
				u.leave()
			}
			// Non-zero constant or non-constant: optimizer hint, discard.
		}
		return value{nil, types.Typ(types.Void)}, true
	case "__noop":
		// MSVC's no-operation builtin. Arguments are syntax-checked by
		// the parser but never evaluated — no side effects. Used in
		// release-mode debug macros: #define LOG(...) __noop(__VA_ARGS__)
		return value{nil, types.Typ(types.Void)}, true
	case "__builtin_expect":
		// No branch-weight metadata kind is specified yet (§K reserves it),
		// so the hint is dropped and the value passes through.
		if len(e.Args) > 0 {
			return u.expr(e.Args[0]), true
		}
	case "__builtin_offsetof_fold":
		if v, ok := u.constInt(e); ok {
			return u.intConst(v, u.sizeType()), true
		}

	// <stdatomic.h>'s generic functions. §7.17 defines them over an object
	// of any atomic type, which no C declaration expresses, and three of them
	// need the value the object held before the operation — which an
	// expression written in C cannot name. Everything else in that header is
	// ordinary C over the _Atomic operators and lives in the header itself.
	case "__builtin_atomic_exchange":
		return u.atomicBuiltinRmw(e, token.ASSIGN), true
	case "__builtin_atomic_fetch_add":
		return u.atomicBuiltinRmw(e, token.ADD), true
	case "__builtin_atomic_fetch_sub":
		return u.atomicBuiltinRmw(e, token.SUB), true
	case "__builtin_atomic_fetch_and":
		return u.atomicBuiltinRmw(e, token.AND), true
	case "__builtin_atomic_fetch_or":
		return u.atomicBuiltinRmw(e, token.OR), true
	case "__builtin_atomic_fetch_xor":
		return u.atomicBuiltinRmw(e, token.XOR), true
	case "__builtin_atomic_compare_exchange":
		return u.atomicBuiltinCas(e), true
	case "__builtin_atomic_fence":
		u.blk().Fence(ir.SeqCst)
		return value{nil, types.Typ(types.Void)}, true
	case "__builtin_atomic_signal_fence":
		u.blk().Fence(ir.SeqCst, ir.SingleThread)
		return value{nil, types.Typ(types.Void)}, true

	// clang spells the same operations __c11_atomic_*, and its
	// <stdatomic.h> is written in terms of them — so a .i file preprocessed
	// by clang carries these whatever header the program included. Each
	// takes a trailing memory order that vcc strengthens to sequentially
	// consistent, exactly as its own header does.
	case "__c11_atomic_init":
		return u.c11AtomicInit(e), true
	case "__c11_atomic_load":
		return u.c11AtomicLoad(e), true
	case "__c11_atomic_store":
		return u.c11AtomicStore(e), true
	case "__c11_atomic_exchange":
		return u.atomicBuiltinRmw(e, token.ASSIGN), true
	case "__c11_atomic_fetch_add":
		return u.atomicBuiltinRmw(e, token.ADD), true
	case "__c11_atomic_fetch_sub":
		return u.atomicBuiltinRmw(e, token.SUB), true
	case "__c11_atomic_fetch_and":
		return u.atomicBuiltinRmw(e, token.AND), true
	case "__c11_atomic_fetch_or":
		return u.atomicBuiltinRmw(e, token.OR), true
	case "__c11_atomic_fetch_xor":
		return u.atomicBuiltinRmw(e, token.XOR), true
	case "__c11_atomic_compare_exchange_strong", "__c11_atomic_compare_exchange_weak":
		return u.atomicBuiltinCas(e), true
	case "__c11_atomic_thread_fence":
		u.blk().Fence(ir.SeqCst)
		return value{nil, types.Typ(types.Void)}, true
	case "__c11_atomic_signal_fence":
		u.blk().Fence(ir.SeqCst, ir.SingleThread)
		return value{nil, types.Typ(types.Void)}, true
	case "__c11_atomic_is_lock_free", "__atomic_is_lock_free":
		// Every atomic type vcc lowers is lock-free; the aggregate case is
		// refused outright, so there is none that is sometimes.
		for _, a := range e.Args {
			u.discard(a)
		}
		return u.intConst(1, types.Typ(types.Int)), true
	}
	// The GCC builtins are a table of their own, in gnu.go.
	return u.gnuBuiltin(name, e)
}

// atomicTarget resolves a builtin's first argument — a pointer to the atomic
// object — to the lvalue the atomic verbs act on.
func (u *unit) atomicTarget(e *ast.CallExpr, arg ast.Expr) (lval, bool) {
	v := u.expr(arg)
	p, ok := asPointer(types.Unqualify(v.t))
	if !ok {
		u.errorf(e, "%s expects a pointer to an atomic object, not %s", u.name(stripParens(e.Fun).(*ast.Ident)), v.t)
		return lval{}, false
	}
	addr := u.ptr(v.v, e)
	if addr.IsZero() {
		return lval{}, false
	}
	return lval{addr: addr, t: p.Elem}, true
}

// atomicBuiltinRmw is atomic_exchange and the atomic_fetch_* family: one
// indivisible read-modify-write whose value is what the object held before.
//
// token.ASSIGN means the exchange, which keeps no part of the old value.
func (u *unit) atomicBuiltinRmw(e *ast.CallExpr, op token.Kind) value {
	if len(e.Args) < 2 {
		u.errorf(e, "an atomic read-modify-write needs an object and a value")
		return u.poison(types.Typ(types.Int))
	}
	l, ok := u.atomicTarget(e, e.Args[0])
	if !ok {
		return u.poison(types.Typ(types.Int))
	}
	rhs := u.expr(e.Args[1])
	t := types.Unqualify(l.t)
	if op == token.ASSIGN {
		// An exchange is a swap: read the old value and write the new one,
		// indivisibly. It is the compare-and-swap loop with a body that
		// ignores what it read.
		old, _ := u.atomicUpdate(token.ASSIGN, l, rhs, e)
		return value{old.v, t}
	}
	old, _ := u.atomicUpdate(op, l, rhs, e)
	return value{old.v, t}
}

// atomicBuiltinCas is atomic_compare_exchange_strong and _weak.
//
// It returns 1 when the swap happened. On failure it writes what the object
// actually held into *expected, which is what lets a caller loop on it
// without a second read — and which is why this cannot be a C expression.
func (u *unit) atomicBuiltinCas(e *ast.CallExpr) value {
	intT := types.Typ(types.Int)
	if len(e.Args) < 3 {
		u.errorf(e, "a compare-and-exchange needs an object, an expected value, and a desired one")
		return u.poison(intT)
	}
	l, ok := u.atomicTarget(e, e.Args[0])
	if !ok {
		return u.poison(intT)
	}
	expPtr, ok := u.atomicTarget(e, e.Args[1])
	if !ok {
		return u.poison(intT)
	}
	t := types.Unqualify(l.t)
	p, ok := u.atomicPlanFor(l, e)
	if !ok {
		return u.poison(intT)
	}
	expect := u.convert(u.load(expPtr, e), t, e)
	desired := u.convert(u.expr(e.Args[2]), t, e)
	if expect.v == nil || desired.v == nil {
		return u.poison(intT)
	}
	seen := u.atomicCas(p, l, expect, desired, e)
	if seen.v == nil {
		return u.poison(intT)
	}
	// Report success, and hand the caller what was actually there. The write
	// back is unconditional: on success it stores the value that was already
	// expected, which changes nothing.
	b := u.blk()
	same := u.emitCompare(b, seen.v, expect.v, t, token.EQL, e)
	u.storeLval(value{seen.v, t}, expPtr, e)
	return value{b.I32.ZExtI1(same), intT}
}

func (u *unit) vaStart(e *ast.CallExpr) value {
	if len(e.Args) < 1 {
		u.errorf(e, "__builtin_va_start needs the va_list")
		return value{nil, types.Typ(types.Void)}
	}
	ap := u.vaList(e.Args[0], e)
	u.blk().VaStart(ap)
	return value{nil, types.Typ(types.Void)}
}

func (u *unit) vaEnd(e *ast.CallExpr) value {
	if len(e.Args) < 1 {
		return value{nil, types.Typ(types.Void)}
	}
	u.blk().VaEnd(u.vaList(e.Args[0], e))
	return value{nil, types.Typ(types.Void)}
}

func (u *unit) vaCopy(e *ast.CallExpr) value {
	if len(e.Args) < 2 {
		return value{nil, types.Typ(types.Void)}
	}
	u.blk().VaCopy(u.vaList(e.Args[0], e), u.vaList(e.Args[1], e))
	return value{nil, types.Typ(types.Void)}
}

// vaList yields the pointer the va_* verbs take.
//
// vcc's <stdarg.h> declares va_list as a one-element array type, so an
// argument of that type decays to the address of its storage and the verbs
// get what they want without the macro needing to write &.
// vaList resolves the va_list operand of a varargs builtin to the address of
// the list object, which is what §I's verbs take.
//
// Two spellings arrive here. vcc's own <stdarg.h> writes va_start(ap, last)
// as __builtin_va_start(&(ap)) and hands over the address. clang's writes
// __builtin_va_start(ap, last) and hands over the list itself — and a .i file
// carries whichever header produced it, so both have to work. They are told
// apart by type: vcc's va_list is void *, so the address of one is void **.
func (u *unit) vaList(e ast.Expr, at ast.Node) ir.Ptr {
	if t := u.staticType(e); t != nil {
		if p, ok := asPointer(types.Unqualify(t)); ok {
			if _, inner := asPointer(types.Unqualify(p.Elem)); !inner {
				// A pointer to something that is not a pointer: the list
				// itself, passed by value. Its address is what is wanted.
				if lv := u.lvalue(e); !lv.addr.IsZero() {
					return lv.addr
				}
			}
		}
	}
	v := u.expr(e)
	return u.ptr(v.v, at)
}

// vaArgRef is the va_arg builtin, spelled to fit C's grammar.
//
// <stdarg.h> writes:
//
//	#define va_arg(ap, T) (*(T *)__builtin_va_arg_ref(ap, (T *)0))
//
// The second argument is never evaluated: its *type* carries T, which is the
// only way to hand a type to a builtin without a parser carve-out. What comes
// back is a pointer to storage holding the argument, which the macro's cast
// and dereference turn into a value of T.
//
// An aggregate uses ptr.va_arg_ref directly, which advances the list and
// yields the argument's address in place. A scalar uses the typed va_arg verb
// and gets a frame temporary, because §I has no verb that hands back an
// address for a scalar and the macro's shape needs one.
func (u *unit) vaArgRef(e *ast.CallExpr) value {
	voidp := &types.Pointer{Elem: types.Typ(types.Void)}
	if len(e.Args) < 2 {
		u.errorf(e, "__builtin_va_arg_ref needs a va_list and a type witness")
		return u.poison(voidp)
	}
	pt := u.decayType(u.staticType(e.Args[1]))
	p, ok := asPointer(pt)
	if !ok {
		u.errorf(e.Args[1], "the second argument of __builtin_va_arg_ref must be a pointer")
		return u.poison(voidp)
	}
	ref := u.vaArgAt(e.Args[0], p.Elem, e)
	if ref.IsZero() {
		return u.poison(voidp)
	}
	return value{ref, &types.Pointer{Elem: p.Elem}}
}

// vaArgAt fetches the next variadic argument at type t and returns a pointer
// to storage holding it.
//
// A pointer rather than a value because that is what both spellings need: the
// address is the answer for an aggregate, which never travels in a register,
// and a scalar is loaded back out of it. apExpr is the va_list — an object,
// whose address the verbs take.
func (u *unit) vaArgAt(apExpr ast.Expr, elem types.Type, at ast.Node) ir.Ptr {
	ap := u.vaList(apExpr, at)
	if ap.IsZero() {
		return ir.Ptr{}
	}
	t := types.Unqualify(elem)
	b := u.blk()

	if r, isRec := asRecord(t); isRec {
		_ = r
		return b.Ptr.VaArgRef(ap, u.types.record(t.(*types.Record)))
	}

	// The default argument promotions decide which verb applies: there is no
	// f32.va_arg and no narrow integer form, because no such argument can be
	// present in the list.
	fetch := u.defaultPromote(t)
	rt, _ := u.types.regType(fetch)
	var v ir.Value
	switch rt {
	case ir.TypeI32:
		v = b.I32.VaArg(ap)
	case ir.TypeI64:
		v = b.I64.VaArg(ap)
	case ir.TypeF64:
		v = b.F64.VaArg(ap)
	case ir.TypeF80:
		v = b.F80().VaArg(ap)
	case ir.TypeF128:
		v = b.F128().VaArg(ap)
	case ir.TypePtr:
		v = b.Ptr.VaArg(ap)
	default:
		u.errorf(at, "va_arg of type %s is not supported", t)
		return ir.Ptr{}
	}
	tmp := u.alloca(t, "va", at)
	u.store(value{v, fetch}, tmp, t, at)
	return tmp
}

// warnOnce reports msg the first time key comes up in this unit.
//
// at is where the warning points. Pointing at the file instead — which this
// did — puts every such warning at 1:1, and after preprocessing line 1 is
// whatever system header the translation unit happens to start with, so the
// caret lands on an unrelated typedef in an SDK the user did not write.
func (u *unit) warnOnce(at ast.Node, key, msg string) {
	if u.warned == nil {
		u.warned = map[string]bool{}
	}
	if u.warned[key] {
		return
	}
	u.warned[key] = true
	if at == nil {
		at = u.file
	}
	u.warnf(at, "%s", msg)
}

// errorOnce is warnOnce's error, and shares its key space. An
// unimplemented type is reached from every operand position that
// mentions it, so without this one declaration reports once per use.
func (u *unit) errorOnce(at ast.Node, key, msg string) {
	if u.warned == nil {
		u.warned = map[string]bool{}
	}
	if u.warned[key] {
		return
	}
	u.warned[key] = true
	if at == nil {
		at = u.file
	}
	u.errorf(at, "%s", msg)
}

// vaArgExpr lowers __builtin_va_arg(ap, type): the next variadic argument,
// read at the named type.
//
// It is the same machinery vcc's own <stdarg.h> reaches through
// __builtin_va_arg_ref — the reference to the argument's storage, then a load
// of it — with the type coming from a type name rather than from a witness
// pointer, because that is the shape clang's <stdarg.h> uses.
func (u *unit) vaArgExpr(e *ast.VaArgExpr) value {
	if e.Type == nil {
		u.errorf(e, "__builtin_va_arg needs a va_list and a type")
		return u.poison(types.Typ(types.Int))
	}
	t := u.typeOf(e.Type)
	ref := u.vaArgAt(e.Ap, t, e)
	if ref.IsZero() {
		return u.poison(t)
	}
	if _, isRec := asRecord(types.Unqualify(t)); isRec {
		return value{ref, t}
	}
	return u.load(lval{addr: ref, t: t}, e)
}

// c11AtomicInit is clang's __c11_atomic_init(obj, value).
//
// §7.17.2.2: initialization is not an atomic operation, and the object is
// not yet shared when it happens — so this is an ordinary store, which is
// also what makes it usable on a const-qualified initializer.
func (u *unit) c11AtomicInit(e *ast.CallExpr) value {
	voidT := types.Typ(types.Void)
	if len(e.Args) < 2 {
		u.errorf(e, "__c11_atomic_init takes an object and a value")
		return u.poison(voidT)
	}
	l, ok := u.atomicTarget(e, e.Args[0])
	if !ok {
		return u.poison(voidT)
	}
	t := types.Unqualify(l.t)
	u.store(u.expr(e.Args[1]), l.addr, t, e)
	return value{nil, voidT}
}

// c11AtomicLoad is __c11_atomic_load(obj, order).
func (u *unit) c11AtomicLoad(e *ast.CallExpr) value {
	if len(e.Args) < 1 {
		u.errorf(e, "__c11_atomic_load takes an object and a memory order")
		return u.poison(types.Typ(types.Int))
	}
	l, ok := u.atomicTarget(e, e.Args[0])
	if !ok {
		return u.poison(types.Typ(types.Int))
	}
	for _, a := range e.Args[1:] {
		u.discard(a)
	}
	return u.atomicLoad(l, e)
}

// c11AtomicStore is __c11_atomic_store(obj, value, order).
func (u *unit) c11AtomicStore(e *ast.CallExpr) value {
	voidT := types.Typ(types.Void)
	if len(e.Args) < 2 {
		u.errorf(e, "__c11_atomic_store takes an object, a value and a memory order")
		return u.poison(voidT)
	}
	l, ok := u.atomicTarget(e, e.Args[0])
	if !ok {
		return u.poison(voidT)
	}
	v := u.expr(e.Args[1])
	for _, a := range e.Args[2:] {
		u.discard(a)
	}
	u.atomicStore(v, l, e)
	return value{nil, voidT}
}
