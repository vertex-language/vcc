package lower

import (
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// The Interlocked family: the Windows platform's atomics.
//
// <windows.h> writes InterlockedIncrement and each of its relatives as a
// macro onto an underscore-prefixed name, declares a prototype for that
// name, and hands it to the compiler with `#pragma intrinsic`. Nothing
// defines them — ucrt and kernel32 both stop at the prototype — so a
// compiler that lowers one as an ordinary call compiles a program that does
// not link. A threaded Windows program is written in these.
//
// They are here rather than in gnu.go's table because they are not a
// dialect: they are how the platform spells the operations vcc already
// performs for _Atomic, and the header that needs them is the platform's
// own. The mapping is exact, and each entry below is a name, an operation,
// and which of the two values the operation produced MSVC hands back.
//
// The prototype in the header does not shadow them the way a declaration
// shadows an ordinary builtin. It exists to give the intrinsic a type, not
// to promise a definition, and vcc reads no #pragma intrinsic — so the name
// is the whole signal, as it is for __va_start.

// interlocked is one intrinsic: what it does to the object, and what it
// answers with.
type interlocked struct {
	// op is the read-modify-write, or ILLEGAL where cas is set.
	op token.Kind
	// implicit is the operand Increment and Decrement do not take, and zero
	// where the call carries its own.
	implicit int64
	// returnsNew asks for the value the object holds afterwards. Increment
	// and Decrement are the two that do; every other one in the family
	// answers with what the object held before.
	returnsNew bool
	// cas is the compare-and-swap, which answers with the value it read
	// whether or not it swapped.
	cas bool
}

// interlockedOps is every spelling winnt.h's `#pragma intrinsic` list
// carries, by width: the bare name is a LONG, 8/16/64 name their own, and
// Pointer is a PVOID. The width is not in the table because it does not
// need to be — the object's own type carries it, and each of these takes a
// pointer to the object.
var interlockedOps = func() map[string]interlocked {
	m := map[string]interlocked{}
	for _, e := range []struct {
		base     string
		suffixes []string
		op       interlocked
	}{
		{"Increment", []string{"", "16", "64"}, interlocked{op: token.ADD, implicit: 1, returnsNew: true}},
		{"Decrement", []string{"", "16", "64"}, interlocked{op: token.SUB, implicit: 1, returnsNew: true}},
		{"Exchange", []string{"", "8", "16", "64", "Pointer"}, interlocked{op: token.ASSIGN}},
		{"ExchangeAdd", []string{"", "8", "16", "64"}, interlocked{op: token.ADD}},
		{"And", []string{"", "8", "16", "64"}, interlocked{op: token.AND}},
		{"Or", []string{"", "8", "16", "64"}, interlocked{op: token.OR}},
		{"Xor", []string{"", "8", "16", "64"}, interlocked{op: token.XOR}},
		{"CompareExchange", []string{"", "8", "16", "64", "Pointer"}, interlocked{cas: true}},
	} {
		for _, s := range e.suffixes {
			m["_Interlocked"+e.base+s] = e.op
		}
	}
	return m
}()

// interlockedCall lowers one of them. Every operation is sequentially
// consistent, which is what the Interlocked family documents and what the
// x86 lock prefix gives whether or not it is asked for; the Acquire,
// Release and NoFence spellings the header defines are macros onto these
// same names, so vcc reads them as the same operation.
func (u *unit) interlockedCall(name string, in interlocked, e *ast.CallExpr) value {
	want := 2
	switch {
	case in.cas:
		want = 3
	case in.implicit != 0:
		want = 1
	}
	if len(e.Args) < want {
		u.errorf(e, "%s takes %d argument(s)", name, want)
		return u.poison(types.Typ(types.Int))
	}

	l, ok := u.atomicTarget(e, e.Args[0])
	if !ok {
		return u.poison(types.Typ(types.Int))
	}
	t := types.Unqualify(l.t)

	if in.cas {
		// MSVC's order is (Destination, ExChange, Comperand): the value to
		// write comes before the value to match, which is the reverse of
		// every other compare-and-swap in the compiler.
		p, ok := u.atomicPlanFor(l, e)
		if !ok {
			return u.poison(t)
		}
		next := u.convert(u.expr(e.Args[1]), t, e)
		expect := u.convert(u.expr(e.Args[2]), t, e)
		if next.v == nil || expect.v == nil {
			return u.poison(t)
		}
		return value{u.atomicCas(p, l, expect, next, e).v, t}
	}

	rhs := u.intConst(in.implicit, t)
	if in.implicit == 0 {
		rhs = u.expr(e.Args[1])
	}
	old, updated := u.atomicUpdate(in.op, l, rhs, e)
	if in.returnsNew {
		return value{updated.v, t}
	}
	return value{old.v, t}
}
