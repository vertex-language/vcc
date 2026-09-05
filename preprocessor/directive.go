package preprocessor

import (
	"strings"

	"github.com/vertex-language/vcc/token"
)

// gnuDirectives are the GNU dialect's directives. They are rejected, not
// tolerated: a directive dialect is a language, and languages are out. Where
// ISO has a spelling for the need, the note names it. #warning is absent:
// C23 standardized it (WG14 N2686), so it is a version gap, not a dialect,
// and it is handled in the directive switch.
var gnuDirectives = map[string]string{
	"assert":   "removed from GNU C itself; no ISO equivalent",
	"unassert": "removed from GNU C itself; no ISO equivalent",
	"ident":    "no ISO equivalent",
	"sccs":     "no ISO equivalent",
	"import":   "no ISO equivalent; write an include guard",
}

// directive processes one directive line. The '#' has been consumed; line
// holds the rest of the logical line, without the terminator.
func (p *Preprocessor) directive(r *reader, hash Token, line []Token) {
	at := hash.Site()

	// The null directive. It does not invalidate the multiple-include
	// optimization, matching gcc, clang and MSVC.
	if len(line) == 0 {
		return
	}

	name := line[0]
	rest := line[1:]
	word := ""
	if name.Kind == token.IDENT || name.Kind.IsKeyword() {
		word = name.Text()
	}

	// Inside a skipped group only the conditional directives are recognized;
	// everything else, including syntactically invalid directives, is skipped.
	switch word {
	case "if", "ifdef", "ifndef", "elif", "else", "endif":
	default:
		if r.skipping() {
			return
		}
	}

	// Any directive other than an opening conditional ends the window in which
	// an include guard can start.
	switch word {
	case "if", "ifdef", "ifndef":
	default:
		r.miValid = false
	}

	switch word {
	case "define":
		p.doDefine(r, rest, at)
	case "undef":
		p.doUndef(rest, at)
	case "include":
		p.doInclude(r, rest, at)
	case "include_next":
		p.doIncludeNext(r, rest, at)
	case "if":
		r.beginIf(p, "#if", at, func() bool { return p.Eval(rest, at) })
		r.noteGuardIf(p, rest)
	case "ifdef", "ifndef":
		p.doIfdef(r, word == "ifndef", rest, at)
	case "elif":
		r.doElif(p, at, func() bool { return p.Eval(rest, at) })
	case "else":
		r.doElse(p, at)
		p.expectEnd(rest, "#else")
		if c := r.topCond(); c != nil {
			c.guard = "" // an #else means the file is not simply guarded
		}
	case "endif":
		r.doEndif(p, at)
		p.expectEnd(rest, "#endif")
	case "line":
		p.doLine(rest, at)
	case "error":
		p.errorf(at, "#error %s", spell(rest))
	case "warning":
		// C23's #warning (N2686): diagnose and continue. vcc targets
		// C11/C17, where it is not yet ISO — so it is itself reported,
		// as a warning, which is also exactly the severity the
		// directive asks for. Named, so the once-per-header rule
		// applies in system headers: cdefs.h fires this on every
		// translation unit from its unknown-compiler branch. The note
		// rides only when the warning was emitted — note() binds to
		// the last diagnostic, whatever that is.
		if p.warn("warning-directive", at, "#warning %s", spell(rest)) {
			p.note(at, "#warning is C23; vcc preprocesses C17")
		}
	case "pragma":
		p.doPragma(r, rest, at)
	default:
		if why, ok := gnuDirectives[word]; ok {
			p.errorf(at, "#%s is a GNU directive; vcc preprocesses ISO C", word)
			p.note(at, why)
			return
		}
		p.errorf(name.Site(), "invalid preprocessing directive #%s", name.Text())
	}
}

// doDefine parses §6.10.3's two forms. The distinction between object-like and
// function-like is whether '(' is *adjacent* to the name: `#define M (x)`
// defines M as `(x)`, not as a macro of one parameter.
func (p *Preprocessor) doDefine(r *reader, line []Token, at Site) {
	if len(line) == 0 {
		p.errorf(at, "#define with no macro name")
		return
	}
	name := line[0]
	if name.Kind != token.IDENT && !name.Kind.IsKeyword() {
		p.errorf(name.Site(), "macro name must be an identifier")
		return
	}
	if Reserved(name.Text()) {
		p.errorf(name.Site(), "%q may not be redefined", name.Text())
		return
	}

	m := &Macro{Name: name.Text(), ObjLike: true, Def: name.Site()}
	body := line[1:]

	if len(body) > 0 && body[0].Kind == token.LPAREN && !body[0].Spaced() {
		var ok bool
		m.ObjLike = false
		m.Params, m.Variadic, body, ok = p.params(body[1:], at)
		if !ok {
			return
		}
	}
	m.Body = append([]Token(nil), body...)
	if !p.checkBody(m, at) {
		return
	}

	if prev := p.macros.Lookup(m.Name); prev != nil && !SameDefinition(prev, m) {
		p.warn("macro-redefined", name.Site(), "%q redefined", m.Name)
		if prev.Def.Valid() {
			p.note(prev.Def, "previous definition is here")
		}
	}
	p.macros.Define(m)
}

func (p *Preprocessor) params(line []Token, at Site) (names []string, variadic bool, body []Token, ok bool) {
	seen := map[string]bool{}
	for i := 0; i < len(line); i++ {
		t := line[i]
		switch {
		case t.Kind == token.RPAREN:
			return names, variadic, line[i+1:], true
		case t.Kind == token.ELLIPSIS:
			variadic = true
			if i+1 >= len(line) || line[i+1].Kind != token.RPAREN {
				p.errorf(t.Site(), "expected ')' after '...'")
				return nil, false, nil, false
			}
			return names, true, line[i+2:], true
		case t.Kind == token.IDENT || t.Kind.IsKeyword():
			n := t.Text()
			if n == "__VA_ARGS__" {
				p.errorf(t.Site(), "__VA_ARGS__ is not a valid parameter name")
				return nil, false, nil, false
			}
			if seen[n] {
				p.errorf(t.Site(), "duplicate macro parameter %q", n)
				return nil, false, nil, false
			}
			seen[n] = true
			names = append(names, n)
			if i+1 < len(line) && line[i+1].Kind == token.COMMA {
				i++
			}
		default:
			p.errorf(t.Site(), "expected a parameter name, found %q", t.Text())
			return nil, false, nil, false
		}
	}
	p.errorf(at, "missing ')' in macro parameter list")
	return nil, false, nil, false
}

// checkBody enforces the constraints §6.10.3.2p1 and §6.10.3.3p1 put on a
// replacement list, and rejects the GNU comma-swallow.
func (p *Preprocessor) checkBody(m *Macro, at Site) bool {
	b := m.Body
	if len(b) > 0 && b[0].Kind == token.HASHHASH {
		p.errorf(b[0].Site(), "'##' may not appear at the start of a replacement list")
		return false
	}
	if len(b) > 0 && b[len(b)-1].Kind == token.HASHHASH {
		p.errorf(b[len(b)-1].Site(), "'##' may not appear at the end of a replacement list")
		return false
	}
	for i, t := range b {
		if t.Kind == token.HASH && !m.ObjLike {
			if i+1 >= len(b) || m.Param(b[i+1].Text()) < 0 {
				p.errorf(t.Site(), "'#' is not followed by a macro parameter")
				return false
			}
		}
		if t.Is("__VA_ARGS__") && !m.Variadic {
			p.errorf(t.Site(), "__VA_ARGS__ may only appear in a variadic macro")
			return false
		}
	}
	return true
}

func (p *Preprocessor) doUndef(line []Token, at Site) {
	if len(line) == 0 {
		p.errorf(at, "#undef with no macro name")
		return
	}
	name := line[0]
	if name.Kind != token.IDENT && !name.Kind.IsKeyword() {
		p.errorf(name.Site(), "macro name must be an identifier")
		return
	}
	if Reserved(name.Text()) {
		p.errorf(name.Site(), "%q may not be undefined", name.Text())
		return
	}
	p.macros.Undef(name.Text())
	p.expectEnd(line[1:], "#undef")
}

func (p *Preprocessor) doIfdef(r *reader, negate bool, line []Token, at Site) {
	word := "#ifdef"
	if negate {
		word = "#ifndef"
	}
	if r.skipping() {
		r.pushCond(cond{site: at, directive: word})
		return
	}
	if len(line) == 0 || (line[0].Kind != token.IDENT && !line[0].Kind.IsKeyword()) {
		p.errorf(at, "%s with no macro name", word)
		r.pushCond(cond{site: at, directive: word})
		return
	}
	name := line[0].Text()
	v := p.macros.Defined(name)
	if negate {
		v = !v
	}
	p.expectEnd(line[1:], word)
	c := cond{site: at, directive: word, taken: v, active: v}
	// #ifndef GUARD at the top of a file, with nothing before it, is the
	// include-guard idiom.
	if negate && r.miValid && len(r.conds) == 0 && !r.sawToken {
		c.guard = name
	}
	r.pushCond(c)
}

// noteGuardIf recognizes the other guard spelling: #if !defined FOO.
func (r *reader) noteGuardIf(p *Preprocessor, line []Token) {
	c := r.topCond()
	if c == nil || !r.miValid || len(r.conds) != 1 || r.sawToken {
		return
	}
	if len(line) >= 2 && line[0].Kind == token.NOT && line[1].Is("defined") {
		for _, t := range line[2:] {
			if t.Kind == token.IDENT {
				c.guard = t.Text()
				return
			}
		}
	}
}

// doLine implements §6.10.4. It changes what __LINE__ and __FILE__ report; it
// does not move any span, because a diagnostic must underline what the user
// actually typed.
func (p *Preprocessor) doLine(line []Token, at Site) {
	line = p.expandClosed(line)
	if len(line) == 0 || line[0].Kind != token.INT_LIT {
		p.errorf(at, "#line requires a digit sequence")
		return
	}
	n := 0
	for _, c := range line[0].Text() {
		if c < '0' || c > '9' {
			p.errorf(line[0].Site(), "#line requires a digit sequence")
			return
		}
		n = n*10 + int(c-'0')
	}
	p.lineDelta = n - p.physicalLine(at)
	if len(line) > 1 {
		if line[1].Kind != token.STRING_LIT {
			p.errorf(line[1].Site(), "#line filename must be a string literal")
			return
		}
		p.fileName = strings.Trim(line[1].Text(), `"`)
	}
}

// doPragma honours what ISO defines and passes the rest through. STDC pragmas
// are the standard's; an unrecognized pragma is not an error, per §6.10.6.
//
// `#pragma once` is honoured. vcc infers the same thing from the #ifndef
// idiom, and for a long while that inference was the whole answer — but it
// is only an answer for a header written in that idiom, and the Windows SDK
// is not: <corecrt.h> and most of what it reaches carry the pragma and no
// guard, so a compiler that merely notes the pragma reads them once per
// #include and reports every struct in them as redefined. The pragma is not
// ISO, so it is still named outside a system header, where the person
// reading the diagnostic is the person who can act on it.
func (p *Preprocessor) doPragma(r *reader, line []Token, at Site) {
	if len(line) > 0 && line[0].Is("once") {
		r.once = true
		if at.Origin == nil || !at.Origin.System {
			if p.warn("pragma-once", at, "#pragma once is not ISO C") {
				p.note(at, "vcc honours it; the portable spelling is an #ifndef include guard, which vcc detects on its own")
			}
		}
		return
	}
	if len(line) > 0 && line[0].Is("GCC") {
		if p.warn("gnu-pragma", at, "#pragma GCC is a GNU extension and has no effect") {
			p.note(at, "warning state is a build decision; use --warn on the command line")
		}
		return
	}

	// Everything else survives into the output for phase 7 to see.
	//
	// The directive walk consumed both the '#' and the word 'pragma'
	// before dispatching here, so both are re-minted — or the output
	// is not a pragma line at all: a bare '#' followed by operands
	// glues onto the preceding printed line and re-enters as garbage
	// in declaration position (the sys/socket.h `#pragma pack` bug).
	// The minted '#' opens a logical line, exactly as it did in the
	// source; the operand tokens keep their file spans and flags.
	hash := p.gen.Mint(token.HASH, "#")
	hash.Flags = token.FlagNLBefore
	word := p.gen.Mint(token.IDENT, "pragma")
	word.Flags = token.FlagAdjacent
	p.out = append(p.out, hash, word)
	p.out = append(p.out, line...)
}

func (p *Preprocessor) expectEnd(rest []Token, what string) {
	if len(rest) > 0 {
		p.warn("extra-tokens", rest[0].Site(), "extra tokens at end of %s directive", what)
	}
}

func spell(ts []Token) string {
	var b strings.Builder
	for i, t := range ts {
		if i > 0 && t.Spaced() {
			b.WriteByte(' ')
		}
		b.WriteString(t.Text())
	}
	return b.String()
}
