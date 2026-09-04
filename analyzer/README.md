# analyzer

`package analyzer` is phases 5–6 and the semantic half of phase 7: literal
decoding and string concatenation, scopes and namespaces (ordinary, tags,
labels), integer constant expressions, and the constraint checks the parser
deliberately deferred.

```
import "github.com/vertex-language/vcc/analyzer"
```

## Usage

```go
unit := token.NewFile("a.c", src)
file, diags := parser.ParseFile(unit, parser.DefaultMode)
info, diags := analyzer.Check(unit, file, types.LP64())
```

`Check` analyzes one translation unit against a target `types.Model`. The
returned `*Info` is **never nil**; diagnostics are sorted and each mistake is
reported once.

```go
type Info struct {
	Types  map[ast.Node]types.Type   // InitDeclarator, ParamDecl, FieldDeclarator, FuncDecl, TypeName
	Consts map[ast.Expr]int64        // expressions required to be ICEs, and were
	Enums  map[*ast.Enumerator]int64 // every enumerator's value, implicit or explicit
}
```

`Info` is the seam: what this package learns, it records there, for phases
above to read rather than recompute.

## Expression typing

`expr.go` types every expression and reports §6.5's constraint violations
against those types: an undeclared identifier, an assignment, argument,
initializer or return value the types do not admit, a call with the wrong
number of arguments, an operator whose operands it does not apply to, a
member a record does not have.

The rule it works to is that a nil type means *not known*, and nothing is
reported about an operand whose type is not known. A checker that guesses
produces a diagnostic about correct code, which is worse than silence: the
user cannot act on it, and learns to ignore the compiler.

The type classification and conversions live in `types` — `IsArithmetic`,
`Decay`, `Model.Promote`, `Model.Usual`, `Compatible`, `Assignable` — rather
than here, because `lower` needs the same answers and the two must agree.
Where they disagreed, the analyzer would accept a program lower could not
emit, or reject one it could.

## What's not here yet, by design

Initializer shape and arity: whether a braced list has too many elements for
what it initializes, and whether each element is assignable to the subobject
it lands on. `expr` walks an `InitList` for its literals and its identifiers
but does not check it against a type.

`Info.Types` still records only the declaring nodes, not every expression, so
`lower` recomputes expression types rather than reading them back. `evalInt`
folds both forms of `sizeof` regardless — the expression form types its
operand on demand, and quietly, since that operand is walked again where the
`sizeof` itself is walked.

## Literal decoding (phases 5–6)

`decode.go` is a set of pure functions from a literal's raw spelling (plus a
`types.Model`) to a decoded value — the scanner already enforced the lexical
grammar, so these only assign value and type, or report range violations.

- **`DecodeIntConst`** implements §6.4.4.1p5's candidate-type table: the
  first type in the suffix/base column the value fits is picked (decimal
  literals skip unsigned candidates unless suffixed; hex and octal don't).
  Overflow past every candidate is one diagnostic, and the value still comes
  back (as `unsigned long long`) so callers can keep going.
- **`DecodeFloatConst`** strips an `f`/`F`/`l`/`L` suffix, then parses with
  `strconv.ParseFloat` — which already handles hex-float spellings like
  `0x1.8p3`.
- **`DecodeCharConst`** handles prefixes (`L`, `u`, `U`) and the plain
  multi-character case (`'ab'`), whose value is the implementation-defined
  left-to-right accumulation `(v << 8) | next`. A multi-character constant
  combined with an encoding prefix is reported.
- **`DecodeString`** concatenates a `StringLit`'s segments per §6.4.5p5,
  reports differing encoding prefixes across segments (plain segments
  combine with anything), and decodes to either UTF-8 bytes (`char`,
  `u8`-prefixed) or code units (`u16`/`u32`/wide), always NUL-terminated so
  array length is `len(Data)`.
- **`decodeOne`** is the shared per-character/escape decoder: simple
  escapes, octal (up to 3 digits), `\x` (unbounded hex digits, range-checked
  against the element type), and `\u`/`\U` (checked against §6.4.3: no
  surrogate range, no code point above U+10FFFF, nothing below U+00A0 except
  `$ @ \``). Unknown escapes were already the scanner's diagnostic, so
  `decodeOne` just falls back to the raw character.

`checkGenDecl`'s `decodeAll` walks every expression once, running the
decoders over each `BasicLit`/`StringLit` it finds — so a bad literal is
reported exactly once, whether or not the surrounding expression is
otherwise analyzed, via `reportOnce`'s single-report funnel.

## Integer constant expressions (§6.6)

`evalInt` folds an `ast.Expr` to `(value, ok)`; `ok` is false, silently,
whenever the expression isn't a constant expression at all — an array
length that isn't constant is a VLA, not a mistake, so `evalInt` doesn't
report on its own. Callers that *require* a constant — enumerator values,
`_Static_assert` conditions, bit-field widths, case labels, `_Alignas`
arguments — go through `requireConst`, which reports and also records the
value in `Info.Consts`.

Folds identifiers only through enum constants (looked up in scope);
short-circuit evaluation applies to `&&`/`||` even in constant expressions,
so `0 && (1/0)` folds to `0` without evaluating the division. Division and
remainder by a folded zero are one diagnostic, not a panic. `CastExpr`
folds by truncating/sign-extending to the target integer type's width via
the `Model`; `COMMA` is never permitted in a constant expression and folds
to not-a-constant.

## Scopes and namespaces (§6.2.3)

Three of C's four namespaces live in `scope.go`: **ordinary** identifiers,
**tags** (struct/union/enum), and **members** (which live on the `Record`
itself, checked while building it). **Labels** are per-function state on the
checker (`c.labels`), reset at the start of each `FuncDecl`, since label
scope is the whole function body regardless of block nesting.

`declare` enters a name into the innermost scope and enforces same-scope
redeclaration rules: permitted for declarations with linkage (file scope,
or `extern`) and for a typedef redeclaring a typedef; anything else —
including a symbol changing kind — is reported, with the specific kind
mismatch named in the message.

Struct/union and enum tags follow their own merge rules in `check.go`'s
`structType`/`enumType`: a tag re-mentioned without a brace refers to the
existing (possibly still-incomplete) type; a brace on an already-complete
tag in the *same* scope is a redefinition; a name reused as the other kind
of tag (`struct S` vs `union S`) is reported as "declared as a different
kind of tag." An inner scope's tag never leaks to an outer one.

## Declarations

`checkGenDecl` builds each declarator's type via `types.BuildDeclarator`,
then classifies the resulting symbol:

- **`typedef`** — reports if given an initializer.
- **function type without a body** (a plain declaration, not a definition)
  — reports if given an initializer.
- **object** — checked for completeness, with two deliberate passes:
  `extern` declarations (completion deferred to wherever they're defined)
  and an incomplete array *with* an initializer (`int a[] = {1,2,3};` —
  arity checking is one of the documented not-yet-here items). Function
  specifiers (`inline`, `_Noreturn`) on a non-function are reported.

Storage-class and placement checks:

- File-scope `auto`/`register` is reported.
- A variably-modified type (a VLA, or a pointer to one) needs block scope
  and automatic storage — `static` or file-scope VLAs are reported.
- `_Alignas`'s constant argument must be a power of two (zero is allowed:
  it means "no effect").

## Bit-fields (§6.7.2.1p3–4)

`bitFieldWidth` enforces the declared type is `_Bool`, `int`, or
`unsigned int`; the width must fold to a non-negative constant, at most the
type's bit width, and — for a *named* bit-field — nonzero. An unnamed
zero-width bit-field is the deliberate padding idiom and passes.

## Functions and K&R matching

`checkFuncDecl` builds the function's type, pushes a scope, and brings
parameters in via `declareFnParams`:

- **Prototype form** — parameters declared directly from the function
  type's parameter list.
- **K&R form** — the parser kept the identifier list (`fd.Idents`) and the
  declaration list (`fn.KR`) separate; this is where they're matched. Each
  K&R declaration must name a parameter from the identifier list (anything
  else is "declared but not in the parameter list"); K&R declarations may
  only add `register` as a storage class. A parameter with no matching K&R
  declaration defaults to `int` per §6.9.1p7, silently.

`goto` targets are collected during the body walk and checked once the
whole function has been walked, so forward gotos resolve correctly; an
unresolved target is "goto to undefined label."

## Statements

`checkStmt` threads two counters — `switchD` and `loopD` — through nested
statements to place discipline on `break` (loop or switch), `continue`
(loop only), and `case`/`default` (switch only) without needing to search
back up a parent chain. `for`'s own scope (covering its init-declaration,
if any) is pushed and popped around the whole statement, matching §6.8.5p5.

## Dependencies

Imports [`token`](../token), [`ast`](../ast), [`types`](../types); is
imported by whatever runs semantic checks before lowering.