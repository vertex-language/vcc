package preprocessor

import (
	"strings"

	"github.com/vertex-language/vcc/token"
)

// Token is phase 4's view of a preprocessing token.
//
// It is token.Token plus the three things phase 4 needs and the front end
// below it does not: which file the span belongs to (Origin), the hide set
// Prosser's algorithm threads through expansion (Hide), and the expansion
// chain a diagnostic reads positions out of (Exp).
//
// Everything token.Token guarantees still holds: no text is carried, spans
// are non-empty, literals stay undecoded. Text resolves through Origin, the
// same way ast resolves identifiers through *token.File.
type Token struct {
	Kind  token.Kind
	Flags token.Flags
	Pos   token.Pos
	End   token.Pos

	Origin *Origin
	Hide   *HideSet
	Exp    *Expansion
}

// Text returns the token's raw spelling.
//
// token.File.Slice returns the translated bytes — what the scanner read —
// which is what every consumer here wants: a macro name is compared against
// the spelling after trigraph replacement and line splicing, because that is
// what the scanner made a token out of. Raw is for underlining, and phase 4
// underlines through Site rather than through spelling.
func (t Token) Text() string {
	switch {
	case t.Origin == nil:
		return ""
	case t.Origin.File != nil:
		return string(t.Origin.File.Slice(t.Pos, t.End))
	case t.Origin.Gen != nil:
		return t.Origin.Gen.slice(t.Pos, t.End)
	default:
		return ""
	}
}

// Spaced reports whether the token was separated from the one before it by
// whitespace or a comment. It is the inverse of token.FlagAdjacent, and it is
// what --emit i consults when deciding whether to print a space.
func (t Token) Spaced() bool { return !t.Flags.Has(token.FlagAdjacent) }

// StartsLine reports whether a line terminator preceded the token. A '#' with
// this flag set opens a directive; a '#' without it is the punctuator.
//
// This is exactly why scanner.ScanPP seeds nlBefore true: a '#' in column 1 of
// line 1 opens a logical line, and nothing precedes it to say so.
func (t Token) StartsLine() bool { return t.Flags.Has(token.FlagNLBefore) }

// Is reports whether the token is an identifier spelled name.
//
// Directive keywords (define, ifdef, include, ...) are not token kinds — they
// are ordinary identifiers that mean something only after a line-opening '#'.
// C keywords are checked too: `#define int x` is a constraint violation, not a
// parse failure, and `#if sizeof` is 0 rather than an error, so both paths
// need to recognize a keyword by spelling.
func (t Token) Is(name string) bool {
	if t.Kind != token.IDENT && !t.Kind.IsKeyword() {
		return false
	}
	return t.Text() == name
}

// IsName reports whether the token may be used as a macro name or parameter:
// an identifier, or a keyword (which is a constraint violation to define, but
// a violation the caller must diagnose rather than fail to parse).
func (t Token) IsName() bool {
	return t.Kind == token.IDENT || t.Kind.IsKeyword()
}

// IsPPNumber reports whether the token is a preprocessing number.
//
// A pp-number is not yet a C constant: 0779 and 1e+ are legal pp-numbers and
// illegal integer/floating constants, and #if 0 may legally hide either.
// scanner.scanNumber already consumes the whole run — digits, letters, '.',
// and exponent signs — as one token, which is the pp-number rule; what it
// additionally does is classify and validate. Phase 4 keeps the classification
// and drops the diagnostics sited inside excluded groups.
func (t Token) IsPPNumber() bool {
	return t.Kind == token.INT_LIT || t.Kind == token.FLOAT_LIT
}

// Gen is the position space for tokens no source file contains: the results
// of # (stringize) and ## (paste).
//
// Prosser's stringize returns "a single string literal token containing the
// concatenated spellings", and glue returns a token spelled L&R. Neither
// exists in any file, but token's contract says a token is a span in a
// position space and carries no text. Gen supplies a position space:
// spellings are appended to one buffer and a generated token is an ordinary
// non-empty span in it, so nothing below phase 4 learns a new concept.
//
// The buffer only grows, so a Pos handed out early stays valid.
type Gen struct {
	buf    []byte
	origin *Origin
}

// NewGen returns an empty generated arena.
func NewGen() *Gen {
	g := &Gen{buf: make([]byte, 0, 256)}
	g.origin = &Origin{Gen: g}
	return g
}

// Origin returns the arena's position space, for tokens minted from it.
func (g *Gen) Origin() *Origin { return g.origin }

// slice mirrors token.File.Slice: Pos is offset+1, so the zero value is
// NoPos and a real position at offset 0 is distinguishable from it.
func (g *Gen) slice(pos, end token.Pos) string {
	lo, hi := int(pos)-1, int(end)-1
	if lo < 0 || hi > len(g.buf) || lo > hi {
		return ""
	}
	return string(g.buf[lo:hi])
}

// intern appends s and returns its span.
func (g *Gen) intern(s string) (pos, end token.Pos) {
	pos = token.Pos(len(g.buf) + 1)
	g.buf = append(g.buf, s...)
	return pos, token.Pos(len(g.buf) + 1)
}

// Mint appends the spelling and returns a token of the given kind spanning
// it. The caller supplies flags, hide set and expansion chain.
func (g *Gen) Mint(kind token.Kind, spelling string) Token {
	pos, end := g.intern(spelling)
	return Token{Kind: kind, Pos: pos, End: end, Origin: g.origin}
}

// Stringize implements §6.10.3.2p2: the tokens of one argument, in order,
// spelled as written, separated by a single space wherever they were
// separated at all, with \ and " escaped inside string and character
// literals — as one STRING_LIT token.
//
// Leading and trailing whitespace is dropped, which falls out of walking
// tokens rather than slicing source. Walking is also required rather than
// merely convenient: an argument's tokens may come from several files, so
// there is no one span to slice.
//
// Escaping is by byte, not by rune. Only \ and " need it, both ASCII, and a
// multi-byte UTF-8 sequence inside a literal passes through untouched because
// none of its bytes can equal either.
func (g *Gen) Stringize(arg []Token) Token {
	var b strings.Builder
	b.WriteByte('"')
	for i, t := range arg {
		if i > 0 && t.Spaced() {
			b.WriteByte(' ')
		}
		s := t.Text()
		if t.Kind == token.STRING_LIT || t.Kind == token.CHAR_LIT {
			for j := 0; j < len(s); j++ {
				if s[j] == '\\' || s[j] == '"' {
					b.WriteByte('\\')
				}
				b.WriteByte(s[j])
			}
			continue
		}
		b.WriteString(s)
	}
	b.WriteByte('"')
	return g.Mint(token.STRING_LIT, b.String())
}

// Paste appends the concatenation of two spellings and returns the span,
// which the caller must re-scan: §6.10.3.3p3 requires the result be a single
// valid preprocessing token, and only the scanner can say whether it is.
func (g *Gen) Paste(l, r Token) (pos, end token.Pos) {
	return g.intern(l.Text() + r.Text())
}

// Buffer exposes the arena's bytes so a diagnostic can be rendered against a
// generated token, and so tests can assert on what expansion built.
func (g *Gen) Buffer() []byte { return g.buf }
