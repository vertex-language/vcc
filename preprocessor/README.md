# preprocessor

`package preprocessor` is translation phase 4: directive execution, macro
replacement, and include resolution.

```go
import "github.com/vertex-language/vcc/preprocessor"
```

The directive grammar lives here and nowhere else in the tree. Below this
package nothing expands and nothing interprets; above it, on `.i` input, this
package is skipped entirely and a `#` opening a line is trivia.

## Invariants

1. **Pure function of `Config`.** Same config, same input, same output, every
   time. Mounts are `fs.FS`; this package never imports `os`, never probes a
   host, never reads a clock.
2. **One diagnostic per mistake.** A bad directive is reported once. A macro
   arity error is reported at the invocation, not at every token it produced.
   A warning sited in a system header is reported once per header, not once
   per inclusion — and that includes deferred phase 1–3 diagnostics, which
   wait for the first real read so they carry the inclusion chain.
3. **Positions survive expansion.** Every token carries the `Origin` its span
   belongs to and, when a macro produced it, the chain of use sites and
   definition sites that made it. A diagnostic in expanded code points at what
   the user typed.
4. **Nothing is decoded.** Literals leave phase 4 exactly as they entered.
   `#if`'s evaluator decodes internally, for itself, and throws the result
   away.

## Usage

```go
cfg := preprocessor.Config{
	Search: []preprocessor.Mount{
		{Name: "include", FS: os.DirFS("include")},
		{Name: "/usr/include", FS: os.DirFS("/usr/include"), System: true},
	},
	Predefines: []preprocessor.Predefine{{Text: "NDEBUG"}},
	Std:        preprocessor.C17,
	Hosted:     true,
}

p := preprocessor.New(cfg)
toks, diags := p.Run(token.NewFile("main.c", src))
```

`Run` returns the token stream phase 5 reads and every diagnostic phases 1
through 4 produced, sorted. The stream is **never nil**; a file that failed to
parse still yields the tokens that were read.

The `os.DirFS` calls belong to the caller, not here. The `vcc` package composes
`sysroot.Resolve` and the `-I` flags into that `Search` list, which is the
list `vcc env` prints.

Beyond the fields above: `PreIncludes` are processed before the main input,
in order, exactly as if `#include`'d at its top (`--include`); `Epoch` clamps
`__DATE__`/`__TIME__` (see *Determinism*); `KeepComments` retains `COMMENT`
tokens for `--emit i`, which must not drop them silently; `TrackDeps` records
the include graph (see *Dependency output*). A zero `Config` preprocesses a
self-contained file with no headers and no macros — which is exactly what a
test wants.

## Where a token came from

Phase 4 breaks `token`'s second invariant — *no cross-file address space* — by
construction: its output is one sequence drawn from `main.c`, `stdio.h`, and
everything they reached. A bare `token.Token` is four fields with no file
identity and cannot say where it is.

`Origin` restores it, and carries the inclusion chain as well:

```go
type Token struct {
	Kind  token.Kind
	Flags token.Flags
	Pos   token.Pos
	End   token.Pos

	Origin *Origin    // the position space Pos and End live in
	Hide   *HideSet   // Prosser's blue paint
	Exp    *Expansion // use site, definition site, outward
}
```

`t.Text()` resolves spelling through `Origin`, exactly as `ast` resolves
identifiers through `*token.File`. Nothing carries text.

```go
for _, t := range toks {
	if t.Kind == token.IDENT && t.Text() == "malloc" {
		s := t.Site()                     // where to point
		p := s.Origin.File.Position(s.Pos)
		fmt.Printf("%s:%d:%d\n", s.Origin.Name(), p.Line, p.Column)
		for _, n := range t.Notes() {     // and where to note
			fmt.Printf("\tfrom %s\n", n.Origin.Name())
		}
	}
}
```

An `Origin` with a nil `File` is the **generated arena**: the position space
for tokens that `#` and `##` built and no file contains. See *Generated
tokens*.

## Macro expansion

The implementation is Dave Prosser's X3J11/86-196 pseudocode, function for
function: `expand`, `subst`, `glue`, `hsadd`, with `ts`/`fp`/`select`/
`stringize` as helpers. The standard's §6.10.3.4 prose is a translation of
that memo and loses detail in the translation; the memo is what `expand.go`
implements.

The detail that matters most:

```
subst(ts(T), fp(T), actuals, (HS ∩ HS') ∪ {T}, {})
```

`HS` is the macro name's hide set. `HS'` belongs to the **closing
parenthesis**, not the name. Their *intersection*, plus the macro itself, is
the new hide set. An implementation that unions them, or that ignores `HS'`,
gets DR 017's `NIL` and `a(a)` cases wrong — and those cases are why the
committee eventually specified this at all.

Three consequences worth stating, because each is a rule elsewhere and a
structural fact here:

- **A macro is disabled while its expansion is rescanned, and not while its
  arguments are expanded.** The hide set rides on the tokens, so this needs no
  context stack and no enable/disable bookkeeping.
- **Argument pre-expansion cannot reach past the invocation.** An argument is
  a closed sequence — a `stream` with no refill function — so `#define f(x) x`
  invoked as `f(g) (2)` cannot pull the `(2)` in. The structure enforces it.
- **The hide-set test precedes the search for `(`.** Reversed, finding the
  parenthesis would pop a context and re-enable the macro we are inside, which
  is subtly wrong in the way cpplib documents.

```
#define foo(x) bar x
foo(foo) (2)          →  bar foo (2)
```

A macro invocation cannot straddle a directive: text expansion stops at a
line-opening `#`, so the argument list simply runs out of tokens and is
reported as unterminated rather than silently swallowing an `#endif`.

Expansion depth is capped (`Config.MaxExpansionDepth`) not for termination —
hide sets guarantee that — but for the inputs that terminate, eventually, some
time next decade.

## Generated tokens

`stringize` returns a string literal containing concatenated spellings.
`glue` returns a token spelled `L&R`. Neither exists in any source file, and
`token`'s contract says a token is a span in a position space carrying no
text.

So phase 4 supplies a position space. `Gen` is an append-only byte buffer;
a generated token is an ordinary non-empty span in it, reached through an
`Origin` whose `File` is nil. Nothing below phase 4 learns a new concept, and
`t.Text()` has one extra case.

A pasted spelling is **re-scanned** before it becomes a token, because
§6.10.3.3p3 requires the result be a single preprocessing token and only the
scanner can say whether it is. `+` `##` `+` gives `++`; `+` `##` `x` is a
constraint violation, reported once, with both operands left in place so the
rest of the line still parses. Results are cached — macro-heavy headers paste
the same pair thousands of times.

Positions for a generated token come from its expansion chain, so a
diagnostic about a pasted identifier still underlines the invocation.

## Spacing

`--emit i` output must re-enter as `.i` input and produce a byte-identical
executable. That requires accidental pastes never happen:

```
#define PLUS +
#define EMPTY
+PLUS -EMPTY-        →  + + - -        not  ++ --
```

gcc solves this with padding tokens in the stream. vcc does not need them: the
claim is a byte-identical *executable*, not byte-identical text, so adjacency
is carried on the tokens and paste avoidance happens once, at print time.

What expansion owes the printer is accurate adjacency, and the rules are the
ones spacing actually follows:

- the first token of an expansion inherits the **invocation's** spacing;
- a substituted argument's first token inherits the **parameter's** spacing in
  the replacement list.

```
#define add(x, y, z) x + y +z;
sum = add (1,2, 3);   →  sum = 1 + 2 +3;
```

`1` is spaced because `add` was; `2` because `y` is; `3` is not, because `z`
is not. None of the three had that property when scanned.

## `#if`

§6.10.1 is a different language from §6.6, and reusing `analyzer`'s constant
folder would be the wrong semantics rather than a shortcut. There is no
`sizeof`, no enum constant, no cast; arithmetic is in `intmax_t`/`uintmax_t`
regardless of the target's `int`; `defined` is an operator; and every
identifier that survives expansion — including keywords — is `0`.

Order is the standard's, and it is not negotiable:

1. `defined X` and `defined ( X )` are resolved **before** expansion, so
   `#if defined FOO` does not expand `FOO`.
2. What remains is macro-expanded.
3. Every remaining identifier becomes `0`.

A `defined` *produced* by step 2 is undefined behavior (§6.10.1p4) — which is
license to define it. gcc and clang both evaluate it as the operator, real
system headers lean on that (Darwin's `pthread.h` tests a function-like macro
expanding to a `defined()` chain), and vcc follows — under a named warning
(`expansion-defined`), so the nonportability stays visible, once per system
header like any other.

`&&` and `||` short-circuit, and the untaken side does not report: `#if 0 &&
1/0` folds to `0` quietly, and so does the unevaluated arm of a `?:`. Division
by zero in a *taken* branch is one diagnostic, not a panic.

Inside a skipped group, conditions are **not evaluated at all** — §6.10.1p6
checks skipped groups only for nesting. This is what makes the universal
idiom safe when the header genuinely is not there:

```c
#ifdef HAVE_ZLIB
#include <zlib.h>
#endif
```

## Include search

One list, in the order §6.10.2 describes:

| | |
| --- | --- |
| `#include "..."` | the including file's own directory, then the list |
| `#include <...>` | the list |

That is the whole rule. There is no `-iquote`/`-isystem`/`-idirafter` tower,
no header maps, no overlays. The list is `Config.Search`, computed once by
the `vcc` package from `sysroot` plus `-I`, printed by `vcc env` before the build runs.

A macro that expands to a header name is handled: §6.10.2p4's form is tried
after the two literal forms fail.

**Header names are reconstructed here, not scanned.** `<stdio.h>` is one
pp-token only in this context; everywhere else it is `LSS IDENT DOT IDENT
GTR`. `include.go` recovers it from the raw bytes between `<` and `>` —
bytes, not token spellings, because `<sys/types.h>`'s slash is not a token
this package should be reassembling.

Absolute paths in `#include` are rejected: an absolute path is the build
machine leaking into the source. So are paths that escape their mount
(`../` past the root) — they are simply not found there.

## The open-once cache and include guards

Each file is read and scanned at most once per translation unit. The token
slice is cached; a second inclusion re-walks it with a fresh `Origin`, so
`__FILE__` and the inclusion chain are right without re-reading anything.

Above that sits the **multiple-include optimization**, gcc's scheme:

- entering a file sets `miValid` true and clears the recorded guard;
- the first text token outside a conditional clears `miValid`;
- any directive other than an opening conditional or the *null* directive
  clears `miValid`;
- `#else` on the outermost conditional clears its guard;
- closing the outermost `#endif` re-arms `miValid`, so only trailing
  whitespace can follow; reaching end of file with it still standing records
  the controlling macro.

A later `#include` of a file with a recorded guard that is currently defined
does not open it.

Both spellings are recognized:

```c
#ifndef FOO_H          #if !defined FOO_H
#define FOO_H          #define FOO_H
...                    ...
#endif                 #endif
```

This is the honest answer to `#pragma once`, and it needs no pragma. `#pragma
once` itself is diagnosed with a note saying so — it is an idiom ISO never
defined, and a cache keyed on a path is exactly the thing that makes
identical-input-identical-output a lie when paths alias.

## Determinism

`__DATE__`, `__TIME__`, and anything else time-shaped read `Config.Epoch`.
Nothing in this package reads a clock. `SOURCE_DATE_EPOCH` is the same
contract arc's output formats hold to, and there is no other mode — no
`--deterministic` flag, because there is nothing to switch off.

`__FILE__` expands to the path as given: as written on the command line, or as
joined from the mount name and the `#include` spelling. Never absolute.

Diagnostics sort by file, then position, then extent, stably — the contract
`token.SortDiagnostics` holds within one file, extended across the include
graph so two runs interleave identically.

## The dialect rule applies to directives

Same rule that keeps `gnu11` out of the language and MASM out of arc: a
directive dialect is a language, and languages are out.

| rejected | note |
| --- | --- |
| `#include_next` | no ISO spelling; restructure the include path so one `-I` directory wins |
| `#assert`, `#unassert` | removed from GNU C itself; no ISO equivalent |
| `#ident`, `#sccs`, `#import` | no ISO equivalent; for `#import`, write an include guard |
| `__COUNTER__` | not defined; nothing predefines it |
| `, ## __VA_ARGS__` | GNU comma-swallow; require one variadic argument, or pass the comma |
| `#pragma GCC …` | warning state is a build decision, made on the command line |
| `#pragma once` | write the guard; it is detected automatically |

`#warning` is deliberately *not* in this table: C23 standardized it (WG14
N2686), so on C11/C17 it is a version gap, not a dialect. It is diagnosed —
as a warning, which is also exactly the severity the directive asks for —
and processing continues.

What ISO defines is what runs: `#include`, `#define` with `__VA_ARGS__`, the
conditionals, `#line`, `#error`, `#pragma`, `_Pragma`, and the predefined
macros. `#pragma STDC …` is honored as ISO defines it; an unrecognized pragma
is not an error (§6.10.6) and passes through to phase 7.

## Predefined macros

`__FILE__`, `__LINE__`, `__DATE__`, `__TIME__` are computed, not stored.
`__STDC__`, `__STDC_VERSION__`, `__STDC_HOSTED__` are plain values. All seven
— plus `defined` — are `Reserved`: the program may not `#define` or `#undef`
them (§6.10.8p2). `__VCC__` is also predefined, as `1`, but it is an ordinary
macro the program may legally shadow, like any feature macro.

Everything **target-dependent** — `__CHAR_BIT__`, `__SIZEOF_LONG__`,
`__INT_MAX__` and kin — arrives through `Config.Predefines` as text. Those are
facts about a `types.Model`, and this package does not import `types`;
The `vcc` package computes them and puts them in the config. Same inversion that keeps
`sysroot` out of phase 4, for the same reason: phase 4 does not learn what a
target is.

`-D` spellings run through the same `#define` grammar a directive uses, so the
two cannot drift apart. `-D NAME` defines it as `1`. `-D` and `-U` are applied
in command-line order.

## Dependency output

`Config.TrackDeps` records every file `#include` reached, in first-seen order.
`Deps.Write` renders the one shape build systems consume: the rule, then a
phony target per header so a deleted header does not break the rebuild.

```
main.o: \
  main.c \
  include/app.h \
  /usr/include/stdio.h

include/app.h:

/usr/include/stdio.h:
```

That is `-MMD -MF -MP` collapsed. There is no flag to rename or requote the
target: the file is generated for machines, and machines read this one.

## Files

| | |
| --- | --- |
| `preprocessor.go` | the driver: directive or text, per file |
| `config.go` | mounts, predefines, dialect, limits |
| `pptoken.go` | the pp-token view; the generated arena; stringize |
| `directive.go` | the directive line grammar |
| `condition.go` | the `#if` stack, group skipping |
| `eval.go` | the `#if` constant evaluator |
| `macro.go` | the macro table, §6.10.3p2 |
| `expand.go` | hide sets and Prosser's four functions |
| `predefined.go` | `__FILE__`, `__STDC_*`, the epoch clamp |
| `include.go` | §6.10.2 search, open-once cache, guard detection, deps |
| `position.go` | `Origin`, `Site`, `Expansion` |

## What this package requires of `scanner`

`ScanPP` mode, for three reasons, all of them because a pp-token is not yet a
C token:

- **pp-numbers keep their spans unclassified.** `0779` and `1e+` are legal
  pp-numbers and illegal C constants, and `#if 0` may legally hide either.
- **Malformed-literal diagnostics defer to phase 7.** They fire for the tokens
  that survive phase 4, and only those.
- **A line-opening `#` is returned as `HASH`.** The ordinary mode consumes it
  as trivia and reports once per file, which is correct for `.i` input and
  wrong here.

Everything routes through one `scanPP` helper, so this is one function to
change when the mode lands.

## Dependencies

Imports [`token`](../token), [`scanner`](../scanner), and `io/fs` — never
`os`, and nothing above it. It is the only package below the CLI that touches
file *contents*, which is what makes the include list inspectable data rather
than driver folklore.

Imported by [`parser`](../parser), which consumes its output, and by
the `vcc` package, which builds its `Config`.