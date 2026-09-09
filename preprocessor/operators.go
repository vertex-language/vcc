package preprocessor

import "github.com/vertex-language/vcc/token"

// The operators the preprocessor answers for itself.
//
// Six of them, in two shapes. __has_include and __has_include_next take a
// header name; __has_builtin and the three __is_target_* take a single word.
// All six are operators rather than macros, and all six are resolved in the
// pass `defined` already had, before expansion -- an operand that is not an
// expression must not be expanded, and none of these operands is one.
//
// # __has_include
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

// # __is_target_arch and its neighbours
//
// A header that wants to know what it is being compiled for has the same
// problem __has_include solves for files. TargetConditionals.h on every Mac
// asks all four, gated on __has_builtin, and the answer decides whether
// TARGET_OS_MACCATALYST or TARGET_OS_SIMULATOR is set -- facts no predefined
// macro carries because they are about the triple rather than the platform.
//
// The operand is one word and is not expanded: `__is_target_arch(arm64)`
// names an architecture, not a macro, and a program that happened to define
// `arm64` would otherwise change what it is being compiled for.

const (
	hasInclude     = "__has_include"
	hasIncludeNext = "__has_include_next"
	hasBuiltin     = "__has_builtin"
	isTargetArch   = "__is_target_arch"
	isTargetVendor = "__is_target_vendor"
	isTargetOS     = "__is_target_os"
	isTargetEnv    = "__is_target_environment"
)

// builtinPPMacro reports whether a name is one of the operators the
// preprocessor answers for itself.
//
// `defined(__has_include)` and `#ifdef __has_include` are how a portable
// header asks whether it may use the operator, so both have to say yes --
// and neither may be answered by defining a macro of that name, because a
// macro would be expanded and the operand would be expanded with it.
func builtinPPMacro(name string) bool {
	switch name {
	case hasInclude, hasIncludeNext, hasBuiltin,
		isTargetArch, isTargetVendor, isTargetOS, isTargetEnv:
		return true
	}
	return false
}

// resolveOperators replaces every one of these operators in a controlling
// expression with 1 or 0.
//
// Run after resolveDefined and before expansion: `defined(__has_include)` is
// a question about the operator and is already a number by the time this
// looks, and what is left is each operator applied to its operand.
func (p *Preprocessor) resolveOperators(r *reader, line []Token, at Site) []Token {
	out := make([]Token, 0, len(line))
	for i := 0; i < len(line); i++ {
		t := line[i]
		what := t.Text()
		if !builtinPPMacro(what) {
			out = append(out, t)
			continue
		}

		if i+1 >= len(line) || line[i+1].Kind != token.LPAREN {
			p.errorf(t.Site(), "operator \"%s\" requires %s in parentheses",
				what, operandKind(what))
			out = append(out, p.number(t, 0))
			continue
		}
		close, ok := matchParen(line, i+1)
		if !ok {
			p.errorf(t.Site(), "missing ')' after \"%s\"", what)
			out = append(out, p.number(t, 0))
			return append(out, line[i+1:]...)
		}

		// The operand alone, so the reader of it sees a whole line and its
		// end-of-line check is about the parentheses rather than the rest of
		// the expression.
		operand := line[i+2 : close]
		out = append(out, p.number(t, p.operator(r, what, operand, t.Site())))
		i = close
	}
	return out
}

// operator answers one of them, as 1 or 0.
func (p *Preprocessor) operator(r *reader, what string, operand []Token, at Site) int {
	switch what {
	case hasInclude, hasIncludeNext:
		name, angled, found := p.headerName(what, operand, at)
		if found && p.headerExists(r, name, angled, what == hasIncludeNext) {
			return 1
		}
		return 0
	}

	word, ok := p.operandWord(what, operand, at)
	if !ok {
		return 0
	}
	switch what {
	case hasBuiltin:
		return p.builtinAnswer(word)
	case isTargetArch:
		return matchTriple(archAliases, p.cfg.Triple.Arch, word)
	case isTargetVendor:
		return matchTriple(nil, p.cfg.Triple.Vendor, word)
	case isTargetOS:
		return matchTriple(osAliases, p.cfg.Triple.OS, word)
	case isTargetEnv:
		return matchTriple(nil, p.cfg.Triple.Environment, word)
	}
	return 0
}

// operandWord is the single word these operators take, and says so where
// there is not exactly one.
//
// It is deliberately not macro-expanded. `__is_target_arch(arm64)` names an
// architecture and `__has_builtin(__builtin_expect)` names a builtin;
// neither is a macro, and a program that defined one would otherwise be
// asking a different question than it wrote.
func (p *Preprocessor) operandWord(what string, operand []Token, at Site) (string, bool) {
	if len(operand) == 1 && (operand[0].Kind == token.IDENT || operand[0].Kind.IsKeyword()) {
		return operand[0].Text(), true
	}
	// An architecture may be spelled with a digit in it -- i386, x86_64 --
	// and phase 3 does not always hand that over as one identifier. What
	// matters is that the whole operand is one run of characters, which the
	// source says better than the tokens do.
	if len(operand) > 0 {
		if org := operand[0].Origin; org != nil && org.File != nil {
			last := operand[len(operand)-1]
			if text := string(org.File.Slice(operand[0].Pos, last.End)); text != "" &&
				!hasSpace(text) {
				return text, true
			}
		}
	}
	p.errorf(at, "operator \"%s\" requires %s", what, operandKind(what))
	return "", false
}

// builtinAnswer is what __has_builtin says about a name.
//
// The three __is_target_* operators and their vendor sibling are builtins
// and answer for themselves, which is exactly the set clang reports: a
// header gates on `__has_builtin(__is_target_arch)` before using them, and
// that gate has to open. The rest is the caller's to answer -- see
// Config.Builtin.
//
// __has_include, __has_builtin and their kin are deliberately *not*
// builtins here, because they are not builtins to clang either. They are
// asked about with `defined`, and a compiler that answered yes to both
// questions would be telling a header something no other compiler tells
// it.
func (p *Preprocessor) builtinAnswer(word string) int {
	switch word {
	case isTargetArch, isTargetVendor, isTargetOS, isTargetEnv:
		return 1
	}
	if p.cfg.Builtin != nil && p.cfg.Builtin(word) {
		return 1
	}
	return 0
}

// operandKind names what an operator takes, for the diagnostic.
func operandKind(what string) string {
	switch what {
	case hasInclude, hasIncludeNext:
		return "a header name"
	case hasBuiltin:
		return "the name of a builtin"
	}
	return "the name of a target component"
}

func hasSpace(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			return true
		}
	}
	return false
}

// The spellings that name one thing. A triple component has more than one
// name in circulation and a header may use either, so the comparison is
// over what each name means rather than over the letters.
var (
	archAliases = map[string]string{
		"arm64":   "aarch64",
		"aarch64": "aarch64",
		"amd64":   "x86_64",
		"x86_64":  "x86_64",
		"i486":    "i386",
		"i586":    "i386",
		"i686":    "i386",
		"i386":    "i386",
	}
	osAliases = map[string]string{
		"macos":  "macos",
		"macosx": "macos",
		"osx":    "macos",
		"darwin": "macos",
	}
)

// matchTriple compares a target component with the name a program asked
// about, through an alias table where the component has one.
//
// An empty component matches nothing. That is the honest answer rather than
// a convenient one: a target with no environment is not in the macabi
// environment, and a caller that supplied no triple at all has said nothing
// about any of them.
func matchTriple(aliases map[string]string, have, want string) int {
	if have == "" || want == "" {
		return 0
	}
	h, w := lower(have), lower(want)
	if aliases != nil {
		if a, ok := aliases[h]; ok {
			h = a
		}
		if a, ok := aliases[w]; ok {
			w = a
		}
	}
	if h == w {
		return 1
	}
	return 0
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// resolveExpandedOperators handles an operator that macro expansion
// produced, which is how the C library actually calls one:
//
//	#define __supports_builtin(builtin, gcc_major, gcc_minor) __has_builtin(builtin)
//	#if __supports_builtin(__builtin___memcpy_chk, 0, 0)
//
// in Darwin's <secure/_string.h>. Nothing there is an operator until the
// macro has been expanded, so a pass that only ran before expansion would
// leave `__has_builtin` standing as an identifier and the parenthesis after
// it would be a syntax error.
//
// Unlike `defined`, there is no warning. A `defined` from expansion is
// undefined behavior the standard declined to give a meaning; these
// operators are not in the standard at all, their meaning is clang's, and
// clang evaluates them here -- which is what the header above is written
// against.
//
// A header name cannot generally survive expansion, because the characters
// between the angle brackets were never tokens. One that came from a macro
// is reported rather than guessed at.
func (p *Preprocessor) resolveExpandedOperators(r *reader, line []Token, at Site) []Token {
	for _, t := range line {
		if builtinPPMacro(t.Text()) {
			return p.resolveOperators(r, line, at)
		}
	}
	return line
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
