# types

`package types` represents C types and constructs them from declaration
specifiers and declarators.

```
import "github.com/vertex-language/vcc/types"
```

Types are trees; struct, union, and enum types additionally have
**identity** — two `*Record` values are the same type iff they are the same
pointer, which is what makes a tag namespace meaningful. Constraint checking
the parser deliberately deferred — specifier multisets, qualifier placement,
`static`/`*` in array declarators, function/array derivation rules — happens
here, during construction, reported through the `Resolver`.

## The `Type` tree

```go
type Type interface {
	Kind() Kind
	String() string
}
```

| Type | Shape |
| --- | --- |
| `*Basic` | A builtin arithmetic type or `void`. Use `Typ(k)` for the canonical singleton of each kind — `*Basic` values compare by identity per kind, never allocated per use. |
| `*Qualified` | `const`/`volatile`/`restrict`/`_Atomic` wrapped around another type. **Never nests**: `Qualify` merges into an existing wrapper rather than stacking one. |
| `*Pointer` | Pointer-to-`Elem`. |
| `*Array` | Array-of-`Elem`, one of four `ArrayForm`s (below). |
| `*Func` | A function type. `Proto` is false for `f()` and K&R identifier-list declarators, where parameters are unspecified — as opposed to `f(void)`, which is a prototype with zero parameters. |
| `*Record` | A struct or union (`Union` distinguishes them). **Has identity**: two `*Record`s are the same type iff the same pointer, the same as `*Enum`. `Complete` flips true when a definition supplies the member list. |
| `*Enum` | An enumerated type; identity like `Record`. The compatible implementation type is `int` in this implementation. |

`Char`, `SChar`, and `UChar` are three distinct kinds — plain `char` is its
own thing per §6.2.5p15, not an alias for signed or unsigned char.

### Qualifiers

```go
func Qualify(t Type, q Qual) Type   // merges with any qualifiers already present
func Unqualify(t Type) Type         // strips the wrapper, if any
func QualsOf(t Type) Qual           // the qualifier set, 0 if unqualified
```

Qualifying with `0` is the identity — `Qualify(t, 0) == t`, no wrapper
allocated. Everything that inspects a type's *shape* (a `switch` on
`Kind()`, a type assertion to `*Array`/`*Func`/…) should go through
`Unqualify` first; qualifiers ride alongside the shape, not inside it.

### Predicates

`IsInteger` reports whether a (unqualified) type is an integer type —
enums count, per §6.2.5p17. `IsSigned` reports whether an integer type is
signed; plain `char`'s signedness isn't the type's business (it's the
`Model`'s — see below), so `IsSigned` reports `false` for `Char` and callers
that care ask the `Model` instead. `Complete` (package-level) is the
element/declared-object completeness test — not `void`, not an incomplete
record, enum, or array — exported for the analyzer's declared-object checks.

### Printing

Every `Type` has a `String()` for diagnostics: C-ish and compact —
`unsigned long long`, `int*`, `int[3]`, `struct S`, `struct <anonymous>` for
an unnamed tag, `int(int, ...)` for a variadic prototype, `int()` for an
unspecified-parameter function. Not a round-trippable declarator spelling —
just enough for an error message to name a type unambiguously.

## Construction

Construction takes two inputs — a `DeclSpecs` list and a `Declarator` tree —
because that's exactly the shape C's own declaration grammar has: a
specifier list denoting a base type, and a declarator tree the
type-construction phase reads inside-out to derive from that base.

### `Resolver`

```go
type Resolver interface {
	Typedef(id *ast.Ident) Type
	Tag(spec ast.Expr) Type
	Eval(e ast.Expr) (int64, bool)
	Report(n ast.Node, msg string)
}
```

This is everything construction needs from its caller: name and tag
resolution, constant evaluation for array lengths, and a place to send
diagnostics. The analyzer implements it against its own scopes and constant
folder; a simpler tool (a one-off type-checker with no scoping) can
implement a stub — see `types_test.go`'s `stub` for the minimal shape.
`Eval`'s `ok` is `false`, not an error, when an expression isn't constant —
a non-constant array length is a VLA, which is `BuildDeclarator`'s job to
recognize, not `Resolver`'s to reject.

### `BuildSpecs`

```go
func BuildSpecs(unit *token.File, specs ast.DeclSpecs, r Resolver) Spec
```

```go
type Spec struct {
	Type        Type
	Storage     token.Kind // TYPEDEF/EXTERN/STATIC/AUTO/REGISTER, or 0
	ThreadLocal bool
	Inline      bool
	Noreturn    bool
	Aligns      []*ast.AlignasSpec // kept as nodes; checked by the analyzer
}
```

Folds a written-order specifier list into a `Spec`, in one pass:

- **At most one storage class** (§6.7.1) — a second one is reported once.
- **`_Thread_local` combines only with `static` or `extern`** (§6.7.1p3).
- **The basic-keyword multiset table** (§6.7.2p2) — keywords are
  canonicalized to a fixed order (independent of how they were written:
  `long int signed` and `signed long` both resolve) and looked up; an
  invalid combination (`long char`, `short long`) is reported once and
  falls back to `int`.
- **At most one data type** — a `struct`/`union`/`enum`/`_Atomic()`/typedef
  specifier combined with a basic keyword, or with a second one of itself,
  is "two or more data types."
- **C11 requires a type specifier** (§6.7.2p2) — implicit `int` is gone;
  an empty specifier list is reported and falls back to `int` so
  construction always produces *a* type.
- **`restrict` requires a pointer type**; a non-pointer `restrict` is
  reported and the qualifier dropped. **`_Atomic` must not qualify an array
  or function type** (reachable through a typedef), likewise reported and
  dropped.

Every reported path still returns a usable `Spec` — construction never
fails outright, only degrades to `int` or drops an offending qualifier.

### `BuildDeclarator`

```go
func BuildDeclarator(unit *token.File, base Type, d ast.Declarator, param bool, r Resolver) (Type, *ast.Ident)
```

Derives a type from `base` through a declarator tree, reporting derivation
constraints as it goes, and returns the derived type plus the declared
identifier (`nil` for an abstract declarator, and for the fully absent
declarator — a `nil` `Declarator` — which just returns `base` unchanged).
`param` enables the parameter-only forms and checks (`[static …]`, `[*]`,
and the `void`-parameter-list rule).

Reading inside-out mirrors how the grammar itself nests: a `*PtrDeclarator`
wraps `base` in a `*Pointer` and recurses on `Inner`; an `*ArrayDeclarator`
wraps it in an `*Array` and recurses; a `*FuncDeclarator` wraps it in a
`*Func`, builds its parameter list, and recurses. `*ParenDeclarator` is
transparent — it groups without adding a type layer. A `*NameDeclarator` is
the base case: the leaf that finally supplies the identifier.

Derivation-constraint reports along the way:

- **Function returning a function**, and **function returning an array** —
  both illegal derivations (§6.7.6.3p1); each is reported and the return
  type replaced with `int` so the tree stays well-formed.
- **Array of functions** — reported; the element becomes a pointer to the
  function instead (the natural correction, though the array is still
  flagged).
- **Array element must be complete** — an incomplete element type (an
  unfinished `struct`, or `void`) is reported.
- **Array length**: `Eval`uated through the `Resolver`. A non-positive
  fixed length is reported and clamped to 1. A length that doesn't fold is
  a VLA (`ArrayForm.VLA`) — not an error here; legality by scope (block
  scope only, no `static`) is the analyzer's call.
- **`[static …]` and `[*]` belong only to a parameter's outermost array
  derivation** (§6.7.6.2p3, §6.7.6.3p7) — checked once at the end of
  `BuildDeclarator` across every array built during the walk (tracked via
  `builder.arrays`), since "outermost" and "is this a parameter" are only
  knowable once the whole tree is built. Anywhere else, both are reported
  and stripped.
- **`(void)` as the sole, unnamed parameter** means an empty parameter
  list; `void` combined with anything else, or named, is reported and
  replaced with `int`.
- **Parameter storage class** must be `register` or nothing; anything else
  is reported.

### `AdjustParam`

```go
func AdjustParam(t Type) Type
```

§6.7.6.3p7–8's parameter adjustment: an array parameter decays to a
pointer to its element type (keeping any qualifiers written inside the
brackets — the reason those qualifiers are threaded onto the array type
during `BuildDeclarator` rather than discarded); a function parameter
decays to a pointer to it. Exported since the analyzer reapplies it when
matching K&R parameter declarations against their identifier list, outside
the normal `BuildDeclarator` path.

## `Model`

```go
type Model struct {
	CharSigned bool
	WCharKind  Kind

	SizeShort, SizeInt, SizeLong, SizeLongLong int64
	SizePtr                                    int64
	SizeFloat, SizeDouble, SizeLongDouble      int64
	AlignLongDouble                            int64
}

func LP64() Model // x86-64 Linux and friends
```

A target's type model: sizes in bytes, `char`'s signedness, `wchar_t`'s
identity. Layout — `Sizeof`, `Alignof`, field offsets — is a **pure
function of the `Model`**; nothing in this package probes a host, so the
same tree can be sized against any target without re-parsing.

```go
func (m Model) Sizeof(t Type) (int64, bool)
func (m Model) Alignof(t Type) (int64, bool)
func (m Model) IntBits(t Type) (bits int64, signed bool)
func (m Model) IntMax(t Type) uint64
```

`ok` is `false` for sizes `sizeof` genuinely cannot know: incomplete types,
function types, VLAs, an incomplete record. `IntBits` resolves `char`'s
signedness through `CharSigned` and treats an enum's width as `int`'s.

### Record layout

`layout` computes a struct or union's size and alignment: fields advance a
running bit offset, rounded up to each field's alignment; a union takes the
maximum member size instead of accumulating. Bit-fields pack into
consecutive allocation units of their **declared type**, System V–style —

- a bit-field that would cross its unit's boundary starts a new unit instead;
- a zero-width bit-field pads to the start of the next unit;
- ordinary fields round up to their own alignment as usual.

The final size rounds up to the record's overall alignment, so `sizeof`
is always a multiple of `alignof` — the usual C guarantee for arrays of the
type.

## Dependencies

Imports [`token`](../token), [`ast`](../ast); is imported by
[`analyzer`](../analyzer), which supplies the `Resolver` and calls
`BuildSpecs`/`BuildDeclarator` while checking a translation unit.