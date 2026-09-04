# lower

Typed C AST → VIR. This package is phase 7's second half: the analyzer
decided what is legal, `types` decided what each declarator denotes, and
`lower` decides what runs.

```go
import "github.com/vertex-language/vcc/lower"
```

One entry point, one direction, no machine:

```go
func Lower(unit *token.File, file *ast.File, info *analyzer.Info, opt Options) (*ir.Module, []token.Diagnostic)
```

In: a tree that parsed and an `*analyzer.Info` for it. Out: an
`*ir.Module` and the diagnostics only code generation can raise. The
module is never nil — a partial module is what `--emit vir` on broken
input should print — and it is always safe to hand to `verify.Module`,
which is the authority on whether it is sound.

This package imports `vertex-language/ir` and nothing past it. It
produces `vir`, not bytes. Instruction selection, register allocation,
and the meaning of a calling convention are `ir/lower`'s, one repository
down.

---

## End to end

```go
package main

import (
	"fmt"
	"os"

	"github.com/vertex-language/ir"
	"github.com/vertex-language/ir/text"
	"github.com/vertex-language/ir/verify"

	"github.com/vertex-language/vcc/analyzer"
	"github.com/vertex-language/vcc/lower"
	"github.com/vertex-language/vcc/parser"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

const src = `
struct point { int x, y; };

static int scale = 3;

int dot(struct point a, struct point b) {
    return a.x * b.x + a.y * b.y;
}

int sum(const int *p, long n) {
    int acc = 0;
    for (long i = 0; i < n; i++)
        acc += p[i] * scale;
    return acc;
}
`

func main() {
	unit := token.NewFile("demo.c", []byte(src))

	file, diags := parser.ParseFile(unit, parser.DefaultMode)
	defer file.Release()

	model := types.LP64()
	info, adiags := analyzer.Check(unit, file, model)
	diags = append(diags, adiags...)

	m, ldiags := lower.Lower(unit, file, info, lower.Options{
		Name:   "demo",
		Target: ir.X86_64Linux,
		Model:  model,
	})
	diags = append(diags, ldiags...)

	token.SortDiagnostics(diags)
	fatal := false
	for _, d := range diags {
		fmt.Fprintln(os.Stderr, d.Print(unit))
		fatal = fatal || d.Severity == token.Error
	}
	if fatal {
		os.Exit(1)
	}

	if err := verify.Module(m); err != nil {
		fmt.Fprintln(os.Stderr, "verify:", err)
		os.Exit(1)
	}
	text.Print(os.Stdout, m)
}
```

`Model` must be the same model `analyzer.Check` ran against. `sizeof`
disagreeing between the two is a bug, not a configuration — the two
compute it independently today, which is the first item under
[Upstream](#upstream-asks) below.

The `struct point` parameters come out as `byval` pointers, `scale` as an
internal global, and the `for` loop as three blocks with the induction
variable in the frame:

```vertex-ir
module demo

use "x86_64/linux"

layout {
  abi        sysv,
  endian     little,
  ptrbits    64,
  stackalign 16,
  extfloat   f80, f128,
}

type @point struct { x i32 @0, y i32 @4 }

global internal rw @scale i32 = 3

export func @dot(%a ptr byval @point, %b ptr byval @point) i32 nounwind {
@entry:
  %0 = i32.load %a
  %1 = i32.load %b
  %2 = i32.mul %0, %1
  %3 = i64.const 4
  %4 = ptr.add %a, %3
  %5 = i32.load %4
  %6 = ptr.add %b, %3
  %7 = i32.load %6
  %8 = i32.mul %5, %7
  %9 = i32.add %2, %8
  return %9
}
```

---

## What this package owns

Four things, because nothing below it does them.

**Expression typing.** `analyzer.Info` records the types of *declaring*
nodes and the values of expressions the analyzer *required* to be
constant. The type of `a + b` is computed here, along with the
conversions §6.3 demands. `convert.go` is that chapter in one file: the
integer promotions, the usual arithmetic conversions, the null-pointer
constant test, and `_Bool`'s compare-against-zero.

**Ordinary-identifier resolution.** `Info` has no use-to-declaration map,
so `lower` rebuilds object scopes in source order as it walks. C is
single-pass, so this is a walk and not a re-analysis. Tags need no scope
here — a tag in type position was already resolved into the `types.Type`
that `Info` holds.

**Record layout.** Byte offsets, bit-field placement, padding. Computed
from the same `types.Model` that answers `sizeof`, in `layout.go`.

**Initializer shape.** Brace elision, designators, and the flattening of
a static initializer into an `ir.Init` tree.

What it does not own: anything about a machine. No register is physical,
no address is known, and the only target facts consulted are the ones in
`types.Model` and in the module's `ir.Layout`.

---

## Three decisions worth knowing

### Everything lives in the frame

Every automatic object gets a `ptr.alloc`; every access is a load or a
store. No block this package emits takes a parameter.

That is what makes `goto` into a scope, Duff's device, and address-taken
locals all fall out for free — there is no place for the frontend to get
SSA construction wrong, because it constructs none. Promotion to
registers is `ir/lower/pass`'s job, where it is a known pass with a known
bug surface and its own tests. A frontend that built SSA here would owe
`goto` a merge, and would owe it in the one place nobody tests.

`ptr.alloc` is entry-block-only (§19.6), so storage is reserved there
regardless of which block declares the object. C's block scoping is a
naming rule, not a storage one. The exception is the VLA, which really is
scoped and uses `ptr.alloca` — see the table at the bottom.

### Aggregates cross boundaries by reference, uniformly

A record argument is a pointer carrying `byval`; a record return is a
leading pointer carrying `sret`. Always, on every target.

vcc states that the value is *passed*, not *how*. Whether that becomes
registers or stack is `ir/lower/abi`'s classification. The alternative —
vcc doing SysV classification itself — would mean this repo knows what
SysV is, which the README says it must not.

### The trapping conversions, not the saturating ones

§6.3.1.4 makes an out-of-range float-to-int conversion undefined, and VIR
has no undefined. The `_sat_` forms exist for a frontend that has proven
the range or chosen to define the overflow; vcc has done neither, so the
conversion traps and the program says so instead of continuing with a
number nobody chose.

Same reasoning for `__builtin_unreachable`, which lowers to `trap`: one
instruction, and a defined outcome when the belief is wrong.

---

## `va_arg` has no parser carve-out

`va_arg(ap, T)` takes a *type*, which C's grammar does not admit in an
argument list. Rather than adding a builtin the parser must know about,
`<stdarg.h>` spells it with a null pointer whose type carries `T`:

```c
#define va_arg(ap, T) (*(T *)__builtin_va_arg_ref(ap, (T *)0))
```

`lower` reads the pointee off the second argument's type and never
evaluates it. `offsetof` rides the same trick —
`((size_t)&((T *)0)->m)` — which `const.go` folds to a null-based address
constant, which is to say an integer.

An aggregate uses `ptr.va_arg_ref` directly. A scalar uses the typed
`va_arg` verb and gets a frame temporary, because §I has no verb handing
back an address for a scalar and the macro's shape needs one. The default
argument promotions decide which verb applies, which is why there is no
`f32.va_arg` case to write.

---

## Files

```
lower/
├── doc.go        the contract, and the two-pass rationale
├── lower.go      Lower, Options, unit, diagnostics
├── scope.go      object scopes, storage classes, linkage
├── layout.go     record layout: offsets, bit-fields, padding
├── type.go       C type → RegType / StoreType / FType / Sig
├── decl.go       file-scope declarations, tentative definitions, bodies
├── convert.go    §6.3, entire
├── expr.go       typing and rvalue emission; the verb dispatch
├── lvalue.go     designations, assignment, staticType
├── call.go       arguments, promotions, byval/sret
├── stmt.go       control flow, switch dispatch, goto, return
├── init.go       brace elision, designators, ir.Init trees
├── const.go      §6.6 folding, address constants, reloc
├── string.go     literal pooling
├── vla.go        variably modified types
└── builtin.go    va_*, trap, the <stdarg.h> surface
```

Two passes, because C admits forward reference at file scope and Go does
not admit patching an `ir.Symbol` into existence twice. Pass one declares
every external object and function; pass two emits bodies and
initializers. That is also why static initializers are attached in pass
two: `int *p = &x; int x;` needs `x`'s symbol to exist before
`ir.RelocInit` can take it.

---

## Where each class of error surfaces

| class | where |
|---|---|
| constraint violations, redeclarations, bad specifier multisets | `analyzer`, before this package runs |
| non-constant static initializer, `_Generic` with no match, `&` on a bit-field | here, as a `token.Diagnostic` |
| wrong operand type, missing verb, store operand order | `go build` — the IR's Go types |
| branch arity, `alloc` outside entry, `sret` not first, `ext-float` absent from layout | `m.Err()`, sticky |
| dominance, terminators, initializer structure vs. declared type | `verify.Module` |

Faults in this package are reported, never panicked: a translation unit
that reached `lower` parsed and checked, so a fault here is a fault in
`lower`. Those diagnostics are prefixed `internal:` and are bugs to file,
not user errors.

A sticky builder failure surfaces as exactly one diagnostic. Every `ir`
call after the first failure is a no-op, so reporting each would be a
cascade — and "every mistake once, never a cascade" is the same promise
the front end makes.

---

## Deliberately not here

| absent | why |
|---|---|
| optimization | Every pass is a vir-to-vir diff, above `ir/lower`. `lower` emits the obvious thing. |
| `unreachable` | §L. A path a frontend believes cannot be taken ends in `trap`. |
| SSA construction | See above. `pass/` promotes; this package allocas. |
| ABI classification | `byval`/`sret` say what, `ir/lower/abi` decides how. |
| a dynamic FP environment | §L. Rounding is round-to-nearest-even and exception flags are unobservable, so `#pragma STDC FENV_ACCESS ON` has nothing to lower to. |
| a general constant evaluator | `reloc` admits what relocation records admit. `const.go` folds to a literal or symbol-plus-displacement and reports anything else. |
| GNU builtins | `builtin.go` grows from vcc's own headers only. A builtin that exists to make third-party code compile is a dialect. |

---

## Known gaps

Each is a diagnostic today, not silence.

| gap | state |
|---|---|
| `_Complex` | Diagnosed in `convert`. Needs a re/im register pair — the one C type with no VIR analogue at all. |
| `_Atomic` access | Diagnosed in `load`/`store`/`assign`. §H has every verb; the missing piece is `<stdatomic.h>`'s ordering plumbing. |
| constant bit-field initializers | Diagnosed in `staticCursor`. Needs the byte-pattern fold — several C fields collapse into one filler's `ir.Str`. |
| VLA block-scope release | Warns once; storage lives to function exit. Correct, wasteful in a loop. Needs a per-block pre-scan so `stacksave` runs before the first `alloca`. |
| sparse switch | Linear comparison chain. A balanced binary search is a local change to `dispatch`; dense sets already use `br_table`. |

---

## Upstream asks

Three changes to packages below this one, each of which deletes code
here. They are listed in the order I'd take them.

**1. `types` should expose record layout.** `Model.Sizeof(*Record)`
works, so `types` computes offsets internally, but `types.Field` has no
offset and there is no `Model.OffsetOf`. `layout.go` re-implements it,
which means `sizeof` in C and `offsetof` in the IR can silently drift
apart. `layout()` asserts agreement on `Size` and warns on mismatch —
that assertion is a smoke alarm, not a fix. Something like
`Model.Layout(*Record) []Placement` (offset, bit offset, bit width, unit
size) deletes the file.

**2. `analyzer.Info` should record expression types.** `Types` covers
declaring nodes only, so `lvalue.go` carries `staticType`, a hand-written
mirror of `expr`'s typing rules for the three places C needs a type
before a value: `sizeof`'s expression form, `_Generic`'s controlling
expression, and `?:`'s result type. It is the function most likely to
drift, and the drift is silent. A `Types map[ast.Expr]types.Type` deletes
it, deletes the `_Generic` compatibility duplication, and turns
`condType` into a lookup.

**3. `types.Array` should keep its length expression.** The VLA form
records that a dimension is variable but not what computes it, so
`vla.go` carries a `map[*types.Array]ast.Expr` populated by a second
declarator walk. One `LenExpr ast.Expr` field makes that a field access.

Together those three are roughly four hundred lines here, and every one
of them is a line where two packages must agree without a compiler
checking that they do.

**One open question.** `StoreF80`'s size and alignment are not stated
anywhere in the IR surface, but C wants `sizeof(long double) == 16` with
16-byte alignment on x86-64 SysV. `type.go` uses `ir.StoreF80` directly
and forces correct record sizes with explicit tail padding; a standalone
`long double` global is the case that could still come out 10 or 12 bytes
wide. Does `f80` carry the target's padded width, or should vcc name a
padded type?