# scanner

`package scanner` turns a `*token.File` into a complete token slice.

```
import "github.com/vertex-language/vcc/scanner"
```

The whole unit is tokenized up front. Every scan path advances at least one
byte; malformed input yields an exact span and **one** diagnostic, never a
cascade. Nothing is interpreted: literals keep their raw spelling and every
identifier is an `IDENT` — typedef-ness is the parser's business.

## Usage

```go
f := token.NewFile("a.c", src)
toks, diags := scanner.Scan(f, 0)
```

`Scan` is the entire API. The slice always ends in an `EOF` token (the one
zero-width span, positioned at `f.Size()`). Diagnostics are sorted, with
phase 1–2 diagnostics from `token.NewFile` already merged in.

### Mode

`ScanComments` keeps `COMMENT` tokens (`/* */` and `//`) in the stream;
without it they're trivia, still reachable via `token.File.Between`. Block
comments don't nest; an unterminated `/*` is one diagnostic and one token to
EOF.

## Maximal munch

Longest token always. The sharp edges are the standard's:

| Source | Tokens |
| --- | --- |
| `a+++b` | `a` `++` `+` `b` |
| `a+++++b` | `a` `++` `++` `+` `b` — syntax error, as intended |
| `..` | `.` `.` |
| `...` | `...` |
| `a>>b` | `a` `>>` `b` — never synthesized from two `>`s |

`.` starts a floating constant only when a digit follows and it isn't the
head of `...`.

**Literal prefixes munch too**: `u`, `U`, `L` before a quote are part of the
literal, not a separate identifier — `u8"…"`, `u"…"`, `U"…"`, `L"…"`,
`u'…'`, `U'…'`, `L'…'` each scan as one `STRING_LIT` or `CHAR_LIT` token.
`u8` only takes the string form (there's no `u8'…'` in C11). A `u`, `U`, or
`L` *not* followed by a quote scans as an ordinary identifier, so `U + 1`
is `IDENT ADD INT_LIT`, not the start of a literal.

## Digraphs and directives

Digraphs scan as their canonical kinds with `FlagDigraph` set — so
`a<:2:>` and `a[2]` produce identical token kinds and differ only in that
flag; the advisory bracket stack also treats them as their canonical
bracket, so `<: :>` balances cleanly. Trigraphs never reach the scanner
(`token.NewFile` replaced them in phase 1).

vcc scans preprocessed source: a `#` (or `%:`) opening a logical line is
consumed to end of line as trivia and reported **once per file**, suggesting
the source be preprocessed. `#` anywhere else scans as `HASH` for the parser
to reject.

## Identifiers

Keyword lookup is by exact spelling (`token.Lookup`); everything else is
`IDENT`. Universal character names (`\uXXXX`, `\UXXXXXXXX`) inside an
identifier stay undecoded — the span keeps the literal `\u`/`\U` spelling —
and a malformed one (too few hex digits) is one diagnostic, scanning
continuing past it. A bare `\` not introducing a UCN is illegal: one
diagnostic (`stray '\' in program`) and an `ILLEGAL` token, not folded into
the surrounding identifier.

## Literals stay undecoded

`0x1Fu` stays five bytes; string and character literals keep prefixes and
quotes. Adjacent string literals are **not** concatenated — that's phase 6,
above this package.

Numeric scanning enforces the lexical grammar, one diagnostic per run:

- `0779` — one malformed octal constant, not two numbers.
- `0779.5` is a decimal float, not an octal error — the `.` changes the
  whole run's classification.
- `0x1.8` without a `p` exponent is reported; `0x1.8p3` and `0x1p+4f` are
  clean.
- Binary exponents take decimal digits (`p+4`, not `p+g`).
- Suffixes are validated: `ul`, `llu`, `ULL` pass (at most one `u` part and
  one `l` part, either order, `ll` case-consistent); `lul`, `lL` are one
  diagnostic each.
- C11 has no digit separators: `1_024` is `1` then identifier `_024`, not
  one malformed number.

`''` is reported (at least one `CChar` required); the multi-character `'ab'`
scans clean — its value is a decoding concern. Escape sequences are
recognized but not decoded: the simple set (`\' \" \? \\ \a \b \f \n \r \t
\v`), octal (`\0` through 3 octal digits), and `\x` (at least one hex digit
required, else one diagnostic) all just advance past their spelling.
`\u`/`\U` inside a literal reuse the same UCN scanning as identifiers.
Unknown escapes (`\q`) are one diagnostic. A backslash immediately before a
raw line terminator is left for the enclosing literal to report as
unterminated, since phase 2 has already spliced genuine backslash-newline
continuations away.

Because `token.NewFile` splices lines in phase 2, a raw line terminator
inside a literal is exactly what it looks like: unterminated, reported once,
token still emitted.

## Flags

Every token carries `Flags`, computed as it's emitted:

- **`FlagAdjacent`** — no trivia (space, comment, newline) since the
  previous token. Reset false after any whitespace or comment, true again
  once a token is emitted; useful for macro-paste-style adjacency checks.
- **`FlagNLBefore`** — at least one line terminator appeared since the
  previous token (across any intervening comments too).
- **`FlagDigraph`** — this token was spelled as a digraph (`<: :> <% %>
  %: %:%:`) rather than its canonical punctuator.

## Diagnostics and recovery

Every reported span is non-empty and clamped to the source. An advisory
bracket stack (never affecting tokenization) reports:

- a closer with nothing open → `unmatched )`, once;
- a mismatch → blame the *opener* (`unclosed {, closed by )`), then go quiet;
- EOF with openers left → the innermost one.

After the first report the stack stops talking. One brace typo produces one
message.