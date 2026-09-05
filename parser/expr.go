package parser

import (
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
)

// Expression: AssignmentExpression {, AssignmentExpression}.
func (p *parser) parseExpr() ast.Expr {
	x := p.parseAssign()
	for p.at(token.COMMA) {
		op := p.pos()
		p.next()
		y := p.parseAssign()
		x = &ast.BinaryExpr{Span: ast.Span{Lo: x.Pos(), Hi: y.End()},
			X: x, OpPos: op, Op: token.COMMA, Y: y}
	}
	return x
}

// parseAssign parses a conditional expression and, if an assignment
// operator follows, builds an AssignExpr. The grammar constrains the
// left operand to a unary expression; that is a check on the finished
// tree, not a parsing decision — same policy as ConstantExpression.
func (p *parser) parseAssign() ast.Expr {
	x := p.parseCond()
	if p.kind() == token.ASSIGN || isAssignOp(p.kind()) {
		op, opPos := p.kind(), p.pos()
		p.next()
		rhs := p.parseAssign() // right-associative
		return &ast.AssignExpr{Span: ast.Span{Lo: x.Pos(), Hi: rhs.End()},
			Lhs: x, OpPos: opPos, Op: op, Rhs: rhs}
	}
	return x
}

func isAssignOp(k token.Kind) bool {
	switch k {
	case token.ASSIGN, token.MUL_ASSIGN, token.QUO_ASSIGN, token.REM_ASSIGN,
		token.ADD_ASSIGN, token.SUB_ASSIGN, token.SHL_ASSIGN, token.SHR_ASSIGN,
		token.AND_ASSIGN, token.XOR_ASSIGN, token.OR_ASSIGN:
		return true
	}
	return false
}

// parseCond: LogicalOrExpression [? Expression : ConditionalExpression].
// ConstantExpression is this production; constant-ness is a check.
func (p *parser) parseCond() ast.Expr {
	x := p.parseBinary(2) // 2 is ||'s level; COMMA (1) never binds here
	if !p.at(token.QUESTION) {
		return x
	}
	c := &ast.CondExpr{Cond: x, Question: p.pos()}
	p.next()
	c.Then = p.parseExpr()
	c.Colon = p.expect(token.COLON)
	c.Else = p.parseCond() // right-associative
	c.Span = ast.Span{Lo: x.Pos(), Hi: c.Else.End()}
	return c
}

// parseBinary is precedence climbing over the collapsed tower: one
// BinaryExpr shape, precedence from token.Precedence.
func (p *parser) parseBinary(minPrec int) ast.Expr {
	x := p.parseCastExpr()
	for {
		op := p.kind()
		prec := op.Precedence()
		if prec < minPrec {
			return x
		}
		opPos := p.pos()
		p.next()
		y := p.parseBinary(prec + 1) // all binary levels left-associate
		x = &ast.BinaryExpr{Span: ast.Span{Lo: x.Pos(), Hi: y.End()},
			X: x, OpPos: opPos, Op: op, Y: y}
	}
}

// parseCastExpr settles cast vs. parenthesized expression with the
// typedef table: (T) - x is a cast iff T is in the table. A ( type )
// followed by { is a compound literal, which is postfix.
func (p *parser) parseCastExpr() ast.Expr {
	p.depth++
	defer func() { p.depth-- }()
	lo := p.pos()
	if p.tooDeep() {
		return &ast.BadExpr{Span: p.span(lo)}
	}

	if p.at(token.LPAREN) && p.isTypeNameStartAt(1) {
		lp := p.pos()
		p.next()
		tn := p.parseTypeName()
		rp := p.expect(token.RPAREN)
		if p.at(token.LBRACE) {
			cl := &ast.CompoundLit{Lparen: lp, Type: tn, Rparen: rp,
				Init: p.parseInitList()}
			cl.Span = p.span(lo)
			return p.parsePostfixSuffixes(cl)
		}
		x := p.parseCastExpr()
		return &ast.CastExpr{Span: p.span(lo), Lparen: lp, Type: tn, Rparen: rp, X: x}
	}
	return p.parseUnary()
}

func (p *parser) isTypeNameStartAt(n int) bool {
	return p.isTypeSpecStart(p.peekTok(n))
}

func (p *parser) parseUnary() ast.Expr {
	lo := p.pos()
	switch p.kind() {
	case token.INC, token.DEC:
		op, opPos := p.kind(), p.pos()
		p.next()
		x := p.parseUnary()
		return &ast.UnaryExpr{Span: p.span(lo), OpPos: opPos, Op: op, X: x}

	case token.LAND:
		// gcc's &&label. The scanner made one token of the two ampersands,
		// which is what tells this apart from taking the address of an
		// address — there is no such expression in C.
		andand := p.pos()
		p.next()
		return &ast.LabelAddrExpr{Span: p.span(lo), AndAnd: andand, Label: p.expectIdent()}

	case token.AND, token.MUL, token.ADD, token.SUB, token.TILDE, token.NOT:
		op, opPos := p.kind(), p.pos()
		p.next()
		x := p.parseCastExpr()
		return &ast.UnaryExpr{Span: p.span(lo), OpPos: opPos, Op: op, X: x}

	case token.SIZEOF:
		s := &ast.SizeofExpr{Sizeof: p.pos()}
		p.next()
		// sizeof ( TypeName ) iff the token after ( opens a type;
		// but a brace after the ) means the parenthesized type was
		// a compound literal's, and the whole thing is the operand.
		if p.at(token.LPAREN) && p.isTypeNameStartAt(1) {
			lp := p.pos()
			p.next()
			tn := p.parseTypeName()
			rp := p.expect(token.RPAREN)
			if p.at(token.LBRACE) {
				cl := &ast.CompoundLit{Lparen: lp, Type: tn, Rparen: rp,
					Init: p.parseInitList()}
				cl.Span = ast.Span{Lo: lp, Hi: p.prevEnd()}
				s.X = p.parsePostfixSuffixes(cl)
			} else {
				s.Lparen, s.Type, s.Rparen = lp, tn, rp
			}
		} else {
			s.X = p.parseUnary()
		}
		s.Span = p.span(lo)
		return s

	case token.ALIGNOF:
		a := &ast.AlignofExpr{Alignof: p.pos()}
		p.next()
		a.Lparen = p.expect(token.LPAREN)
		a.Type = p.parseTypeName()
		a.Rparen = p.expect(token.RPAREN)
		a.Span = p.span(lo)
		return a
	}
	return p.parsePostfixSuffixes(p.parsePrimary())
}

func (p *parser) parsePostfixSuffixes(x ast.Expr) ast.Expr {
	for {
		switch p.kind() {
		case token.LBRACK:
			ix := &ast.IndexExpr{X: x, Lbrack: p.pos()}
			p.next()
			ix.Index = p.parseExpr()
			ix.Rbrack = p.expect(token.RBRACK)
			ix.Span = ast.Span{Lo: x.Pos(), Hi: p.prevEnd()}
			x = ix

		case token.LPAREN:
			c := &ast.CallExpr{Fun: x, Lparen: p.pos()}
			p.next()
			if !p.at(token.RPAREN) {
				for {
					start := p.i
					c.Args = append(c.Args, p.parseAssign())
					if p.i == start {
						p.advanceTo(parenFollow)
						break
					}
					if !p.at(token.COMMA) {
						break
					}
					p.next()
				}
			}
			c.Rparen = p.expect(token.RPAREN)
			c.Span = ast.Span{Lo: x.Pos(), Hi: p.prevEnd()}
			x = c

		case token.PERIOD, token.ARROW:
			s := &ast.SelectorExpr{X: x, OpPos: p.pos(), Op: p.kind()}
			p.next()
			s.Sel = p.expectIdent()
			s.Span = ast.Span{Lo: x.Pos(), Hi: p.prevEnd()}
			x = s

		case token.INC, token.DEC:
			x = &ast.IncDecExpr{Span: ast.Span{Lo: x.Pos(), Hi: p.tok().End},
				X: x, OpPos: p.pos(), Op: p.kind()}
			p.next()

		default:
			return x
		}
	}
}

func (p *parser) parsePrimary() ast.Expr {
	p.skipPragmas()
	lo := p.pos()
	switch p.kind() {
	case token.INT_LIT, token.FLOAT_LIT, token.CHAR_LIT:
		t := p.tok()
		p.next()
		return &ast.BasicLit{Span: ast.Span{Lo: t.Pos, Hi: t.End}, Kind: t.Kind}

	case token.STRING_LIT:
		return p.parseStringRun()

	case token.LPAREN:
		lp := p.pos()
		p.next()
		if p.at(token.LBRACE) {
			// gcc's statement expression. A brace cannot open an expression,
			// so the two forms are told apart by one token.
			body := p.parseCompound(true)
			rp := p.expect(token.RPAREN)
			return &ast.StmtExpr{Span: p.span(lo), Lparen: lp, Body: body, Rparen: rp}
		}
		x := p.parseExpr()
		rp := p.expect(token.RPAREN)
		return &ast.ParenExpr{Span: p.span(lo), Lparen: lp, X: x, Rparen: rp}

	case token.GENERIC:
		return p.parseGeneric(lo)

	case token.IDENT:
		switch p.name(p.tok()) {
		case "__builtin_offsetof":
			if p.peekTok(1).Kind == token.LPAREN {
				return p.parseOffsetof(lo)
			}
		case "__builtin_va_arg":
			if p.peekTok(1).Kind == token.LPAREN {
				return p.parseVaArg(lo)
			}
		case "__builtin_types_compatible_p":
			if p.peekTok(1).Kind == token.LPAREN {
				return p.parseTypesCompatible(lo)
			}
		}
		return p.ident()
	}
	p.errHere("expected expression")
	return &ast.BadExpr{Span: p.span(lo)}
}

// parseStringRun collects one phase-6 run of adjacent string
// literals: one node, one span per segment, prefixes included.
func (p *parser) parseStringRun() *ast.StringLit {
	lo := p.pos()
	s := &ast.StringLit{}
	for p.at(token.STRING_LIT) {
		t := p.tok()
		s.Segs = append(s.Segs, ast.Span{Lo: t.Pos, Hi: t.End})
		p.next()
	}
	s.Span = p.span(lo)
	return s
}

func (p *parser) parseGeneric(lo token.Pos) ast.Expr {
	g := &ast.GenericExpr{Generic: p.pos()}
	p.next()
	g.Lparen = p.expect(token.LPAREN)
	g.Ctrl = p.parseAssign()
	p.expect(token.COMMA)
	for {
		alo := p.pos()
		a := &ast.GenericAssoc{}
		if p.at(token.DEFAULT) {
			a.Default = p.pos()
			p.next()
		} else {
			a.Type = p.parseTypeName()
		}
		a.Colon = p.expect(token.COLON)
		a.Value = p.parseAssign()
		a.Span = p.span(alo)
		g.Assocs = append(g.Assocs, a)
		if !p.at(token.COMMA) {
			break
		}
		p.next()
	}
	g.Rparen = p.expect(token.RPAREN)
	g.Span = p.span(lo)
	return g
}

// parseOffsetof reads __builtin_offsetof(type-name, member-designator).
//
// Neither argument is an expression: the first is a type name and the second
// is a chain of member selections and subscripts rooted at a member name, so
// this cannot be a call and the parser has to know it. vcc's own <stddef.h>
// writes offsetof as an address computation instead, which works; this is
// here because headers written for gcc use the builtin directly.
func (p *parser) parseOffsetof(lo token.Pos) ast.Expr {
	e := &ast.OffsetofExpr{Keyword: p.pos()}
	p.next()
	e.Lparen = p.expect(token.LPAREN)
	e.Type = p.parseTypeName()
	if p.at(token.COMMA) {
		p.next()
		e.Member = p.parseOffsetofMember()
	}
	e.Rparen = p.expect(token.RPAREN)
	e.Span = p.span(lo)
	return e
}

// parseOffsetofMember reads the designator chain: a member name, then any
// number of .name and [expr] steps.
func (p *parser) parseOffsetofMember() ast.Expr {
	lo := p.pos()
	var x ast.Expr = p.expectIdent()
	for {
		switch {
		case p.at(token.PERIOD):
			sel := &ast.SelectorExpr{X: x, OpPos: p.pos(), Op: token.PERIOD}
			p.next()
			sel.Sel = p.expectIdent()
			sel.Span = p.span(lo)
			x = sel
		case p.at(token.LBRACK):
			ix := &ast.IndexExpr{X: x, Lbrack: p.pos()}
			p.next()
			ix.Index = p.parseExpr()
			ix.Rbrack = p.expect(token.RBRACK)
			ix.Span = p.span(lo)
			x = ix
		default:
			return x
		}
	}
}

// parseTypesCompatible reads __builtin_types_compatible_p(type-name,
// type-name). Both operands are types, so like offsetof it cannot be a call.
func (p *parser) parseTypesCompatible(lo token.Pos) ast.Expr {
	e := &ast.TypesCompatibleExpr{Keyword: p.pos()}
	p.next()
	e.Lparen = p.expect(token.LPAREN)
	e.A = p.parseTypeName()
	if p.at(token.COMMA) {
		p.next()
		e.B = p.parseTypeName()
	} else {
		p.errHere("__builtin_types_compatible_p takes two type names")
	}
	e.Rparen = p.expect(token.RPAREN)
	e.Span = p.span(lo)
	return e
}

// parseVaArg reads __builtin_va_arg(ap, type-name). Its second operand is a
// type, so it cannot be a call and the parser has to know it.
func (p *parser) parseVaArg(lo token.Pos) ast.Expr {
	e := &ast.VaArgExpr{Keyword: p.pos()}
	p.next()
	e.Lparen = p.expect(token.LPAREN)
	e.Ap = p.parseAssign()
	if p.at(token.COMMA) {
		p.next()
		e.Type = p.parseTypeName()
	}
	e.Rparen = p.expect(token.RPAREN)
	e.Span = p.span(lo)
	return e
}
