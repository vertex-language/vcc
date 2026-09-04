# token

`package token` defines the lexical vocabulary of C11 (ISO/IEC 9899:2011;
C17 is lexically identical) and the per-translation-unit position space
every span in the front end resolves through.

```
import "github.com/vertex-language/vcc/token"
```

No scanner, no parser — just what they share: `Kind`, `Token`, `File`,
`Diagnostic`.

## Invariants

1. **Nothing below the parser interprets.** Tokens carry no text; literals
   arrive undecoded and resolve through the `File` that produced them.
2. **No cross-file address space.** `Pos` is per-`File`. A `Pos` from one
   file is meaningless in another.
3. **Every span is non-empty.** `End > Pos`, including for `ILLEGAL`. The
   scanner's `EOF` token — one zero-width span at `Pos(Size())` — is the one
   deliberate exception.

## Scope

vcc consumes **preprocessed** source (translation phases 5–7). `HASH` and
`HASHHASH` exist because `#` and `##` are punctuators, but there is no
directive grammar anywhere in vcc. Run `cpp -E` first.

Typedef-name disambiguation is scope-dependent and cannot be a token
property; it lives in the parser's typedef table.

## `Pos`

```go
type Pos int32
```

`Pos` is a translated-text offset plus one, so the zero value `NoPos` is
invalid and distinguishable from a real position at offset 0. Fields like a
delimiter that was never written (an omitted `Rparen`, a `for`-init clause's
absent `Semi1`) hold `NoPos`; `Pos.IsValid()` reports whether a `Pos` is
real.

## `File`

`NewFile` runs phases 1–2 before tokenization — trigraph replacement, then
line splicing — and maps translated text back to raw bytes.

```go
src := []byte("in\\\nt x = 1;")
f := token.NewFile("a.c", src)

f.Text()   // "int x = 1;"      translated
f.Source() // "in\\\nt x = 1;"  raw

pos, end := f.Pos(0), f.Pos(3)
f.Slice(pos, end) // "int"       what the scanner read — feed to decoders
f.Raw(pos, end)   // "in\\\nt"   what the user typed — underline in diagnostics
```

`Raw` widens to cover a whole splice or trigraph when a span cuts through
one. `Position` (offset/line/column) is in raw bytes, so diagnostics line up
with what the user typed.

Trigraph replacements are reported at `Warn` severity. A non-empty file must
end in a line terminator not preceded by a backslash; one that doesn't gets
one diagnostic (this includes a file that ends mid-splice — `int\`+newline
with nothing after — reported as an unspliced-ending file, not silently
dropped). An empty file gets no diagnostics at all.

Sources with no `?` and no `\` take a fast path where translated offsets are
raw offsets and `Raw` degrades to a plain slice of the source — no mapping
table is built. Any other source builds a byte-for-byte translated→raw
mapping during phase 1–2 translation.

`f.Between(prev, next)` returns the raw trivia (whitespace, comments,
spliced-away bytes) between two tokens — for formatters. It keeps a splice
in the gap exactly as typed, since that's still what a formatter must
reproduce or deliberately normalize.

## `Token`

```go
type Token struct {
	Kind  Kind
	Flags Flags
	Pos   Pos // inclusive
	End   Pos // exclusive
}
```

| Flag | Meaning |
| --- | --- |
| `FlagAdjacent` | No whitespace/comment separates this token from the previous. |
| `FlagNLBefore` | A line terminator appeared before this token. |
| `FlagDigraph` | Punctuator was spelled as a digraph (`<:`, `%:`, …). |

The parser is not adjacency-sensitive; all three flags exist for diagnostics
and formatters.

## `Kind`

`ILLEGAL`, `EOF`, `COMMENT`, `IDENT`, four literal kinds (`INT_LIT`,
`FLOAT_LIT`, `CHAR_LIT`, `STRING_LIT`), the punctuators, and the 44 keywords
of §6.4.1 (including `_Imaginary`, reserved even without Annex G).

```go
token.Lookup("typedef") // token.TYPEDEF
token.Lookup("T")       // token.IDENT — typedef-ness is the parser's call
```

`IsLiteral`, `IsPunct`, and `IsKeyword` classify a `Kind` by which fenced
range of the enum it falls in — useful for callers that want "any literal"
or "any keyword" without a long `switch`.

Digraphs have no `Kind` of their own: `<:` is `LBRACK` with `FlagDigraph`
set. There are no synthesized kinds; `>>` is one scanned token, never built
from two `>`s even when `SHR_ASSIGN`'s three-character lookahead is right
next to it.

`Kind.String()` returns the keyword or punctuator's exact spelling; kinds
with no fixed spelling (`IDENT`, `INT_LIT`, …) return the class name
instead, and an out-of-range value renders as `Kind(n)` rather than
panicking.

## Precedence

```go
func (k Kind) Precedence() int // LowestPrec (0) for non-binary operators
```

Covers the ten binary levels plus `COMMA` below them. Assignment and `? :`
are right-associative and not driven by this table, and report
`LowestPrec` like any other non-binary token.

## Diagnostics

```go
type Severity uint8 // Note, Warn, Error
```

There's no non-pedantic mode: a constraint violation is `Error` because the
standard says "shall," not because vcc picked a strictness level.

Phases 1–2 are the only work here that reports on its own; their
diagnostics live on the `File` via `f.Diagnostics()`, and the scanner and
parser merge them into their own slices as they run. `SortDiagnostics`
orders by position, then extent, then message, stably — so diagnostics from
different phases interleave deterministically once merged.

`Diagnostic.Print(f)` renders one line through the `File` that owns its
span — `name:line:col: severity: message` — in raw (as-typed) coordinates,
since that's what a human reading the diagnostic actually typed.