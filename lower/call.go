package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/types"
)

// call lowers a function call, direct or through a pointer.
//
// The two forms differ in one thing only: where the convention comes from. A
// direct call reads it off the declaration; an indirect one reads it off a
// func typedef, which is why callind names a type at all.
func (u *unit) call(e *ast.CallExpr) value {
	if v, ok := u.builtin(e); ok {
		return v
	}

	fnExpr := stripParens(e.Fun)
	var (
		callee ir.Callee
		fp     ir.Ptr
		ft     *types.Func
	)

	if id, ok := fnExpr.(*ast.Ident); ok {
		if c := u.callable(u.lookup(u.name(id))); c != nil {
			callee = c
			ft, _ = asFunc(u.lookup(u.name(id)).typ)
		}
	}
	if callee == nil {
		v := u.expr(e.Fun)
		t := types.Unqualify(v.t)
		if p, ok := asPointer(t); ok {
			t = types.Unqualify(p.Elem)
		}
		f, ok := t.(*types.Func)
		if !ok {
			u.errorf(e, "called object is not a function")
			return u.poison(types.Typ(types.Int))
		}
		ft, fp = f, u.ptr(v.v, e)
	}
	if ft == nil {
		u.errorf(e, "internal: call with no function type")
		return u.poison(types.Typ(types.Int))
	}

	args, sret, retTy := u.callArgs(e, ft)

	b := u.blk()
	if id, ok := fnExpr.(*ast.Ident); ok && u.msSetjmp(u.name(id)) && len(args) == 1 {
		args = append(args, b.Ptr.StackSave())
	}
	var res ir.Results
	if callee != nil {
		res = b.Call(callee, args...)
	} else {
		res = b.CallInd(fp, u.types.funcType(ft), args...)
	}

	switch {
	case !sret.IsZero():
		return value{sret, retTy}
	case isVoid(types.Unqualify(retTy)):
		return value{nil, retTy}
	case res.Len() == 0:
		u.errorf(e, "internal: call to a %s returned no result", retTy)
		return u.poison(retTy)
	default:
		return value{res.Value(0), retTy}
	}
}

// msSetjmp reports whether name is one of the Microsoft CRT's setjmp entry
// points, which take a second argument the source never writes.
//
// <setjmp.h> declares int _setjmp(jmp_buf) and every program calls it that
// way, but on x64 the CRT's _setjmp stores a second register as the frame it
// will unwind to, and longjmp hands that value to RtlUnwindEx as the frame to
// stop at. Nothing in the source supplies it; the compiler does, and MSVC and
// clang both do; MSVC emits a bare "mov rdx, rsp" before the call.
//
// A call that leaves the register as it found it gives longjmp a frame that
// was never on the stack, and RtlUnwindEx walks past the top of it and raises
// STATUS_BAD_FUNCTION_TABLE.
//
// The value is the stack pointer and not the frame pointer, because what
// RtlUnwindEx compares it against is the establisher frame: the base of a
// function's fixed allocation, which is the RSP its body runs with. Handing
// over RBP names a frame the unwinder never sees, and it answers
// STATUS_BAD_STACK instead.
//
// The extra argument is added here and the extra parameter in imported, so
// the call and the signature it is made against say the same thing. The C
// declaration is untouched, which is what keeps setjmp(env) a one-argument
// call to everything above lowering.
func (u *unit) msSetjmp(name string) bool {
	if u.layout.ABI != "ms" {
		return false
	}
	return name == "_setjmp" || name == "_setjmpex"
}

// callArgs evaluates the arguments and returns the VIR argument list.
//
// Three rules apply and are applied in this order. A prototyped parameter
// converts the argument to its declared type (§6.5.2.2p7). An argument past
// the prototype's fixed parameters, or every argument of an unprototyped
// call, gets the default argument promotions (§6.5.2.2p6): float becomes
// double, narrow integers become int. An aggregate argument is copied into a
// frame temporary and passed by address, because byval means the callee owns
// its copy and C says the callee may modify it.
func (u *unit) callArgs(e *ast.CallExpr, ft *types.Func) (args []ir.Value, sret ir.Ptr, retTy types.Type) {
	retTy = ft.Ret
	if r, ok := asRecord(types.Unqualify(retTy)); ok && !r.Vector {
		sret = u.alloca(retTy, "ret", e)
		_ = r
		args = append(args, sret)
	}

	// An unprototyped declaration says nothing about its parameters, so
	// §6.5.2.2p6's default promotions are all a call to one can apply. An
	// identifier-list *definition* does say — the analyzer resolves its
	// declaration list onto the type — and where this unit can see it, the
	// argument is converted to the parameter it will be bound to. That is
	// what the callee's signature was built from, so anything else is a call
	// the module does not typecheck, and §6.5.2.2p6 leaves the mismatch
	// undefined rather than meaningful.
	fixed := len(ft.Params)
	for i, a := range e.Args {
		v := u.expr(a)
		var want types.Type
		if i < fixed {
			want = types.Unqualify(types.AdjustParam(ft.Params[i].Type))
			if isVoid(want) {
				continue
			}
			if !ft.Proto {
				// §6.5.2.2p6 promotes first; a parameter narrower than int
				// was declared that way but is passed as the promoted type.
				want = u.defaultPromote(want)
			}
		} else {
			want = u.defaultPromote(v.t)
		}
		if _, ok := u.types.indirect(want); ok {
			// A copy the callee owns, which is what byval means and what
			// C requires: a parameter is a modifiable object. Where the
			// argument is already in memory that is a memcpy from its
			// address; where it is a register value — which __m128i is —
			// it is a store into the copy.
			tmp := u.alloca(want, "arg", a)
			if r, isRec := types.Unqualify(want).(*types.Record); isRec && !r.Vector {
				size, _ := u.model.Sizeof(want)
				b := u.blk()
				b.MemCpy(tmp, u.ptr(v.v, a), b.I64.Const(size))
			} else {
				u.store(u.convert(v, want, a), tmp, want, a)
			}
			args = append(args, tmp)
			continue
		}
		args = append(args, u.convert(v, want, a).v)
	}
	if ft.Proto && len(e.Args) < fixed {
		u.errorf(e, "internal: %d arguments for %d parameters", len(e.Args), fixed)
	}
	return args, sret, retTy
}

// defaultPromote is §6.5.2.2p6.
func (u *unit) defaultPromote(t types.Type) types.Type {
	t = types.Unqualify(t)
	if b, ok := t.(*types.Basic); ok && b.K == types.Float {
		return types.Typ(types.Double)
	}
	if types.IsInteger(t) {
		return u.promote(t)
	}
	return t
}
