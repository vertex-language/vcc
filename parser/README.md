# parser

`package parser` turns a `*token.File` into an `*ast.File` plus a sorted
diagnostic slice.

```
import "github.com/vertex-language/vcc/parser"
```

Recursive descent for declarations and statements, precedence climbing for
expressions, and one typedef table instead of any rollback machinery — C's
ambiguities are semantic, not structural.

The parser interprets nothing except typedef names (below). It decides which
production applies and where each node begins and ends; it does not decode
literals or check §6.7 constraints beyond the production admitting the
tokens.

**A partial parse is a usable one.** Every entry point returns a node —
a `Bad*` placeholder if it must — so consumers read a tree, not a success
flag.

## Usage

```go
unit := token.NewFile("a.c", src)
file, diags := parser.ParseFile(unit, parser.DefaultMode)
defer file.Release()
```

`ParseFile` runs the scanner itself. The tree is **never nil**; diagnostics
from phases 1–2, scanning and parsing arrive merged and sorted. Input is
expected to be preprocessed C.

### Modes

| Mode | Effect |
| --- | --- |
| `ParseComments` | Retains comment tokens on the `File`. |
| `SkipBodies` | Function bodies skipped balanced, not parsed — declarations, prototypes and typedefs still land. A fast structural pass. |
| `Tolerant` | Keeps going past the resync budget. For editors; wasteful in batch builds. |

`DefaultMode` is 0.

## Lifetime

Nodes come from an unexported arena; `ast` sees it only through the
one-method `Releaser` interface. **Release is a promise, not a check** —
nothing detects a kept pointer. Copy what you need first.

## The typedef table

C's grammar isn't context-free: only prior declarations decide whether an
identifier is a `TypedefName`. The table is a per-scope set of *names* —
nothing about the types they denote. It follows scope exactly, including
immediate visibility after a declarator: in
`typedef int T; void f(void) { T T; T * x; }` the first `T` of `T T;` is a
type; by the next statement `T * x` multiplies.

Enumeration constants are ordinary names: they shadow typedefs in the table
like any other declaration. Function parameters (prototype and K&R) are
declared into the scope that becomes the body's, so a parameter shadowing a
typedef disambiguates correctly inside the body.

Three productions use the standard's tie-breakers:

- **Parenthesized parameter**: in `int f(int (T))`, if `T` is a typedef the
  parens read as an abstract function declarator (§6.7.6.3p11, "treated as
  a typedef name if possible"); otherwise they group a concrete declarator
  naming the parameter.
- **`_Atomic`**: type specifier when `(` immediately follows, qualifier
  otherwise.
- **Labels beat typedefs**: `T: ;` always labels, even when `T` names a
  type — label names are their own namespace, checked before declaration
  start.

With the table, the classic ambiguities are deterministic:

- **Declaration vs. expression statement** — settled by the first token.
- **Cast vs. parenthesized expression** — `(T) - x` is a cast iff `T` is in
  the table.
- **`sizeof ( … )`** and **`_Alignof ( … )`** — the parenthesized form reads
  as a `TypeName` iff the token after `(` opens a type; otherwise `sizeof`
  takes an operand.
- **Compound literals**: a `( TypeName )` immediately followed by `{` is a
  `CompoundLit`, not a cast — checked in both the ordinary cast position and
  inside `sizeof (…)`, and the result still runs through postfix suffixes
  (`(T){1}[0]` etc.).

Dangling else binds to the nearest unmatched `if`, which recursive descent
produces naturally.

## Expressions

Precedence climbing collapses the ten binary levels plus comma into one
`parseBinary`/`BinaryExpr` shape; `token.Kind.Precedence()` supplies the
level. `?:` and assignment are handled separately since they're not part of
the uniform left-associative tower: conditional expressions recurse
right-associatively on their `Else` branch, and assignment operators are
right-associative with a left operand the grammar constrains to a unary
expression — a check on the finished tree, not a parsing decision, the same
policy as `ConstantExpression`.

String literals: a run of adjacent `STRING_LIT` tokens (phase-6
concatenation candidates) collects into one `StringLit` with one span per
segment, prefixes included. `_Generic` selections parse their controlling
expression, then each `TypeName : expr` (or `default : expr`) association
in order.

## The tolerated-spelling carve-out

A closed, documented set of pre-standard/GNU spellings is parsed and
discarded wherever declaration specifiers, declarators, or their qualifier
lists are read — never given semantics, never allowed anywhere else,
visible as absent in `--emit cir`:

- Bare spellings: `__restrict`, `__restrict__`, `__extension__`, `__const`,
  `__volatile__`, `__signed__`.
- Spellings followed by a balanced `(…)` group, which is skipped whole:
  `__attribute__`, `__declspec`, `__asm`, `__asm__`.

The list grows only on evidence from real system headers, with the table
updated in the same commit as the CI gate that motivated it.

`__inline`, `__inline__` and `__forceinline` were on it and are not any more.
They are not decoration: they are `inline` under another name, and discarding
one turns an inline definition into an ordinary one. Darwin's `<stdio.h>`
resolves `__header_inline` to `extern __inline` for any compiler defining
`__GNUC__`, so with the spelling dropped every unit that included it defined
`__sputc` outright and two such units would not link. They parse as
`token.INLINE` now, and mean §6.7.4 exactly as the standard spelling does.

## Digraphs

`<:  :>  <%  %>` collapse to their canonical punctuators (`[ ] { }`) at the
token level, so the parser builds the same tree for `a<:2:> = <%1, 2%>;` as
for the bracket/brace spelling; recovering the original spelling for
diagnostics is `token`'s job, not this package's.

## Errors and recovery

**One recoverable diagnostic, never a cascade.** After an error the parser
goes quiet until it consumes a token, and never reports twice at one
position. `advanceTo` resyncs to a follow set, stepping *over* balanced
bracket groups. Past `maxResync` (100) attempts it stops reporting and runs
to EOF, unless `Tolerant`.

- **`maxDepth` (1000)** caps nesting in declarators, statements, types and
  expressions.
- **Progress checks** in every list loop (external declarations, block
  items, struct members, initializer items, call arguments) force a resync
  if nothing was consumed.

`Bad*` spans are non-empty even when nothing was consumed.

## Deliberate non-decisions

These parse and are checked later:

- **Specifier multisets** — `long char` vs. `long long unsigned int`, "at
  most one storage class": constraints for the type-building phase.
- **Qualifier placement** — `restrict` on non-pointers is semantic.
- **`static` / `*` in array declarators** outside function parameters
  (§6.7.6.2).
- **Bit-field widths, VLA placement, initializer arity, enum values.**
- **Label discipline** — `case`/`default` outside `switch`, `goto` targets.
- **K&R definitions** — that the declarations match the identifiers.

One thing the grammar itself demands and the parser enforces: a declaration
needs at least one declaration specifier, so a stray file-scope `;` is
reported once and kept as an `EmptyDecl`.

Declarators are kept exactly as written — `int *(*f[3])(void)` stays a
declarator tree the type-construction phase reads inside-out.

## Dependencies

Imports [`token`](../token), [`scanner`](../scanner), [`ast`](../ast); is
imported by nothing below it.