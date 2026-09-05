package preprocessor

import "github.com/vertex-language/vcc/token"

// HideSet is Prosser's blue paint: the set of macros that must not be
// expanded in a token, carried by the token itself rather than by a stack.
// Sets are immutable and shared; they are almost always empty or tiny.
type HideSet struct{ ids []int }

func (h *HideSet) Has(id int) bool {
	if h == nil {
		return false
	}
	for _, x := range h.ids {
		if x == id {
			return true
		}
		if x > id {
			break
		}
	}
	return false
}

func (h *HideSet) Add(id int) *HideSet {
	if h.Has(id) {
		return h
	}
	n := len(h.idsOf())
	out := make([]int, 0, n+1)
	inserted := false
	for _, x := range h.idsOf() {
		if !inserted && x > id {
			out, inserted = append(out, id), true
		}
		out = append(out, x)
	}
	if !inserted {
		out = append(out, id)
	}
	return &HideSet{ids: out}
}

// idsOf is a nil-safe accessor so Add and the set operations need no nil checks.
func (h *HideSet) idsOf() []int {
	if h == nil {
		return nil
	}
	return h.ids
}

func hsUnion(a, b *HideSet) *HideSet {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := a
	for _, x := range b.ids {
		out = out.Add(x)
	}
	return out
}

// hsInter is the operation the standard's prose loses. For a function-like
// macro the new hide set is (HS ∩ HS') ∪ {T}, where HS' belongs to the
// *closing parenthesis* — not the macro name. Anything else gets DR 017's
// NIL and a(a) cases wrong.
func hsInter(a, b *HideSet) *HideSet {
	if a == nil || b == nil {
		return nil
	}
	var out []int
	for _, x := range a.ids {
		if b.Has(x) {
			out = append(out, x)
		}
	}
	if out == nil {
		return nil
	}
	return &HideSet{ids: out}
}

// hsadd unions hs into every token's hide set.
func hsadd(hs *HideSet, ts []Token) []Token {
	if hs == nil {
		return ts
	}
	for i := range ts {
		ts[i].Hide = hsUnion(hs, ts[i].Hide)
	}
	return ts
}

// stream is a token sequence with pushback. more supplies tokens from a file
// and is nil for a closed sequence (a macro argument), which is what makes
// "pre-expansion of an argument cannot take tokens from after the invocation"
// fall out of the structure instead of needing a rule.
type stream struct {
	buf  []Token
	i    int
	more func() (Token, bool)
}

func (s *stream) fill(n int) bool {
	for len(s.buf)-s.i <= n {
		if s.more == nil {
			return false
		}
		t, ok := s.more()
		if !ok {
			s.more = nil
			return false
		}
		s.buf = append(s.buf, t)
	}
	return true
}

func (s *stream) peek(n int) (Token, bool) {
	if !s.fill(n) {
		return Token{}, false
	}
	return s.buf[s.i+n], true
}

func (s *stream) next() (Token, bool) {
	t, ok := s.peek(0)
	if ok {
		s.i++
	}
	return t, ok
}

func (s *stream) push(ts []Token) {
	if len(ts) == 0 {
		return
	}
	rest := s.buf[s.i:]
	s.buf = append(append(make([]Token, 0, len(ts)+len(rest)), ts...), rest...)
	s.i = 0
}

// expandInto is Prosser's expand(), driven as a loop rather than as recursion
// on a whole sequence, so the top-level stream can keep reading from a file.
func (p *Preprocessor) expandInto(s *stream, out []Token) []Token {
	for {
		t, ok := s.peek(0)
		if !ok {
			return out
		}
		if t.Kind == token.IDENT && p.expandOne(s) {
			continue
		}
		s.next()
		out = append(out, t)
	}
}

// expandClosed expands a finite sequence: macro arguments, and #if lines.
func (p *Preprocessor) expandClosed(ts []Token) []Token {
	if len(ts) == 0 {
		return nil
	}
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > p.cfg.MaxExpansionDepth {
		p.errorf(ts[0].Site(), "macro expansion nested too deeply")
		return ts
	}
	s := &stream{buf: append([]Token(nil), ts...)}
	return p.expandInto(s, nil)
}

// expandOne handles the head of s if it invokes a macro, and reports whether
// it did. The order of tests matters: the hide-set check comes before the
// search for '(', because finding the paren can pop a context and re-enable
// the very macro we are inside.
func (p *Preprocessor) expandOne(s *stream) bool {
	t, _ := s.peek(0)
	m := p.macros.Lookup(t.Text())
	if m == nil || t.Hide.Has(m.ID) {
		return false
	}
	m.Used = true

	if m.Builtin != NotBuiltin {
		s.next()
		p.pushRep(s, p.builtin(m, t), t)
		return true
	}

	if m.ObjLike {
		s.next()
		use := t.Site()
		rep := p.subst(m, m.Body, nil, t.Hide.Add(m.ID), use, t.Exp)
		p.pushRep(s, rep, t)
		return true
	}

	// A function-like macro expands only when the next token is '('.
	lp, ok := s.peek(1)
	if !ok || lp.Kind != token.LPAREN {
		return false
	}
	s.next() // name
	s.next() // '('
	args, rp, ok := p.arguments(s, m, t)
	if !ok {
		return false
	}
	use := t.Site()
	hs := hsInter(t.Hide, rp.Hide).Add(m.ID)
	rep := p.subst(m, m.Body, args, hs, use, t.Exp)
	p.pushRep(s, rep, t)
	return true
}

// pushRep pushes a replacement list back for rescanning. The first token of an
// expansion inherits the invocation's spacing, which is what keeps
// `x=FOO` from printing as `x= bar` and `x = FOO` from printing as `x =bar`.
func (p *Preprocessor) pushRep(s *stream, rep []Token, at Token) {
	if len(rep) > 0 {
		rep[0].Flags &^= token.FlagAdjacent
		rep[0].Flags |= at.Flags & token.FlagAdjacent
		rep[0].Flags |= at.Flags & token.FlagNLBefore
	} else if at.Flags.Has(token.FlagNLBefore) {
		if _, ok := s.peek(0); ok {
			s.buf[s.i].Flags |= token.FlagNLBefore
		}
	}
	s.push(rep)
}

// arguments collects a function-like macro's actual arguments, splitting on
// commas that are not inside parentheses — Prosser's select(), done once.
//
// A variadic macro's trailing slot swallows the remaining commas. An
// invocation that reaches the end of the file, or a line-opening '#', is
// unterminated: the reader refuses to cross a directive, so a macro
// invocation can never straddle one.
func (p *Preprocessor) arguments(s *stream, m *Macro, name Token) ([][]Token, Token, bool) {
	args := make([][]Token, 0, m.Arity())
	cur := []Token{}
	depth := 0
	slot := 0
	for {
		t, ok := s.next()
		if !ok {
			p.errorf(name.Site(), "unterminated argument list for macro %q", m.Name)
			return nil, Token{}, false
		}
		switch {
		case t.Kind == token.LPAREN:
			depth++
		case t.Kind == token.RPAREN:
			if depth == 0 {
				args = append(args, cur)
				return p.checkArity(args, m, name, t)
			}
			depth--
		case t.Kind == token.COMMA && depth == 0:
			// Once we are in the variadic slot, commas are ordinary tokens.
			if !(m.Variadic && slot >= len(m.Params)) {
				args = append(args, cur)
				cur = []Token{}
				slot++
				continue
			}
		}
		cur = append(cur, t)
	}
}

func (p *Preprocessor) checkArity(args [][]Token, m *Macro, name, rp Token) ([][]Token, Token, bool) {
	// `M()` with M taking one parameter passes one empty argument, not zero.
	if len(args) == 1 && len(args[0]) == 0 && m.Arity() == 0 {
		args = nil
	}
	switch {
	case len(args) < m.Arity() && m.Variadic && len(args) == m.Arity()-1:
		// A variadic macro may be called with nothing in the variadic slot.
		args = append(args, nil)
	case len(args) < m.Arity():
		p.errorf(name.Site(), "macro %q requires %d arguments, but %d given",
			m.Name, m.Arity(), len(args))
		return nil, rp, false
	case len(args) > m.Arity():
		p.errorf(name.Site(), "macro %q passed %d arguments, but takes %d",
			m.Name, len(args), m.Arity())
		return nil, rp, false
	}
	return args, rp, true
}

// subst is Prosser's subst(), case for case: stringize, paste-with-parameter
// on either side, plain parameter (fully expanded first), and everything else
// copied through. hsadd is applied once at the end, as in the memo.
func (p *Preprocessor) subst(m *Macro, is []Token, ap [][]Token, hs *HideSet, use Site, outer *Expansion) []Token {
	var os []Token
	arg := func(t Token) (int, bool) {
		if t.Kind != token.IDENT {
			return 0, false
		}
		i := m.Param(t.Text())
		return i, i >= 0
	}
	body := func(t Token) Token {
		t.Exp = &Expansion{
			Macro: m.Name,
			Use:   use,
			Def:   Site{Origin: t.Origin, Pos: t.Pos, End: t.End},
			Outer: outer,
		}
		return t
	}

	for i := 0; i < len(is); i++ {
		t := is[i]

		// # parameter
		if t.Kind == token.HASH && i+1 < len(is) {
			if n, ok := arg(is[i+1]); ok {
				st := p.gen.Stringize(ap[n])
				st.Flags = t.Flags & token.FlagAdjacent
				os = append(os, body(st))
				i++
				continue
			}
		}

		// , ## __VA_ARGS__ — GNU's comma swallow.
		//
		// The ## here does not paste. It marks the comma as belonging to the
		// variadic argument: if that argument is empty the comma goes with
		// it, and otherwise both stay and the argument is substituted the
		// ordinary way. Every logging macro in C is written with it, and
		// there is no ISO spelling that does the same thing — __VA_OPT__ is
		// C23's answer and this code is C17.
		if t.Kind == token.HASHHASH && i+1 < len(is) && len(os) > 0 &&
			os[len(os)-1].Kind == token.COMMA && m.Variadic {
			if n, ok := arg(is[i+1]); ok && n == len(m.Params) {
				if len(ap[n]) == 0 {
					os = os[:len(os)-1]
				} else {
					os = append(os, spaceLike(is[i+1], p.expandClosed(ap[n]))...)
				}
				i++
				continue
			}
		}

		// ## parameter, and ## anything
		if t.Kind == token.HASHHASH && i+1 < len(is) {
			nxt := is[i+1]
			if n, ok := arg(nxt); ok {
				if len(ap[n]) > 0 {
					os = p.glue(os, ap[n], t)
				}
				i++
				continue
			}
			os = p.glue(os, []Token{body(nxt)}, t)
			i++
			continue
		}

		// parameter ## — the argument goes in unexpanded, and the ## is left
		// for the next iteration to consume.
		if n, ok := arg(t); ok && i+1 < len(is) && is[i+1].Kind == token.HASHHASH {
			if len(ap[n]) == 0 {
				i++ // drop the parameter and the ##; C99's placemarker rule
				continue
			}
			os = append(os, spaceLike(t, ap[n])...)
			continue
		}

		// plain parameter: fully expanded before substitution
		if n, ok := arg(t); ok {
			os = append(os, spaceLike(t, p.expandClosed(ap[n]))...)
			continue
		}

		os = append(os, body(t))
	}
	return hsadd(hs, os)
}

// spaceLike copies ts, giving its first token the spacing the parameter had in
// the replacement list. `x + y +z` must keep the space before y and not before
// z, whatever the arguments looked like at the call.
func spaceLike(param Token, ts []Token) []Token {
	out := append([]Token(nil), ts...)
	if len(out) > 0 {
		out[0].Flags &^= token.FlagAdjacent
		out[0].Flags |= param.Flags & token.FlagAdjacent
	}
	return out
}

// glue pastes the last token of ls with the first of rs. §6.10.3.3p3 requires
// the result be a single preprocessing token; anything else is a constraint
// violation, reported once, with both operands left in place so the rest of
// the line still parses.
func (p *Preprocessor) glue(ls, rs []Token, at Token) []Token {
	if len(ls) == 0 {
		return append(ls, rs...)
	}
	l := ls[len(ls)-1]
	r := rs[0]
	pos, end := p.gen.Paste(l, r)
	spelling := p.gen.Origin().Gen.slice(pos, end)

	kind, ok := p.rescan(spelling)
	if !ok {
		p.errorf(at.Site(), "pasting %q and %q does not give a valid preprocessing token",
			l.Text(), r.Text())
		return append(ls, rs...)
	}
	pasted := Token{
		Kind:   kind,
		Flags:  l.Flags & token.FlagAdjacent,
		Pos:    pos,
		End:    end,
		Origin: p.gen.Origin(),
		Hide:   hsInter(l.Hide, r.Hide),
		Exp:    l.Exp,
	}
	out := append(ls[:len(ls)-1:len(ls)-1], pasted)
	return append(out, rs[1:]...)
}

// rescan asks the scanner whether a spelling is exactly one token. Results are
// cached: the same paste recurs constantly in macro-heavy headers.
func (p *Preprocessor) rescan(spelling string) (token.Kind, bool) {
	if k, ok := p.pasteCache[spelling]; ok {
		return k.kind, k.ok
	}
	f := token.NewFile("<paste>", []byte(spelling+"\n"))
	toks, diags := scanPP(f)
	res := struct {
		kind token.Kind
		ok   bool
	}{}
	if len(diags) == 0 && len(toks) == 2 && toks[1].Kind == token.EOF {
		res.kind, res.ok = toks[0].Kind, true
	}
	p.pasteCache[spelling] = res
	return res.kind, res.ok
}
