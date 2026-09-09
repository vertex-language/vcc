package preprocessor

import "github.com/vertex-language/vcc/token"

// __has_include, and the operators of its shape.
//
// A header that wants a header it may not have has no other way to ask.
// Before this existed the question was answered by the build system --
// configure ran a compile and wrote a macro -- and the SDK on every Mac
// stopped doing that: Availability.h asks
//
//	#if __has_include(<AvailabilityInternalPrivate.h>)
//
// on line 199, so a compiler without the operator cannot read <stdlib.h>,
// and therefore cannot read anything.
//
// It is an operator rather than a macro, and for the same reason `defined`
// is one: its operand is not an expression. `__has_include(<sys/types.h>)`
// has a header-name inside it, which phase 3 does not produce and macro
// expansion must not touch -- expanding it would rewrite `sys`, and the
// slash and the dot were never tokens at all. So it is resolved before
// expansion, over the line as written, exactly where `defined` is.
//
// The answer is whether an #include written here would find a file. Not
// whether one exists somewhere: the search list and the including file's own
// directory are what decide, which is why this asks the same searchList the
// directive asks. An operator that answered a different question from the
// directive it guards would be worse than no operator.

const (
	hasInclude     = "__has_include"
	hasIncludeNext = "__has_include_next"
)

// builtinPPMacro reports whether a name is one of the operators the
// preprocessor answers for itself.
//
// `defined(__has_include)` and `#ifdef __has_include` are how a portable
// header asks whether it may use the operator, so both have to say yes --
// and neither may be answered by defining a macro of that name, because a
// macro would be expanded and the operand would be expanded with it.
func builtinPPMacro(name string) bool {
	return name == hasInclude || name == hasIncludeNext
}

// resolveHasInclude replaces every `__has_include(...)` in a controlling
// expression with 1 or 0.
//
// Run after resolveDefined and before expansion: `defined(__has_include)` is
// a question about the operator and is already a number by the time this
// looks, and what is left is the operator applied to a header-name.
func (p *Preprocessor) resolveHasInclude(r *reader, line []Token, at Site) []Token {
	out := make([]Token, 0, len(line))
	for i := 0; i < len(line); i++ {
		t := line[i]
		if !t.Is(hasInclude) && !t.Is(hasIncludeNext) {
			out = append(out, t)
			continue
		}
		next := t.Is(hasIncludeNext)
		what := hasInclude
		if next {
			what = hasIncludeNext
		}

		if i+1 >= len(line) || line[i+1].Kind != token.LPAREN {
			p.errorf(t.Site(), "operator \"%s\" requires a header name in parentheses", what)
			out = append(out, p.number(t, 0))
			continue
		}
		close, ok := matchParen(line, i+1)
		if !ok {
			p.errorf(t.Site(), "missing ')' after \"%s\"", what)
			out = append(out, p.number(t, 0))
			return append(out, line[i+1:]...)
		}

		// The operand alone, so headerName sees a whole line and its
		// end-of-line check is about the parentheses rather than the rest of
		// the expression.
		name, angled, found := p.headerName(what, line[i+1+1:close], t.Site())
		n := 0
		if found && p.headerExists(r, name, angled, next) {
			n = 1
		}
		out = append(out, p.number(t, n))
		i = close
	}
	return out
}

// matchParen is the index of the ')' closing the '(' at open, and reports
// whether there was one. Nesting is counted because a header name has no
// parentheses but a macro that expands to one may.
func matchParen(line []Token, open int) (int, bool) {
	depth := 0
	for i := open; i < len(line); i++ {
		switch line[i].Kind {
		case token.LPAREN:
			depth++
		case token.RPAREN:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}
