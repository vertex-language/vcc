package parser

import (
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
)

func (p *parser) parseStmt() ast.Stmt {
	p.depth++
	defer func() { p.depth-- }()
	p.skipPragmas()
	lo := p.pos()
	if p.tooDeep() {
		return &ast.BadStmt{Span: p.span(lo)}
	}

	switch p.kind() {
	case token.IDENT:
		switch p.name(p.tok()) {
		case asmKeyword1, asmKeyword2, asmKeyword3:
			if p.atAsmStmt() {
				return p.parseAsmStmt(lo)
			}
		case "__label__":
			// gcc's local label declaration. It exists to scope a label to
			// the block it is written in, which matters only for a label
			// used from a nested statement expression; vcc's labels are
			// function-scoped and unique per function, so declaring one
			// changes nothing and the declaration is consumed.
			p.next()
			for p.at(token.IDENT) {
				p.next()
				if !p.at(token.COMMA) {
					break
				}
				p.next()
			}
			p.expect(token.SEMI)
			return &ast.EmptyStmt{Span: p.span(lo)}
		}
		// Labels first: label names are their own namespace, so
		// `T: ;` labels even when T is a typedef.
		if p.peekTok(1).Kind == token.COLON {
			l := &ast.LabeledStmt{Label: p.ident()}
			l.Colon = p.pos()
			p.next()
			l.Stmt = p.parseStmt()
			l.Span = p.span(lo)
			return l
		}
		if p.isDeclStartHere() { // a typedef name: declaration
			return p.declStmt(lo)
		}
		return p.parseExprStmt(lo)

	case token.CASE, token.DEFAULT:
		// Placement discipline (outside switch) is checked later.
		c := &ast.CaseStmt{Keyword: p.pos(), Kind: p.kind()}
		p.next()
		if c.Kind == token.CASE {
			c.Value = p.parseCond()
			if p.at(token.ELLIPSIS) {
				// gcc's case range. The two bounds are constant
				// expressions and the range includes both.
				p.next()
				c.High = p.parseCond()
			}
		}
		c.Colon = p.expect(token.COLON)
		c.Stmt = p.parseStmt()
		c.Span = p.span(lo)
		return c

	case token.LBRACE:
		return p.parseCompound(true)

	case token.SEMI:
		semi := p.pos()
		p.next()
		return &ast.EmptyStmt{Span: p.span(lo), Semi: semi}

	case token.IF:
		s := &ast.IfStmt{If: p.pos()}
		p.next()
		s.Lparen = p.expect(token.LPAREN)
		s.Cond = p.parseExpr()
		s.Rparen = p.expect(token.RPAREN)
		s.Then = p.parseStmt()
		// Dangling else binds to the nearest unmatched if — which
		// this call structure produces naturally.
		if p.at(token.ELSE) {
			s.ElsePos = p.pos()
			p.next()
			s.Else = p.parseStmt()
		}
		s.Span = p.span(lo)
		return s

	case token.SWITCH:
		s := &ast.SwitchStmt{Switch: p.pos()}
		p.next()
		s.Lparen = p.expect(token.LPAREN)
		s.Cond = p.parseExpr()
		s.Rparen = p.expect(token.RPAREN)
		s.Body = p.parseStmt()
		s.Span = p.span(lo)
		return s

	case token.WHILE:
		s := &ast.WhileStmt{While: p.pos()}
		p.next()
		s.Lparen = p.expect(token.LPAREN)
		s.Cond = p.parseExpr()
		s.Rparen = p.expect(token.RPAREN)
		s.Body = p.parseStmt()
		s.Span = p.span(lo)
		return s

	case token.DO:
		s := &ast.DoStmt{Do: p.pos()}
		p.next()
		s.Body = p.parseStmt()
		s.While = p.expect(token.WHILE)
		s.Lparen = p.expect(token.LPAREN)
		s.Cond = p.parseExpr()
		s.Rparen = p.expect(token.RPAREN)
		s.Semi = p.expectSemi()
		s.Span = p.span(lo)
		return s

	case token.FOR:
		return p.parseFor(lo)

	case token.GOTO:
		s := &ast.GotoStmt{Goto: p.pos()}
		p.next()
		if p.at(token.MUL) {
			// gcc's computed goto: the operand is an address, produced by
			// &&label somewhere in this function.
			p.next()
			s.Target = p.parseCastExpr()
		} else {
			s.Label = p.expectIdent() // target discipline is checked later
		}
		s.Semi = p.expectSemi()
		s.Span = p.span(lo)
		return s

	case token.LEAVE:
		s := &ast.SEHLeaveStmt{Leave: p.pos()}
		p.next()
		s.Semi = p.expectSemi()
		s.Span = p.span(lo)
		return s

	case token.TRY:
		s := &ast.SEHTryStmt{Try: p.pos()}
		p.next()
		s.Body = p.parseStmt()
		if p.at(token.EXCEPT) {
			s.Except = p.pos()
			p.next()
			p.expect(token.LPAREN)
			s.Filter = p.parseExpr()
			p.expect(token.RPAREN)
			s.Handler = p.parseStmt()
		} else if p.at(token.FINALLY) {
			s.Finally = p.pos()
			p.next()
			s.Handler = p.parseStmt()
		} else {
			p.errHere("expected __except or __finally after __try block")
		}
		s.Span = p.span(lo)
		return s

	case token.CONTINUE:
		s := &ast.ContinueStmt{Continue: p.pos()}
		p.next()
		s.Semi = p.expectSemi()
		s.Span = p.span(lo)
		return s

	case token.BREAK:
		s := &ast.BreakStmt{Break: p.pos()}
		p.next()
		s.Semi = p.expectSemi()
		s.Span = p.span(lo)
		return s

	case token.RETURN:
		s := &ast.ReturnStmt{Return: p.pos()}
		p.next()
		if !p.at(token.SEMI) {
			s.Result = p.parseExpr()
		}
		s.Semi = p.expectSemi()
		s.Span = p.span(lo)
		return s

	default:
		if p.isDeclStartHere() {
			return p.declStmt(lo)
		}
		return p.parseExprStmt(lo)
	}
}

func (p *parser) declStmt(lo token.Pos) ast.Stmt {
	d := p.parseDeclaration()
	return &ast.DeclStmt{Span: ast.Span{Lo: d.Pos(), Hi: d.End()}, D: d}
}

func (p *parser) parseExprStmt(lo token.Pos) ast.Stmt {
	x := p.parseExpr()
	semi := p.expectSemi()
	return &ast.ExprStmt{Span: p.span(lo), X: x, Semi: semi}
}

// parseCompound parses { BlockItemList }. push is false when the
// caller already opened the scope (function bodies: parameters live
// in the same scope as the body's outermost declarations).
func (p *parser) parseCompound(push bool) *ast.CompoundStmt {
	lo := p.pos()
	cs := &ast.CompoundStmt{Lbrace: p.expect(token.LBRACE)}
	if push {
		p.pushScope()
		defer p.popScope()
	}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		start := p.i
		cs.Items = append(cs.Items, p.parseStmt())
		if p.i == start { // progress check: force a resync
			p.advanceTo(declFollow)
			if p.at(token.SEMI) {
				p.next()
			}
			if p.i == start {
				p.next()
			}
		}
	}
	cs.Rbrace = p.expect(token.RBRACE)
	cs.Span = p.span(lo)
	return cs
}

// parseFor covers both for forms. In the declaration form the
// GenDecl owns its semicolon and Semi1 is NoPos — no ';' terminates
// the for-init clause separately.
func (p *parser) parseFor(lo token.Pos) ast.Stmt {
	s := &ast.ForStmt{For: p.pos()}
	p.next()
	s.Lparen = p.expect(token.LPAREN)
	if p.isDeclStartHere() {
		s.Init = p.parseDeclaration()
	} else {
		if !p.at(token.SEMI) {
			s.Init = p.parseExpr()
		}
		s.Semi1 = p.expect(token.SEMI)
	}
	if !p.at(token.SEMI) {
		s.Cond = p.parseExpr()
	}
	s.Semi2 = p.expect(token.SEMI)
	if !p.at(token.RPAREN) {
		s.Post = p.parseExpr()
	}
	s.Rparen = p.expect(token.RPAREN)
	s.Body = p.parseStmt()
	s.Span = p.span(lo)
	return s
}

// The three spellings of the keyword. `asm` itself is not reserved in ISO C,
// which is why every system header writes one of the other two.
const (
	asmKeyword1 = "asm"
	asmKeyword2 = "__asm"
	asmKeyword3 = "__asm__"
)

func isAsmKeyword(name string) bool {
	return name == asmKeyword1 || name == asmKeyword2 || name == asmKeyword3
}

// parseAsmDecl reads gcc's file-scope assembly, `asm("...")` outside any
// function. It has no operands — there is no object in scope to constrain —
// so it is the template and nothing else.
func (p *parser) parseAsmDecl() ast.Decl {
	lo := p.pos()
	a := &ast.AsmDecl{Keyword: p.pos()}
	p.next()
	a.Lparen = p.expect(token.LPAREN)
	a.Template = p.parseAsmString("assembly")
	if a.Template == nil {
		p.advanceTo(parenFollow)
	}
	if p.at(token.RPAREN) {
		a.Rparen = p.pos()
		p.next()
	} else {
		p.errHere("expected ')' after file-scope assembly")
		p.advanceTo(declFollow)
	}
	if p.at(token.SEMI) {
		a.Semi = p.pos()
		p.next()
	}
	a.Span = p.span(lo)
	return a
}

// atAsmStmt reports whether the asm keyword under the cursor begins a
// statement rather than something else spelled with the same word — an
// assembler label on a declaration, mostly. The test is that a run of
// qualifiers leads to the opening parenthesis, and `goto` is one of them:
// `asm goto (...)` is the form whose labels this parser now reads.
func (p *parser) atAsmStmt() bool {
	for n := 1; n < 8; n++ {
		switch t := p.peekTok(n); t.Kind {
		case token.LPAREN:
			return true
		case token.VOLATILE, token.INLINE, token.GOTO:
		case token.IDENT:
			if !toleratedBare[p.name(t)] && !inlineSpellings[p.name(t)] &&
				p.name(t) != "__volatile" {
				return false
			}
		default:
			return false
		}
	}
	return false
}

// parseAsmStmt reads gcc's inline assembly statement.
//
// Two forms share one node. Basic asm is `asm("...")` with no colon in it,
// and the whole construct is the template. Extended asm is the one with
// colons, and each colon opens a list: outputs, then inputs, then clobbers,
// then — only for the goto form — the labels the assembled text may branch
// to. A list may be empty, which is why `::: "memory"` is three colons and
// not a typo.
//
// The lists are parsed rather than skipped, which is the change: an operand
// is a constraint string and an expression, and an expression is exactly
// what the rest of this parser already reads. What each constraint means is
// a question for lower, which is the layer that knows the target.
func (p *parser) parseAsmStmt(lo token.Pos) ast.Stmt {
	a := &ast.AsmStmt{Keyword: p.pos()}
	p.next()
	p.parseAsmQualifiers(a)

	a.Lparen = p.expect(token.LPAREN)
	if a.Lparen == token.NoPos {
		// Not the construct after all. Consume to the end of the statement
		// the way the old skip did, so one malformed asm does not cascade.
		p.advanceTo(declFollow)
		if p.at(token.SEMI) {
			p.next()
		}
		a.Span = p.span(lo)
		return a
	}

	a.Template = p.parseAsmString("assembly template")
	if a.Template == nil {
		return p.badAsm(a, lo)
	}

	// Each colon opens the next list. gcc allows a trailing colon with
	// nothing after it, and an omitted list is empty rather than absent.
	for section := 0; p.at(token.COLON); section++ {
		p.next()
		switch section {
		case 0:
			a.Outputs = p.parseAsmOperands()
		case 1:
			a.Inputs = p.parseAsmOperands()
		case 2:
			a.Clobbers = p.parseAsmStringList()
		case 3:
			if !a.Goto {
				p.errHere("only `asm goto` has a label list")
				return p.badAsm(a, lo)
			}
			a.Labels = p.parseAsmLabels()
		default:
			p.errHere("too many ':' sections in an asm statement")
			return p.badAsm(a, lo)
		}
		a.Extended = true
		if p.dead {
			return p.badAsm(a, lo)
		}
	}

	if !p.at(token.RPAREN) {
		p.errHere("expected ')' or ':' in the asm statement")
		return p.badAsm(a, lo)
	}
	a.Rparen = p.pos()
	p.next()
	a.Semi = p.expectSemi()
	a.Span = p.span(lo)
	return a
}

// parseAsmQualifiers consumes volatile, inline and goto in any order, in
// both the plain and the __-decorated spellings. The bare tolerated
// spellings are consumed and dropped, as they are everywhere else.
func (p *parser) parseAsmQualifiers(a *ast.AsmStmt) {
	for {
		switch {
		case p.at(token.VOLATILE):
			a.Volatile = true
		case p.at(token.INLINE):
			a.Inline = true
		case p.at(token.GOTO):
			a.Goto = true
		case p.at(token.IDENT):
			switch p.name(p.tok()) {
			case "__volatile__", "__volatile":
				a.Volatile = true
			case "__inline__", "__inline":
				a.Inline = true
			default:
				if !toleratedBare[p.name(p.tok())] && !inlineSpellings[p.name(p.tok())] {
					return
				}
			}
		default:
			return
		}
		p.next()
	}
}

// badAsm recovers from a malformed asm by consuming the rest of the
// construct. The node is returned rather than a BadStmt so that the
// statement still has an extent and lower still sees an asm — one that
// reports nothing further, because the parser already did.
func (p *parser) badAsm(a *ast.AsmStmt, lo token.Pos) ast.Stmt {
	p.advanceTo(parenFollow)
	if p.at(token.RPAREN) {
		p.next()
	}
	if p.at(token.SEMI) {
		p.next()
	}
	a.Template = nil
	a.Span = p.span(lo)
	return a
}

// parseAsmOperands reads one comma-separated operand list, which ends at
// the next colon or at the closing parenthesis.
func (p *parser) parseAsmOperands() []*ast.AsmOperand {
	if p.at(token.COLON) || p.at(token.RPAREN) || p.at(token.EOF) {
		return nil
	}
	var out []*ast.AsmOperand
	for {
		o := p.parseAsmOperand()
		if o == nil {
			return out
		}
		out = append(out, o)
		if !p.at(token.COMMA) {
			return out
		}
		p.next()
	}
}

// parseAsmOperand is `[name] "constraint" (expression)`.
func (p *parser) parseAsmOperand() *ast.AsmOperand {
	lo := p.pos()
	o := &ast.AsmOperand{}

	if p.at(token.LBRACK) {
		o.Lbrack = p.pos()
		p.next()
		o.Name = p.expectIdent()
		o.Rbrack = p.expect(token.RBRACK)
		if o.Name == nil {
			return nil
		}
	}

	o.Constraint = p.parseAsmString("operand constraint")
	if o.Constraint == nil {
		return nil
	}

	o.Lparen = p.expect(token.LPAREN)
	if o.Lparen == token.NoPos {
		return nil
	}
	o.X = p.parseExpr()
	o.Rparen = p.expect(token.RPAREN)
	o.Span = p.span(lo)
	return o
}

// parseAsmStringList reads the clobber list: comma-separated strings.
func (p *parser) parseAsmStringList() []*ast.StringLit {
	if !p.at(token.STRING_LIT) {
		return nil
	}
	var out []*ast.StringLit
	for {
		out = append(out, p.parseStringRun())
		if !p.at(token.COMMA) {
			return out
		}
		p.next()
	}
}

// parseAsmLabels reads asm goto's label list: comma-separated identifiers,
// which name labels in this function rather than objects.
func (p *parser) parseAsmLabels() []*ast.Ident {
	if !p.at(token.IDENT) {
		return nil
	}
	var out []*ast.Ident
	for {
		id := p.expectIdent()
		if id == nil {
			return out
		}
		out = append(out, id)
		if !p.at(token.COMMA) {
			return out
		}
		p.next()
	}
}

// parseAsmString reads one phase-6 run of adjacent string literals in a
// position that requires one.
func (p *parser) parseAsmString(what string) *ast.StringLit {
	if !p.at(token.STRING_LIT) {
		p.errHere("expected a string literal for the " + what)
		return nil
	}
	return p.parseStringRun()
}
