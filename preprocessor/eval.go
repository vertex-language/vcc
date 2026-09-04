package preprocessor

import (
	"strings"

	"github.com/vertex-language/vcc/token"
)

// §6.10.1 is a different language from §6.6, and reusing the analyzer's
// constant folder would be the wrong semantics, not a shortcut: there is no
// sizeof, no enum constant and no cast; every identifier that survives
// expansion is 0; `defined` is an operator; and arithmetic is in
// intmax_t/uintmax_t regardless of what int is on the target.
type value struct {
	u        uint64
	unsigned bool
}

func (v value) i() int64      { return int64(v.u) }
func (v value) nonzero() bool { return v.u != 0 }

// usual applies §6.3.1.8 in the two types this evaluator has.
func usual(a, b value) (value, value, bool) {
	un := a.unsigned || b.unsigned
	a.unsigned, b.unsigned = un, un
	return a, b, un
}

type evaluator struct {
	p    *Preprocessor
	toks []Token
	i    int
	site Site
	bad  bool
}

// Eval evaluates a #if or #elif controlling expression.
//
// Order matters and is the standard's: `defined` is resolved first, over the
// unexpanded line, because `#if defined FOO` must not expand FOO. What is
// left is macro-expanded. A `defined` *produced* by that expansion is
// undefined behavior (§6.10.1p4) — which is license to define it: gcc and
// clang both evaluate it as the operator, real system headers lean on that
// (Darwin's pthread.h tests a function-like macro that expands to a
// defined() chain), and vcc follows, under a named warning so the
// nonportability stays visible — once per system header, like any other.
// Only then does every remaining identifier become 0.
func (p *Preprocessor) Eval(line []Token, at Site) bool {
	if len(line) == 0 {
		p.errorf(at, "#if with no expression")
		return false
	}
	line = p.resolveDefined(line, at)
	line = p.expandClosed(line)
	line = p.resolveExpandedDefined(line, at)
	line = p.zeroIdents(line)

	e := &evaluator{p: p, toks: line, site: at}
	v := e.conditional()
	if !e.bad && e.i < len(e.toks) {
		e.fail(e.toks[e.i].Site(), "unexpected %q in preprocessor expression", e.toks[e.i].Text())
	}
	return !e.bad && v.nonzero()
}

// resolveDefined replaces `defined X` and `defined ( X )` with 1 or 0.
func (p *Preprocessor) resolveDefined(line []Token, at Site) []Token {
	out := make([]Token, 0, len(line))
	for i := 0; i < len(line); i++ {
		t := line[i]
		if !t.Is("defined") {
			out = append(out, t)
			continue
		}
		j := i + 1
		paren := j < len(line) && line[j].Kind == token.LPAREN
		if paren {
			j++
		}
		if j >= len(line) || line[j].Kind != token.IDENT {
			p.errorf(t.Site(), "operator \"defined\" requires an identifier")
			out = append(out, p.number(t, 0))
			i = len(line)
			continue
		}
		name := line[j].Text()
		if paren {
			if j+1 >= len(line) || line[j+1].Kind != token.RPAREN {
				p.errorf(t.Site(), "missing ')' after \"defined\"")
			} else {
				j++
			}
		}
		n := 0
		if p.macros.Defined(name) {
			n = 1
			p.macros.Lookup(name).Used = true
		}
		out = append(out, p.number(t, n))
		i = j
	}
	return out
}

// resolveExpandedDefined handles a `defined` that macro expansion produced.
// One warning per controlling expression, however many operators the
// expansion yielded; the warning is named, so a system header that does
// this on every inclusion (pthread.h does) reports once per translation
// unit. The resolution itself is resolveDefined, unchanged — the operator
// means the same thing wherever it came from.
func (p *Preprocessor) resolveExpandedDefined(line []Token, at Site) []Token {
	for _, t := range line {
		if t.Is("defined") {
			p.warn("expansion-defined", t.Site(),
				"\"defined\" produced by macro expansion is not portable; vcc evaluates it as the operator, matching gcc and clang")
			return p.resolveDefined(line, at)
		}
	}
	return line
}

func (p *Preprocessor) number(at Token, n int) Token {
	t := p.gen.Mint(token.INT_LIT, map[int]string{0: "0", 1: "1"}[n])
	t.Flags = at.Flags & token.FlagAdjacent
	t.Exp = at.Exp
	return t
}

// zeroIdents implements §6.10.1p4: identifiers remaining after expansion are
// replaced by 0. Keywords are identifiers here — `#if sizeof` is 0, not an
// error — which is why this runs on kinds the scanner already classified.
// No `defined` can reach this point: both resolveDefined passes consume
// every spelling, and the malformed-operand path truncates the line.
func (p *Preprocessor) zeroIdents(line []Token) []Token {
	for i, t := range line {
		if t.Kind == token.IDENT || t.Kind.IsKeyword() {
			line[i] = p.number(t, 0)
		}
	}
	return line
}

func (e *evaluator) fail(s Site, f string, a ...any) {
	if !e.bad {
		e.p.errorf(s, f, a...)
		e.bad = true
	}
}

func (e *evaluator) peek() (Token, bool) {
	if e.i >= len(e.toks) {
		return Token{}, false
	}
	return e.toks[e.i], true
}

func (e *evaluator) accept(k token.Kind) bool {
	if t, ok := e.peek(); ok && t.Kind == k {
		e.i++
		return true
	}
	return false
}

func (e *evaluator) conditional() value {
	c := e.binary(1)
	if !e.accept(token.QUESTION) {
		return c
	}
	// Both arms are parsed; only the taken one may report. Division by zero in
	// the untaken arm of `#if 0 ? 1/0 : 1` is not a mistake.
	save := e.bad
	t := e.conditional()
	if !e.accept(token.COLON) {
		e.fail(e.site, "expected ':' in preprocessor conditional")
		return value{}
	}
	f := e.conditional()
	if c.nonzero() {
		return t
	}
	e.bad = save
	return f
}

// prec gives the ten binary levels. Keeping the mapping in one table means a
// rename in token/kind.go is a one-site fix here.
func prec(k token.Kind) int {
	switch k {
	case token.LOR:
		return 1
	case token.LAND:
		return 2
	case token.OR:
		return 3
	case token.XOR:
		return 4
	case token.AND:
		return 5
	case token.EQL, token.NEQ:
		return 6
	case token.LSS, token.GTR, token.LEQ, token.GEQ:
		return 7
	case token.SHL, token.SHR:
		return 8
	case token.ADD, token.SUB:
		return 9
	case token.MUL, token.QUO, token.REM:
		return 10
	}
	return 0
}

func (e *evaluator) binary(min int) value {
	lhs := e.unary()
	for {
		t, ok := e.peek()
		if !ok {
			return lhs
		}
		pr := prec(t.Kind)
		if pr < min {
			return lhs
		}
		e.i++

		// && and || short-circuit even here, so `defined(X) && X > 2` is safe
		// and `0 && 1/0` folds without reporting.
		if t.Kind == token.LAND || t.Kind == token.LOR {
			save := e.bad
			rhs := e.binary(pr + 1)
			skip := (t.Kind == token.LAND && !lhs.nonzero()) ||
				(t.Kind == token.LOR && lhs.nonzero())
			if skip {
				e.bad = save
			}
			n := uint64(0)
			if (t.Kind == token.LAND && lhs.nonzero() && rhs.nonzero()) ||
				(t.Kind == token.LOR && (lhs.nonzero() || rhs.nonzero())) {
				n = 1
			}
			lhs = value{u: n}
			continue
		}

		rhs := e.binary(pr + 1)
		lhs = e.apply(t, lhs, rhs)
	}
}

func (e *evaluator) apply(op Token, a, b value) value {
	boolean := func(t bool) value {
		if t {
			return value{u: 1}
		}
		return value{}
	}
	switch op.Kind {
	case token.SHL, token.SHR:
		// Shifts do not convert: the left operand's type wins.
		n := b.u & 63
		if op.Kind == token.SHL {
			return value{u: a.u << n, unsigned: a.unsigned}
		}
		if a.unsigned {
			return value{u: a.u >> n, unsigned: true}
		}
		return value{u: uint64(a.i() >> n)}
	}

	a, b, un := usual(a, b)
	switch op.Kind {
	case token.ADD:
		return value{u: a.u + b.u, unsigned: un}
	case token.SUB:
		return value{u: a.u - b.u, unsigned: un}
	case token.MUL:
		return value{u: a.u * b.u, unsigned: un}
	case token.QUO, token.REM:
		if b.u == 0 {
			e.fail(op.Site(), "division by zero in preprocessor expression")
			return value{}
		}
		if un {
			if op.Kind == token.QUO {
				return value{u: a.u / b.u, unsigned: true}
			}
			return value{u: a.u % b.u, unsigned: true}
		}
		if op.Kind == token.QUO {
			return value{u: uint64(a.i() / b.i())}
		}
		return value{u: uint64(a.i() % b.i())}
	case token.AND:
		return value{u: a.u & b.u, unsigned: un}
	case token.OR:
		return value{u: a.u | b.u, unsigned: un}
	case token.XOR:
		return value{u: a.u ^ b.u, unsigned: un}
	case token.EQL:
		return boolean(a.u == b.u)
	case token.NEQ:
		return boolean(a.u != b.u)
	case token.LSS:
		if un {
			return boolean(a.u < b.u)
		}
		return boolean(a.i() < b.i())
	case token.GTR:
		if un {
			return boolean(a.u > b.u)
		}
		return boolean(a.i() > b.i())
	case token.LEQ:
		if un {
			return boolean(a.u <= b.u)
		}
		return boolean(a.i() <= b.i())
	case token.GEQ:
		if un {
			return boolean(a.u >= b.u)
		}
		return boolean(a.i() >= b.i())
	}
	e.fail(op.Site(), "%q is not valid in a preprocessor expression", op.Text())
	return value{}
}

func (e *evaluator) unary() value {
	t, ok := e.peek()
	if !ok {
		e.fail(e.site, "expected an expression")
		return value{}
	}
	switch t.Kind {
	case token.ADD:
		e.i++
		return e.unary()
	case token.SUB:
		e.i++
		v := e.unary()
		return value{u: -v.u, unsigned: v.unsigned}
	case token.TILDE:
		e.i++
		v := e.unary()
		return value{u: ^v.u, unsigned: v.unsigned}
	case token.NOT:
		e.i++
		v := e.unary()
		if v.nonzero() {
			return value{}
		}
		return value{u: 1}
	case token.LPAREN:
		e.i++
		v := e.conditional()
		if !e.accept(token.RPAREN) {
			e.fail(t.Site(), "missing ')' in preprocessor expression")
		}
		return v
	case token.INT_LIT:
		e.i++
		return e.intConst(t)
	case token.CHAR_LIT:
		e.i++
		return e.charConst(t)
	case token.FLOAT_LIT:
		e.i++
		e.fail(t.Site(), "floating constant in preprocessor expression")
		return value{}
	case token.STRING_LIT:
		e.i++
		e.fail(t.Site(), "string literal in preprocessor expression")
		return value{}
	}
	e.fail(t.Site(), "unexpected %q in preprocessor expression", t.Text())
	return value{}
}

// intConst decodes a pp-number as an integer constant. This is the second
// decoder in the tree — analyzer has the §6.6 one — and it must stay in step
// with it, because `#if 'x' == 120` and `char c = 'x';` are asking about the
// same source text. The shared core is small enough that the duplication is
// cheaper than the import inversion; if it grows, it moves down into token.
func (e *evaluator) intConst(t Token) value {
	s := t.Text()
	base := 10
	switch {
	case len(s) > 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X'):
		base, s = 16, s[2:]
	case len(s) > 1 && s[0] == '0':
		base, s = 8, s[1:]
	}
	unsigned := false

	// MSVC sized suffix: [u|U]i{8,16,32,64}. Strip it before the u/l
	// loop so the digit scanner sees only digits.
	s, unsigned = stripMSVCSuffix(s, unsigned)

	for len(s) > 0 {
		c := s[len(s)-1]
		if c == 'u' || c == 'U' {
			unsigned = true
		} else if c != 'l' && c != 'L' {
			break
		}
		s = s[:len(s)-1]
	}
	var v uint64
	over := false
	for i := 0; i < len(s); i++ {
		d := digit(s[i])
		if d < 0 || d >= base {
			e.fail(t.Site(), "invalid digit %q in constant", string(s[i]))
			return value{}
		}
		n := v*uint64(base) + uint64(d)
		if n < v {
			over = true
		}
		v = n
	}
	if over {
		e.fail(t.Site(), "integer constant is too large for intmax_t")
	}
	// A decimal constant too large for intmax_t is unsigned; hex and octal
	// reach unsigned candidates earlier. Both land in the same place here.
	if v > 1<<63-1 {
		unsigned = true
	}
	return value{u: v, unsigned: unsigned}
}

// stripMSVCSuffix strips an MSVC sized integer suffix from the end of s
// and updates the unsigned flag. Returns the stripped string and the
// updated unsigned flag.
func stripMSVCSuffix(s string, unsigned bool) (string, bool) {
	for _, suf := range []string{"i64", "I64", "i32", "I32", "i16", "I16", "i8", "I8"} {
		if len(s) > len(suf) && s[len(s)-len(suf):] == suf {
			pos := len(s) - len(suf)
			if pos > 0 && (s[pos-1] == 'u' || s[pos-1] == 'U') {
				return s[:pos-1], true
			}
			return s[:pos], unsigned
		}
	}
	return s, unsigned
}

func digit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// charConst decodes a character constant. Multi-character constants use the
// same implementation-defined left-to-right accumulation the analyzer uses.
func (e *evaluator) charConst(t Token) value {
	s := t.Text()
	wide := false
	for len(s) > 0 && s[0] != '\'' {
		wide = true
		s = s[1:]
	}
	s = strings.TrimPrefix(s, "'")
	s = strings.TrimSuffix(s, "'")

	var acc int64
	n := 0
	for len(s) > 0 {
		var c int64
		c, s = decodeChar(s)
		acc = acc<<8 | (c & 0xff)
		if n == 0 && (wide || len(s) == 0) {
			acc = c
		}
		n++
	}
	// A plain single-character constant has type int and, on targets where
	// char is signed, a negative value for the high half.
	if n == 1 && !wide && acc > 127 {
		acc = int64(int8(acc))
	}
	return value{u: uint64(acc)}
}

func decodeChar(s string) (int64, string) {
	if s[0] != '\\' {
		return int64(s[0]), s[1:]
	}
	s = s[1:]
	if s == "" {
		return '\\', ""
	}
	switch s[0] {
	case 'n':
		return '\n', s[1:]
	case 't':
		return '\t', s[1:]
	case 'r':
		return '\r', s[1:]
	case '0', '1', '2', '3', '4', '5', '6', '7':
		v, i := int64(0), 0
		for i < 3 && i < len(s) && s[i] >= '0' && s[i] <= '7' {
			v = v*8 + int64(s[i]-'0')
			i++
		}
		return v, s[i:]
	case 'x':
		v, i := int64(0), 1
		for i < len(s) && digit(s[i]) >= 0 && digit(s[i]) < 16 {
			v = v*16 + int64(digit(s[i]))
			i++
		}
		return v, s[i:]
	case 'a':
		return 7, s[1:]
	case 'b':
		return 8, s[1:]
	case 'f':
		return 12, s[1:]
	case 'v':
		return 11, s[1:]
	}
	return int64(s[0]), s[1:]
}