// Package scanner turns a *token.File into a complete token slice.
//
// The whole unit is tokenized up front. Every scan path advances at
// least one byte; malformed input yields an exact span and one
// diagnostic, never a cascade. Nothing is interpreted: literals keep
// their raw spelling, and typedef-ness is the parser's business.
package scanner

import (
	"bytes"
	"fmt"

	"github.com/vertex-language/vcc/token"
)

// Mode controls optional scanner behavior.
type Mode uint

const (
	// ScanComments keeps COMMENT tokens in the stream; without it
	// they are trivia, still reachable via token.File.Between.
	ScanComments Mode = 1 << iota

	// ScanPP scans preprocessing tokens: the input is a source file,
	// not the output of a preprocessor. Four things change, all
	// because this package is now running below phase 4 instead of
	// above it.
	//
	//  1. A line-opening # is a HASH token, not trivia, and the
	//     once-per-file "run the preprocessor first" report is off.
	//     A directive in a .c file is not a mistake.
	//  2. The bracket stack is off. `#define BEGIN {` is a real and
	//     common header idiom; bracket balance across a whole file is
	//     a claim about preprocessed source, not about a header.
	//  3. The first token carries FlagNLBefore, because it does open
	//     a logical line — which is what makes a # in column 1 of
	//     line 1 a directive.
	//  4. Value-level literal diagnostics are deferred. A pp-number
	//     is not yet a constant: 0779 and 10.12.2 are legal
	//     pp-numbers, and a macro body or an excluded group may
	//     legally hold what phase 7 would reject. Classification
	//     (INT_LIT vs FLOAT_LIT) still happens — phase 4 needs it —
	//     but the suffix/digit/exponent/escape reports fire only for
	//     tokens that survive: the #if evaluator decodes and reports
	//     the ones it consumes, and everything that reaches phase 5
	//     is scanned again without ScanPP. Token-formation errors
	//     (unterminated literals, stray '\', illegal characters)
	//     still report here: they are about whether a pp-token
	//     exists, not what it is worth.
	ScanPP
)

// Scan is the entire API. The slice always ends in an EOF token (the
// one zero-width span). Diagnostics are sorted, with phase 1–2
// diagnostics from token.NewFile already merged in.
func Scan(f *token.File, mode Mode) ([]token.Token, []token.Diagnostic) {
	s := &scanner{
		f:        f,
		text:     f.Text(),
		mode:     mode,
		lineOpen: true,
		nlBefore: mode&ScanPP != 0,
	}
	s.diags = append(s.diags, f.Diagnostics()...)
	for {
		s.skipTrivia()
		if s.off >= len(s.text) {
			break
		}
		s.scanToken()
	}
	s.emit(token.EOF, s.off) // the one zero-width span
	if s.mode&ScanPP == 0 {
		s.atEOF()
	}
	token.SortDiagnostics(s.diags)
	return s.toks, s.diags
}

type scanner struct {
	f    *token.File
	text []byte
	mode Mode
	off  int

	toks  []token.Token
	diags []token.Diagnostic

	adjacent bool // no trivia since the previous token
	nlBefore bool // line terminator since the previous token
	lineOpen bool // no token yet on this logical line
	quietTok bool // current token already reported

	hashReported bool // the once-per-file directive-line report

	brackets     []bracket
	bracketQuiet bool
}

func (s *scanner) peek(i int) byte {
	if s.off+i < len(s.text) {
		return s.text[s.off+i]
	}
	return 0
}

// ---- trivia ----

func (s *scanner) skipTrivia() {
	for s.off < len(s.text) {
		switch c := s.text[s.off]; {
		case c == ' ' || c == '\t' || c == '\v' || c == '\f':
			s.off++
			s.adjacent = false

		case c == '\n' || c == '\r':
			s.off++
			if c == '\r' && s.off < len(s.text) && s.text[s.off] == '\n' {
				s.off++
			}
			s.adjacent = false
			s.nlBefore = true
			s.lineOpen = true

		case c == '/' && s.peek(1) == '/':
			start := s.off
			for s.off < len(s.text) && s.text[s.off] != '\n' && s.text[s.off] != '\r' {
				s.off++
			}
			s.comment(start)

		case c == '/' && s.peek(1) == '*':
			start := s.off
			s.off += 2
			closed := false
			for s.off < len(s.text) {
				if s.text[s.off] == '*' && s.peek(1) == '/' {
					s.off += 2
					closed = true
					break
				}
				s.off++
			}
			if !closed {
				s.report(token.Error, start, s.off, "unterminated /* comment")
			}
			s.comment(start)

		default:
			return
		}
	}
}

func (s *scanner) comment(start int) {
	if bytes.ContainsAny(s.text[start:s.off], "\n\r") {
		s.nlBefore = true
	}
	if s.mode&ScanComments != 0 {
		s.emit(token.COMMENT, start)
	}
	s.adjacent = false
}

// ---- tokens ----

func (s *scanner) scanToken() {
	s.quietTok = false
	c := s.text[s.off]

	// A # (or %:) opening a logical line is consumed to end of line
	// as trivia and reported once per file. # anywhere else scans as
	// HASH for the parser to reject.
	//
	// Under ScanPP it is a HASH like any other: the directive grammar
	// lives in the preprocessor, and it needs the token.
	if s.lineOpen && s.mode&ScanPP == 0 && (c == '#' || (c == '%' && s.peek(1) == ':')) {
		s.directiveLine()
		return
	}
	s.lineOpen = false

	switch {
	case c == 'u' || c == 'U' || c == 'L':
		s.scanIdentOrLiteralPrefix()
	case isIdentStart(c) || c == '\\':
		s.scanIdent(s.off)
	case isDigit(c) || (c == '.' && isDigit(s.peek(1))):
		s.scanNumber()
	case c == '\'':
		s.scanChar(s.off)
	case c == '"':
		s.scanString(s.off)
	default:
		s.scanPunct()
	}
}

func (s *scanner) directiveLine() {
	start := s.off
	for s.off < len(s.text) && s.text[s.off] != '\n' && s.text[s.off] != '\r' {
		s.off++
	}
	// A line marker is not a directive. `# 42 "foo.c" 3` is what gcc, clang
	// and vcc's own --emit i put in preprocessed output to say where the
	// following lines came from, and .i input is exactly that output — so
	// warning about it would fire on every file this scanner is meant to
	// read. It carries no program text and is skipped.
	if !s.hashReported && !isLineMarker(s.text[start:s.off]) {
		s.hashReported = true
		s.report(token.Warn, start, s.off,
			"preprocessor directive in scanner input; vcc scans preprocessed source — run the preprocessor first")
	}
	s.adjacent = false
}

// isLineMarker reports whether a line-opening '#' begins a line marker: the
// '#' followed by a decimal line number, and then whatever the producer chose
// to add. `#line 42 "foo.c"` is the ISO spelling of the same thing and is
// recognized too.
func isLineMarker(line []byte) bool {
	i := 0
	for i < len(line) && (line[i] == '#' || line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if rest := line[i:]; len(rest) >= 4 && string(rest[:4]) == "line" {
		i += 4
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
	}
	if i >= len(line) || line[i] < '0' || line[i] > '9' {
		return false
	}
	return true
}

// ---- punctuators: maximal munch, digraphs collapse to canonical kinds ----

func (s *scanner) scanPunct() {
	start := s.off
	c, c1, c2 := s.text[s.off], s.peek(1), s.peek(2)
	var k token.Kind
	var fl token.Flags
	adv := 1

	switch c {
	case '[':
		k = token.LBRACK
	case ']':
		k = token.RBRACK
	case '(':
		k = token.LPAREN
	case ')':
		k = token.RPAREN
	case '{':
		k = token.LBRACE
	case '}':
		k = token.RBRACE
	case ';':
		k = token.SEMI
	case ',':
		k = token.COMMA
	case '?':
		k = token.QUESTION
	case '~':
		k = token.TILDE
	case '.':
		if c1 == '.' && c2 == '.' {
			k, adv = token.ELLIPSIS, 3
		} else {
			k = token.PERIOD // ".." is PERIOD PERIOD
		}
	case '+':
		switch c1 {
		case '+':
			k, adv = token.INC, 2
		case '=':
			k, adv = token.ADD_ASSIGN, 2
		default:
			k = token.ADD
		}
	case '-':
		switch c1 {
		case '>':
			k, adv = token.ARROW, 2
		case '-':
			k, adv = token.DEC, 2
		case '=':
			k, adv = token.SUB_ASSIGN, 2
		default:
			k = token.SUB
		}
	case '*':
		if c1 == '=' {
			k, adv = token.MUL_ASSIGN, 2
		} else {
			k = token.MUL
		}
	case '/':
		if c1 == '=' {
			k, adv = token.QUO_ASSIGN, 2
		} else {
			k = token.QUO // comments were consumed as trivia
		}
	case '!':
		if c1 == '=' {
			k, adv = token.NEQ, 2
		} else {
			k = token.NOT
		}
	case '=':
		if c1 == '=' {
			k, adv = token.EQL, 2
		} else {
			k = token.ASSIGN
		}
	case '^':
		if c1 == '=' {
			k, adv = token.XOR_ASSIGN, 2
		} else {
			k = token.XOR
		}
	case '&':
		switch c1 {
		case '&':
			k, adv = token.LAND, 2
		case '=':
			k, adv = token.AND_ASSIGN, 2
		default:
			k = token.AND
		}
	case '|':
		switch c1 {
		case '|':
			k, adv = token.LOR, 2
		case '=':
			k, adv = token.OR_ASSIGN, 2
		default:
			k = token.OR
		}
	case '<':
		switch {
		case c1 == '<' && c2 == '=':
			k, adv = token.SHL_ASSIGN, 3
		case c1 == '<':
			k, adv = token.SHL, 2
		case c1 == '=':
			k, adv = token.LEQ, 2
		case c1 == ':':
			k, adv, fl = token.LBRACK, 2, token.FlagDigraph
		case c1 == '%':
			k, adv, fl = token.LBRACE, 2, token.FlagDigraph
		default:
			k = token.LSS
		}
	case '>':
		switch {
		case c1 == '>' && c2 == '=':
			k, adv = token.SHR_ASSIGN, 3
		case c1 == '>':
			k, adv = token.SHR, 2 // one scanned token, never synthesized
		case c1 == '=':
			k, adv = token.GEQ, 2
		default:
			k = token.GTR
		}
	case ':':
		if c1 == '>' {
			k, adv, fl = token.RBRACK, 2, token.FlagDigraph
		} else {
			k = token.COLON
		}
	case '%':
		switch {
		case c1 == ':' && c2 == '%' && s.peek(3) == ':':
			k, adv, fl = token.HASHHASH, 4, token.FlagDigraph
		case c1 == ':':
			k, adv, fl = token.HASH, 2, token.FlagDigraph
		case c1 == '>':
			k, adv, fl = token.RBRACE, 2, token.FlagDigraph
		case c1 == '=':
			k, adv = token.REM_ASSIGN, 2
		default:
			k = token.REM
		}
	case '#':
		if c1 == '#' {
			k, adv = token.HASHHASH, 2
		} else {
			k = token.HASH
		}
	default:
		s.off++
		s.other(start, fmt.Sprintf("illegal character %q", c))
		return
	}

	s.off += adv
	// Bracket balance is a claim about preprocessed source. A header
	// that defines `{` as a macro is not unbalanced, it is a header.
	if s.mode&ScanPP == 0 {
		s.bracket(k, start)
	}
	s.emitFlags(k, start, fl)
}

// ---- the advisory bracket stack: never affects tokenization ----

type bracket struct {
	kind token.Kind
	pos  int
}

func (s *scanner) bracket(k token.Kind, pos int) {
	switch k {
	case token.LPAREN, token.LBRACK, token.LBRACE:
		s.brackets = append(s.brackets, bracket{k, pos})
	case token.RPAREN, token.RBRACK, token.RBRACE:
		if len(s.brackets) == 0 {
			s.bracketErr(pos, fmt.Sprintf("unmatched %s", k))
			return
		}
		top := s.brackets[len(s.brackets)-1]
		s.brackets = s.brackets[:len(s.brackets)-1]
		if closerFor(top.kind) != k {
			// Blame the opener, then go quiet.
			s.bracketErr(top.pos, fmt.Sprintf("unclosed %s, closed by %s", top.kind, k))
		}
	}
}

func (s *scanner) atEOF() {
	if len(s.brackets) > 0 {
		top := s.brackets[len(s.brackets)-1] // the innermost one
		s.bracketErr(top.pos, fmt.Sprintf("unclosed %s at end of file", top.kind))
	}
}

func (s *scanner) bracketErr(pos int, msg string) {
	if s.bracketQuiet {
		return
	}
	s.bracketQuiet = true
	s.report(token.Error, pos, pos+1, msg)
}

func closerFor(k token.Kind) token.Kind {
	switch k {
	case token.LPAREN:
		return token.RPAREN
	case token.LBRACK:
		return token.RBRACK
	}
	return token.RBRACE
}

// ---- emission and diagnostics ----

func (s *scanner) emit(k token.Kind, start int) { s.emitFlags(k, start, 0) }

func (s *scanner) emitFlags(k token.Kind, start int, fl token.Flags) {
	if s.adjacent {
		fl |= token.FlagAdjacent
	}
	if s.nlBefore {
		fl |= token.FlagNLBefore
	}
	s.toks = append(s.toks, token.Token{
		Kind: k, Flags: fl,
		Pos: s.f.Pos(start), End: s.f.Pos(s.off),
	})
	s.adjacent = true
	s.nlBefore = false
}

// errTok reports at most once per token: after the first report the
// current token goes quiet.
// other emits the pp-token §6.4p1 spells "each non-white-space character
// that cannot be one of the above": a backslash outside a UCN, a @, a `.
//
// The grammar has a production for it, so under ScanPP it exists and is
// quiet. What it does not have is a token to be converted into, which is
// §6.4p1's constraint on phase 7 and not on phase 3 — so the diagnostic
// fires when the character survives macro expansion and is scanned again
// without ScanPP, and not where it was written.
//
// The difference is <windows.h>. It reaches DriverSpecs.h, which defines
// __drv_innerAcquiresGlobal with a `param\t` in its replacement list; a
// Win32 program never expands it, MSVC and clang never mention it, and a
// compiler that reports the backslash where it is written cannot open the
// header at all. The same rule covers an @ inside an #if group that is
// excluded, which the preprocessor drops before phase 7 ever sees it.
func (s *scanner) other(start int, msg string) {
	if s.mode&ScanPP == 0 {
		s.errTok(start, s.off, msg)
	}
	s.emit(token.ILLEGAL, start)
}

func (s *scanner) errTok(lo, hi int, msg string) {
	if s.quietTok {
		return
	}
	s.quietTok = true
	s.report(token.Error, lo, hi, msg)
}

// valueErr reports a value-level literal mistake: a suffix, digit,
// exponent, or escape that phase 7's decoding would reject. Under
// ScanPP these defer — a pp-number is not yet a constant, and only
// the tokens that survive phase 4 are anyone's mistake. The #if
// evaluator reports the ones it decodes; the re-scan above phase 4
// reports the ones that reach the parser. Formation errors
// (unterminated literals, stray '\') go through errTok and never
// defer.
func (s *scanner) valueErr(lo, hi int, msg string) {
	if s.mode&ScanPP != 0 {
		return
	}
	s.errTok(lo, hi, msg)
}

// report appends one diagnostic with a non-empty span clamped to the
// source.
func (s *scanner) report(sev token.Severity, lo, hi int, msg string) {
	n := len(s.text)
	if n == 0 {
		return
	}
	if hi <= lo {
		hi = lo + 1
	}
	if hi > n {
		hi = n
	}
	if lo >= hi {
		lo = hi - 1
	}
	s.diags = append(s.diags, token.Diagnostic{
		Pos: s.f.Pos(lo), End: s.f.Pos(hi), Severity: sev, Message: msg,
	})
}

func isDigit(c byte) bool  { return '0' <= c && c <= '9' }
func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
func isBinDigit(c byte) bool { return c == '0' || c == '1' }

func isHexDigit(c byte) bool {
	return isDigit(c) || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}
func isIdentStart(c byte) bool { return c == '_' || isLetter(c) }
func isIdentPart(c byte) bool  { return isIdentStart(c) || isDigit(c) }