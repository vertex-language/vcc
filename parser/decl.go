package parser

import (
	"strings"

	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
)

// ---- the tolerated-spelling carve-out ----
//
// A closed, documented set of extension spellings is parsed and
// discarded in declarations only — never given semantics, never
// allowed anywhere else, visible as absent in --emit cir. The list
// grows only on evidence from real system headers, with this table
// updated in the same commit as the CI gate that motivated it.

var toleratedBare = map[string]bool{
	"__restrict": true, "__restrict__": true,
	"__extension__": true,
	"__const":       true, "__volatile__": true, "__signed__": true,

	// clang's nullability qualifiers. They qualify a pointer and say nothing
	// about its representation, so there is nothing here to act on — but a
	// .i file produced by `clang -E` still contains them, because clang
	// knows them natively and so does not macro them away. A compiler that
	// accepts preprocessed clang output has to read them.
	"_Nullable": true, "_Nonnull": true, "_Null_unspecified": true,
	"__nullable": true, "__nonnull": true, "__null_unspecified": true,

	// MSVC calling conventions and pointer modifiers
	"__cdecl": true, "__stdcall": true, "__fastcall": true, "__vectorcall": true, "__thiscall": true,
	"__ptr32": true, "__ptr64": true, "__sptr": true, "__uptr": true, "__w64": true, "__unaligned": true,
}

// inlineSpellings are gcc's and MSVC's names for the inline specifier.
//
// These are not tolerated spellings and are not discarded: they are the same
// function specifier under another name, and dropping one turns an inline
// definition into an ordinary one. That is not a nothing. Darwin's
// <stdio.h> resolves __header_inline to `extern __inline` for a compiler
// defining __GNUC__, so with the spelling discarded every unit that included
// it defined __sputc outright, and two such units would not link.
//
// The keyword they resolve to is token.INLINE, and what it then means is
// §6.7.4's, exactly as if the program had written inline.
var inlineSpellings = map[string]bool{
	"__inline": true, "__inline__": true, "__forceinline": true,
}

var toleratedParen = map[string]bool{ // spelling followed by a (…) group
	"__attribute__": true, "__declspec": true, "__pragma": true,
	"__asm": true, "__asm__": true,
}

// toleratedType names compiler-provided types that appear in system
// headers with no declaring typedef — the compiler is expected to
// just know them. Unlike the discarded spellings above, these cannot
// vanish: a declaration needs a type specifier. Each parses as a
// TypedefType, the same node a typedef-table match produces; giving
// the type semantics (or rejecting a use of it) is the analyzer's
// decision, not the parser's.
//
// Evidence: Darwin's arm64 SDK declares __uint128_t vectors
// (arm/_mcontext.h reached via <stdio.h>) and _Float16 math
// functions (<math.h>) behind #if defined(__arm64__) — an
// architecture gate, not a compiler gate, so no unknown-compiler
// fallback protects vcc from them. __int128_t and __fp16 are the
// sibling spellings the same conditional blocks use. __int128 is not
// here: it is a type specifier rather than a typedef name, so it is a
// keyword (token.INT128) and combines with signed and unsigned.
//
// Like the tables above, this applies everywhere, not only in system
// headers: the reparse bridge (see cli/pp.go) erases Origin before
// the parser runs, so System-ness is not yet answerable here. When
// parser.ParseTokens lands and tokens arrive carrying their Origin,
// this table should gate on it.
// typeofSpellings are gcc's three names for the same specifier. typeof is
// the plain one; the underscored forms exist so a program may use it while
// -std=c11 reserves the identifier.
var typeofSpellings = map[string]bool{
	"typeof": true, "__typeof__": true, "__typeof": true,
}

var toleratedType = map[string]bool{
	"__int128_t": true, "__uint128_t": true,
	"_Float16": true, "__fp16": true,
	// __builtin_va_list is not tolerated-and-discarded: the analyzer
	// predeclares it as a typedef, so it has a real type. It is here because
	// the parser has to know it names one before the analyzer runs.
	"__builtin_va_list": true,
}

// skipPragmas consumes any run of MSVC __pragma(...) groups.
//
// __pragma is how a pragma is written inside a macro replacement list, so it
// turns up wherever a macro can: at file scope after a declaration, before a
// statement, and in the middle of an expression — the Windows SDK writes
// isinf(x) with one in front of it. It is a pragma, and vcc honours no pragma
// this could be, so consuming it is the whole of the handling.
//
// It is deliberately narrower than skipTolerated. A calling convention or an
// __attribute__ in an expression would be a syntax error worth reporting; a
// __pragma there is ordinary Windows C.
func (p *parser) skipPragmas() bool {
	consumed := false
	for p.at(token.IDENT) && p.name(p.tok()) == "__pragma" && p.peekTok(1).Kind == token.LPAREN {
		p.next()
		p.skipBalanced()
		consumed = true
	}
	return consumed
}

func (p *parser) skipTolerated() bool {
	consumed, _ := p.takeTolerated()
	return consumed
}

// takeTolerated consumes the extension spellings and returns the attributes
// among them.
//
// __attribute__ is parsed rather than skipped because two of its entries
// change what a program means: packed and aligned decide a record's layout,
// and a compiler that drops them silently produces a struct of the wrong
// shape — a wrong answer, where refusing to compile would at least have been
// a visible one. The rest are collected and ignored, which is what they are
// worth, and keeps one code path for all of them.
//
// __declspec is parsed for the same reason and by the same rule. It is
// MSVC's spelling of the idea, and align(n) inside one decides a layout as
// surely as aligned(n) inside the other: <setjmp.h> declares the sixteen-byte
// halves of a jmp_buf with __declspec(align(16)), and a compiler that drops
// it gives the buffer eight-byte alignment. _setjmp then stores xmm6 to it
// with movdqa, which faults — inside the CRT, on an address the program
// never computed, for a program that looks entirely ordinary.
//
// __asm__ stays discarded: it is an assembler label or an asm statement,
// neither of which is a declaration specifier.
func (p *parser) takeTolerated() (bool, []*ast.Attr) {
	consumed := false
	var attrs []*ast.Attr
	for p.at(token.IDENT) {
		switch name := p.name(p.tok()); {
		case name == "__attribute__":
			attrs = append(attrs, p.parseAttributeList()...)
		case name == "__declspec":
			attrs = append(attrs, p.parseDeclspecList()...)
		case toleratedBare[name]:
			p.next()
		case toleratedParen[name]:
			isAsm := isAsmKeyword(name)
			p.next()
			if !p.at(token.LPAREN) {
				break
			}
			if !isAsm || p.peekTok(1).Kind != token.STRING_LIT {
				p.skipBalanced()
				break
			}
			// An assembler label is the one tolerated group whose contents
			// mean something: it renames the symbol and changes nothing
			// else. The rest are discarded, which is what tolerated means.
			p.next()
			p.asmLabel = p.parseStringRun()
			if p.at(token.RPAREN) {
				p.next()
				break
			}
			p.errHere("expected ')' after an assembler label")
			p.advanceTo(parenFollow)
			if p.at(token.RPAREN) {
				p.next()
			}
		default:
			return consumed, attrs
		}
		consumed = true
	}
	return consumed, attrs
}

// parseTypeof reads gcc's typeof (expr) or typeof (type-name).
//
// Which of the two it is takes the same lookahead a sizeof needs: the token
// after the paren opens a type, or it does not. `typeof(x)` where x is a
// typedef name is the type; where x is an object it is that object's type,
// and the two spellings are indistinguishable without the typedef table —
// which is exactly what the table is for.
func (p *parser) parseTypeof() ast.Expr {
	lo := p.pos()
	t := &ast.TypeofType{Keyword: p.pos()}
	p.next()
	if !p.at(token.LPAREN) {
		p.errHere("expected '(' after typeof")
		t.Span = p.span(lo)
		return t
	}
	t.Lparen = p.pos()
	p.next()
	if p.isTypeNameStartAt(0) {
		t.Type = p.parseTypeName()
	} else {
		t.X = p.parseExpr()
	}
	t.Rparen = p.expect(token.RPAREN)
	t.Span = p.span(lo)
	return t
}

// parseDeclspecList reads one __declspec (...) — MSVC's single-paren form —
// and returns the entries inside.
//
// The entries are separated by whitespace rather than by commas, which is the
// only shape difference from the inner list of an __attribute__; a comma is
// accepted anyway, since both spellings turn up.
func (p *parser) parseDeclspecList() []*ast.Attr {
	p.next() // __declspec
	if !p.at(token.LPAREN) {
		return nil
	}
	p.next()
	var attrs []*ast.Attr
	for !p.at(token.RPAREN) && !p.at(token.EOF) {
		if p.at(token.COMMA) {
			p.next()
			continue
		}
		a := p.parseAttribute()
		if a == nil {
			break
		}
		attrs = append(attrs, a)
	}
	p.expect(token.RPAREN)
	return attrs
}

// parseAttributeList reads one __attribute__ ((...)) — the doubled parens
// gcc's syntax requires — and returns the entries inside.
func (p *parser) parseAttributeList() []*ast.Attr {
	p.next() // __attribute__
	if !p.at(token.LPAREN) {
		return nil
	}
	outer := p.i
	p.next()
	if !p.at(token.LPAREN) {
		// Not the doubled form. Put the cursor back where skipping would
		// have left it rather than guessing at what was written.
		p.i = outer
		p.skipBalanced()
		return nil
	}
	p.next()

	var attrs []*ast.Attr
	for !p.at(token.RPAREN) && !p.at(token.EOF) {
		if p.at(token.COMMA) { // an empty entry is allowed
			p.next()
			continue
		}
		a := p.parseAttribute()
		if a == nil {
			break
		}
		attrs = append(attrs, a)
		if p.at(token.COMMA) {
			p.next()
		}
	}
	p.expect(token.RPAREN)
	p.expect(token.RPAREN)
	return attrs
}

// attrTakesExprs names the attributes whose arguments vcc reads. Anything
// else has its arguments skipped; see parseAttribute.
//
// align is MSVC's spelling of aligned, written inside a __declspec rather
// than an __attribute__, and it decides a layout in exactly the same way.
var attrTakesExprs = map[string]bool{"aligned": true, "align": true, "allocate": true}

// attrBaseName strips gcc's doubled underscores: __aligned__ and aligned are
// the same attribute, the doubling existing so a macro named aligned cannot
// break a header.
func attrBaseName(n string) string {
	if len(n) > 4 && strings.HasPrefix(n, "__") && strings.HasSuffix(n, "__") {
		return n[2 : len(n)-2]
	}
	return n
}

// parseAttribute reads one entry: a name, optionally followed by arguments.
func (p *parser) parseAttribute() *ast.Attr {
	lo := p.pos()
	if !p.at(token.IDENT) {
		// An attribute name may be a keyword — __attribute__((const)) is
		// one — so anything that is not a punctuator is taken as the name.
		if p.at(token.RPAREN) || p.at(token.EOF) {
			return nil
		}
		p.next()
		if p.at(token.LPAREN) {
			p.skipBalanced()
		}
		return &ast.Attr{Span: p.span(lo)}
	}
	a := &ast.Attr{Name: p.ident()}
	if p.at(token.LPAREN) {
		if !attrTakesExprs[attrBaseName(a.Name.Name(p.f))] {
			// Arguments are parsed only for the attributes whose arguments
			// are read. Everything else is skipped as tokens, because an
			// attribute's arguments are not required to be expressions at
			// all: clang's availability(macosx, introduced=10.12.1) has a
			// version number where an expression would go, and parsing it
			// as one turns an attribute nobody acts on into a syntax error.
			a.Lparen = p.pos()
			p.skipBalanced()
			a.Span = p.span(lo)
			return a
		}
		a.Lparen = p.pos()
		p.next()
		for !p.at(token.RPAREN) && !p.at(token.EOF) {
			a.Args = append(a.Args, p.parseAssign())
			if !p.at(token.COMMA) {
				break
			}
			p.next()
		}
		a.Rparen = p.expect(token.RPAREN)
	}
	a.Span = p.span(lo)
	return a
}

// ---- external declarations ----

func (p *parser) parseExternalDecl() ast.Decl {
	p.skipPragmas()
	switch {
	case p.at(token.SEMI):
		// A stray file-scope ';' violates C17's grammar — a
		// declaration requires at least one declaration specifier —
		// but C23 legalized it and every deployed compiler accepts
		// it; Apple's sys headers ship them (clang -E shows the
		// ';;' too). Same posture as #warning: a version gap, not a
		// dialect, so it is accepted under a warning and kept
		// visible as an EmptyDecl. When parser.ParseTokens lands
		// and Origin survives to here, warnings sited in system
		// headers can get the once-per-header treatment phase 4's
		// warnings already have.
		p.warnHere("extra ';' at file scope is not ISO C17")
		lo := p.pos()
		semi := p.pos()
		p.next()
		return &ast.EmptyDecl{Span: p.span(lo), Semi: semi}
	case p.at(token.STATIC_ASSERT):
		return p.parseStaticAssert()
	case p.at(token.IDENT) && isAsmKeyword(p.name(p.tok())) && p.peekTok(1).Kind == token.LPAREN:
		// gcc's file-scope assembly. Only the basic form reaches here: an
		// operand would have to name an object, and at file scope there is
		// no object in scope to constrain.
		return p.parseAsmDecl()
	}

	lo := p.pos()
	specs := p.parseDeclSpecs(false)
	if len(specs) == 0 {
		p.errHere("expected declaration")
		start := p.i
		p.advanceTo(declFollow)
		if p.i == start {
			p.next()
		}
		return &ast.BadDecl{Span: p.span(lo)}
	}
	if p.at(token.SEMI) { // struct S { … };  enum E { … };
		semi := p.pos()
		p.next()
		return &ast.GenDecl{Span: p.span(lo), Specs: specs, Semi: semi}
	}

	d := p.parseDeclarator(modeConcrete)

	// FunctionDefinition: Declarator [DeclarationList] Compound.
	// That the declarator has function type is §6.9.1's constraint,
	// checked later — grammatically, a body or a K&R declaration
	// list after the first declarator means definition.
	if p.at(token.LBRACE) || p.isDeclStartHere() {
		return p.parseFuncDef(lo, specs, d)
	}
	return p.finishGenDecl(lo, specs, d)
}

func (p *parser) parseFuncDef(lo token.Pos, specs ast.DeclSpecs, d ast.Declarator) ast.Decl {
	fn := &ast.FuncDecl{Specs: specs, Decl: d, AsmLabel: p.takeAsmLabel(), Name: declNameOf(d)}
	p.declareDeclarator(d, hasKeyword(specs, token.TYPEDEF))

	p.pushScope()
	p.declareParams(d)

	// K&R declaration list, kept whole. (_Static_assert is
	// grammatically a Declaration but the KR field holds GenDecls;
	// it is vanishingly rare here and reported by the analyzer.)
	for p.isDeclStartHere() && !p.at(token.STATIC_ASSERT) {
		kd := p.parseDeclaration()
		if g, ok := kd.(*ast.GenDecl); ok {
			fn.KR = append(fn.KR, g)
		}
	}

	if p.mode&SkipBodies != 0 {
		fn.Body = p.skipBody()
	} else {
		fn.Body = p.parseCompound(false) // scope already pushed
	}
	p.popScope()

	fn.Span = p.span(lo)
	return fn
}

// skipBody consumes a function body balanced, not parsed.
func (p *parser) skipBody() *ast.CompoundStmt {
	lo := p.pos()
	lb := p.expect(token.LBRACE)
	depth := 1
	var rb token.Pos
	for !p.at(token.EOF) {
		switch p.kind() {
		case token.LBRACE:
			depth++
		case token.RBRACE:
			depth--
			if depth == 0 {
				rb = p.pos()
				p.next()
				return &ast.CompoundStmt{Span: p.span(lo), Lbrace: lb, Rbrace: rb}
			}
		}
		p.next()
	}
	// Unterminated: the scanner's bracket stack already reported it.
	return &ast.CompoundStmt{Span: p.span(lo), Lbrace: lb, Rbrace: rb}
}

// parseDeclaration is the block-scope form: no function definitions.
func (p *parser) parseDeclaration() ast.Decl {
	if p.at(token.STATIC_ASSERT) {
		return p.parseStaticAssert()
	}
	lo := p.pos()
	specs := p.parseDeclSpecs(false)
	if p.at(token.SEMI) {
		semi := p.pos()
		p.next()
		return &ast.GenDecl{Span: p.span(lo), Specs: specs, Semi: semi}
	}
	d := p.parseDeclarator(modeConcrete)
	return p.finishGenDecl(lo, specs, d)
}

func (p *parser) finishGenDecl(lo token.Pos, specs ast.DeclSpecs, first ast.Declarator) *ast.GenDecl {
	isTypedef := hasKeyword(specs, token.TYPEDEF)
	g := &ast.GenDecl{Specs: specs}
	d := first
	for {
		// Immediate visibility: the name is declared before its
		// initializer parses, so `typedef int T; void f(void)
		// { T T; T * x; }` reads T T; as a declaration and T * x
		// as multiplication by the next statement.
		p.declareDeclarator(d, isTypedef)
		id := &ast.InitDeclarator{
			Span: ast.Span{Lo: d.Pos(), Hi: d.End()}, Decl: d,
			AsmLabel: p.takeAsmLabel(), Attrs: p.takeDeclAttrs(),
		}
		if p.at(token.ASSIGN) {
			id.Assign = p.pos()
			p.next()
			id.Init = p.parseInitializer()
			id.Hi = p.prevEnd()
		}
		g.List = append(g.List, id)
		if !p.at(token.COMMA) {
			break
		}
		p.next()
		start := p.i
		d = p.parseDeclarator(modeConcrete)
		if p.i == start { // Bad with no progress: let expectSemi recover
			break
		}
	}
	g.Semi = p.expectSemi()
	g.Span = p.span(lo)
	return g
}

func (p *parser) parseStaticAssert() *ast.StaticAssertDecl {
	lo := p.pos()
	d := &ast.StaticAssertDecl{Keyword: p.pos()}
	p.next()
	d.Lparen = p.expect(token.LPAREN)
	d.Cond = p.parseCond() // ConstantExpression: a shape check happens later
	d.Comma = p.expect(token.COMMA)
	if p.at(token.STRING_LIT) {
		d.Msg = p.parseStringRun()
	} else {
		p.errHere("expected string literal")
		p.advanceTo(parenFollow)
	}
	d.Rparen = p.expect(token.RPAREN)
	d.Semi = p.expectSemi()
	d.Span = p.span(lo)
	return d
}

// ---- declaration specifiers, in written order ----

// parseDeclSpecs consumes specifiers greedily. sq restricts to the
// specifier-qualifier list (struct members, type names). Constraint
// checking — specifier multisets, at most one storage class — is the
// type-building phase's job; this loop only decides membership.
//
// An identifier is consumed as a TypedefName only while no type
// specifier has been seen (§6.7.7's tie-break): in `unsigned T;` a
// typedef T is the declarator, not a second specifier. A tolerated
// builtin type is consumed under the same rule, without consulting
// the typedef table: nothing declares it.
func (p *parser) parseDeclSpecs(sq bool) ast.DeclSpecs {
	var specs ast.DeclSpecs
	sawType := false
	for {
		if took, attrs := p.takeTolerated(); took {
			if len(attrs) > 0 {
				specs = append(specs, &ast.AttrSpec{
					Span:  attrs[0].Span,
					Attrs: attrs,
				})
			}
			continue
		}
		t := p.tok()
		switch t.Kind {
		case token.TYPEDEF, token.EXTERN, token.STATIC, token.THREAD_LOCAL,
			token.AUTO, token.REGISTER, token.INLINE, token.NORETURN:
			if sq {
				return specs
			}
			specs = append(specs, p.keywordSpec())
		case token.VOID, token.CHAR, token.SHORT, token.INT, token.LONG,
			token.FLOAT, token.DOUBLE, token.SIGNED, token.UNSIGNED,
			token.BOOL, token.COMPLEX, token.INT128, token.INT64, token.INT32, token.INT16, token.INT8, token.AUTO_TYPE:
			sawType = true
			specs = append(specs, p.keywordSpec())
		case token.CONST, token.RESTRICT, token.VOLATILE:
			specs = append(specs, p.keywordSpec())
		case token.ATOMIC:
			// Type specifier when ( immediately follows, qualifier
			// otherwise — the standard's own tie-break.
			if p.peekTok(1).Kind == token.LPAREN {
				specs = append(specs, p.parseAtomicType())
				sawType = true
			} else {
				specs = append(specs, p.keywordSpec())
			}
		case token.ALIGNAS:
			if sq {
				return specs
			}
			specs = append(specs, p.parseAlignas())
		case token.STRUCT, token.UNION:
			specs = append(specs, p.parseStructType())
			sawType = true
		case token.ENUM:
			specs = append(specs, p.parseEnumDecl())
			sawType = true
		case token.IDENT:
			if inlineSpellings[p.name(t)] {
				// A function specifier, so it leaves a specifier-qualifier
				// list alone exactly as `inline` does.
				if sq {
					return specs
				}
				p.next()
				specs = append(specs, &ast.KeywordSpec{
					Span: ast.Span{Lo: t.Pos, Hi: t.End},
					Kind: token.INLINE,
				})
				continue
			}
			if !sawType && typeofSpellings[p.name(t)] {
				specs = append(specs, p.parseTypeof())
				sawType = true
				continue
			}
			if !sawType && toleratedType[p.name(t)] {
				// A compiler-provided type from the tolerated set:
				// parsed as if a typedef, never declared as one.
				id := p.ident()
				specs = append(specs, &ast.TypedefType{Span: id.Span, Name: id})
				sawType = true
				continue
			}
			if sawType || !p.isTypedefName(p.name(t)) {
				return specs
			}
			id := p.ident()
			specs = append(specs, &ast.TypedefType{Span: id.Span, Name: id})
			sawType = true
		default:
			return specs
		}
	}
}

func (p *parser) keywordSpec() *ast.KeywordSpec {
	t := p.tok()
	p.next()
	return &ast.KeywordSpec{Span: ast.Span{Lo: t.Pos, Hi: t.End}, Kind: t.Kind}
}

func (p *parser) parseAtomicType() *ast.AtomicType {
	lo := p.pos()
	a := &ast.AtomicType{Atomic: p.pos()}
	p.next()
	a.Lparen = p.expect(token.LPAREN)
	a.Type = p.parseTypeName()
	a.Rparen = p.expect(token.RPAREN)
	a.Span = p.span(lo)
	return a
}

func (p *parser) parseAlignas() *ast.AlignasSpec {
	lo := p.pos()
	a := &ast.AlignasSpec{Alignas: p.pos()}
	p.next()
	a.Lparen = p.expect(token.LPAREN)
	if p.isTypeSpecStart(p.tok()) {
		a.Type = p.parseTypeName()
	} else {
		a.X = p.parseCond()
	}
	a.Rparen = p.expect(token.RPAREN)
	a.Span = p.span(lo)
	return a
}

func (p *parser) parseStructType() *ast.StructType {
	p.depth++
	defer func() { p.depth-- }()
	lo := p.pos()
	st := &ast.StructType{Keyword: p.pos(), Kind: p.kind()}
	p.next()
	if p.tooDeep() {
		st.Span = p.span(lo)
		return st
	}
	// gcc admits attributes in two places on a struct specifier: between the
	// keyword and the tag, and after the closing brace.
	if _, attrs := p.takeTolerated(); len(attrs) > 0 {
		st.Attrs = append(st.Attrs, attrs...)
	}
	if p.at(token.IDENT) {
		st.Name = p.ident()
	}
	if p.at(token.LBRACE) {
		st.Lbrace = p.pos()
		p.next()
		for !p.at(token.RBRACE) && !p.at(token.EOF) {
			start := p.i
			st.Fields = append(st.Fields, p.parseStructDeclaration())
			if p.i == start {
				p.advanceTo(fieldFollow)
				if p.at(token.SEMI) {
					p.next()
				}
				if p.i == start {
					p.next()
				}
			}
		}
		st.Rbrace = p.expect(token.RBRACE)
		if _, attrs := p.takeTolerated(); len(attrs) > 0 {
			st.Attrs = append(st.Attrs, attrs...)
		}
	} else if st.Name == nil {
		p.errHere("expected identifier or '{' after struct/union")
	}
	st.Span = p.span(lo)
	return st
}

func (p *parser) parseStructDeclaration() ast.Decl {
	if p.at(token.STATIC_ASSERT) {
		return p.parseStaticAssert()
	}
	lo := p.pos()
	fd := &ast.FieldDecl{Specs: p.parseDeclSpecs(true)}
	if len(fd.Specs) == 0 {
		p.errHere("expected specifier-qualifier list")
	}
	for !p.at(token.SEMI) && !p.at(token.EOF) {
		flo := p.pos()
		f := &ast.FieldDeclarator{}
		if !p.at(token.COLON) {
			f.Decl = p.parseDeclarator(modeConcrete)
			flo = f.Decl.Pos()
		}
		if p.at(token.COLON) { // bit-field; width discipline is deferred
			f.Colon = p.pos()
			p.next()
			f.Width = p.parseCond()
		}
		f.Span = p.span(flo)
		fd.List = append(fd.List, f)
		if !p.at(token.COMMA) {
			break
		}
		p.next()
	}
	fd.Semi = p.expectSemi()
	fd.Span = p.span(lo)
	return fd
}

func (p *parser) parseEnumDecl() *ast.EnumDecl {
	lo := p.pos()
	e := &ast.EnumDecl{Enum: p.pos()}
	p.next()
	if p.at(token.IDENT) {
		e.Name = p.ident()
	}
	if p.at(token.LBRACE) {
		e.Lbrace = p.pos()
		p.next()
		for p.at(token.IDENT) {
			elo := p.pos()
			en := &ast.Enumerator{Name: p.ident()}
			// Enumeration constants are ordinary names: they shadow
			// typedefs in the table like any other declaration.
			p.declare(en.Name.Name(p.f), false)
			// An enumerator may carry attributes, and Darwin's <time.h>
			// gives every clock_id_t constant four of them. They say nothing
			// about the value.
			p.skipTolerated()
			if p.at(token.ASSIGN) {
				en.Assign = p.pos()
				p.next()
				en.Value = p.parseCond()
			}
			en.Span = p.span(elo)
			e.List = append(e.List, en)
			if !p.at(token.COMMA) {
				break
			}
			c := p.pos()
			p.next()
			if p.at(token.RBRACE) {
				e.Comma = c // trailing comma, kept
				break
			}
		}
		e.Rbrace = p.expect(token.RBRACE)
	} else if e.Name == nil {
		p.errHere("expected identifier or '{' after enum")
	}
	e.Span = p.span(lo)
	return e
}

// ---- declarators ----

type dmode int

const (
	modeConcrete dmode = iota // a name is required at the leaf
	modeAbstract              // a name is an error (type names)
	modeEither                // parameters: both forms
)

func (p *parser) parseDeclarator(m dmode) ast.Declarator {
	p.depth++
	defer func() { p.depth-- }()
	// Cleared here so that asmLabel always describes the declarator this
	// call is about to read, and never one an earlier declaration left
	// behind for it.
	p.asmLabel, p.declAttrs = nil, nil
	lo := p.pos()
	if p.tooDeep() {
		return &ast.BadDeclarator{Span: p.span(lo)}
	}

	// A calling convention may sit here, before the star rather than after
	// it: MSVC writes a function pointer as int (__cdecl *f)(void), and the
	// Windows SDK headers are full of them. It says nothing this compiler
	// acts on — there is one convention per target — so it is consumed and
	// dropped, exactly as the same spelling is in a specifier list.
	p.skipTolerated()

	if p.at(token.MUL) {
		pd := &ast.PtrDeclarator{Star: p.pos()}
		p.next()
		for {
			if p.skipTolerated() {
				continue
			}
			switch p.kind() {
			case token.CONST, token.RESTRICT, token.VOLATILE, token.ATOMIC:
				pd.Quals = append(pd.Quals, p.keywordSpec())
				continue
			}
			break
		}
		if p.startsDeclaratorTail(m) {
			pd.Inner = p.parseDeclarator(m)
		} else if m == modeConcrete {
			p.errHere("expected declarator after '*'")
		}
		pd.Span = p.span(lo)
		return pd
	}
	return p.parseDirectDeclarator(m, lo)
}

func (p *parser) startsDeclaratorTail(m dmode) bool {
	switch p.kind() {
	case token.MUL, token.LPAREN:
		return true
	case token.IDENT:
		return m != modeAbstract || !p.isTypeSpecStart(p.tok())
	case token.LBRACK:
		return m != modeConcrete
	}
	return false
}

func (p *parser) parseDirectDeclarator(m dmode, lo token.Pos) ast.Declarator {
	var d ast.Declarator

	switch {
	case p.at(token.IDENT):
		if m == modeAbstract {
			p.errHere("identifier in a type name")
		}
		id := p.ident()
		d = &ast.NameDeclarator{Span: id.Span, Ident: id}

	case p.at(token.LPAREN):
		// Grouping vs. parameter list. In a parameter or type name,
		// §6.7.6.3p11 applies: if what follows the ( can open a
		// parameter list — a specifier, a typedef name ("treated as
		// a typedef name if possible"), or ) — these parens are a
		// function declarator with nil inner. Otherwise they group.
		if m != modeConcrete && p.parenIsParams() {
			d = nil // the suffix loop parses ( params )
		} else {
			lp := p.pos()
			p.next()
			inner := p.parseDeclarator(m)
			rp := p.expect(token.RPAREN)
			d = &ast.ParenDeclarator{Span: p.span(lo), Lparen: lp, Inner: inner, Rparen: rp}
		}

	case p.at(token.LBRACK) && m != modeConcrete:
		d = nil // abstract: suffixes with no direct part, e.g. int[3]

	default:
		if m == modeConcrete {
			p.errHere("expected declarator")
			return &ast.BadDeclarator{Span: p.span(lo)}
		}
		d = nil
	}

	for {
		if _, attrs := p.takeTolerated(); len(attrs) > 0 {
			p.declAttrs = append(p.declAttrs, attrs...)
		}
		switch p.kind() {
		case token.LBRACK:
			d = p.parseArraySuffix(d, lo)
		case token.LPAREN:
			d = p.parseFuncSuffix(d, lo)
		default:
			if d == nil {
				d = &ast.BadDeclarator{Span: p.span(lo)}
			}
			return d
		}
	}
}

func (p *parser) parenIsParams() bool {
	t := p.peekTok(1)
	return t.Kind == token.RPAREN || p.isDeclSpecStart(t)
}

// parseArraySuffix admits every §6.7.6.2 form; static/* placement
// outside function parameters is a deliberate non-decision, checked
// later.
func (p *parser) parseArraySuffix(inner ast.Declarator, lo token.Pos) ast.Declarator {
	if inner != nil {
		lo = inner.Pos()
	}
	a := &ast.ArrayDeclarator{Inner: inner, Lbrack: p.pos()}
	p.next()
	if p.at(token.STATIC) {
		a.Static = p.pos()
		p.next()
	}
	for {
		if p.skipTolerated() {
			continue
		}
		switch p.kind() {
		case token.CONST, token.RESTRICT, token.VOLATILE, token.ATOMIC:
			a.Quals = append(a.Quals, p.keywordSpec())
			continue
		}
		break
	}
	if a.Static == token.NoPos && p.at(token.STATIC) {
		a.Static = p.pos()
		p.next()
	}
	if p.at(token.MUL) && p.peekTok(1).Kind == token.RBRACK {
		a.Star = p.pos()
		p.next()
	} else if !p.at(token.RBRACK) {
		a.Len = p.parseAssign()
	}
	a.Rbrack = p.expect(token.RBRACK)
	a.Span = p.span(lo)
	return a
}

func (p *parser) parseFuncSuffix(inner ast.Declarator, lo token.Pos) ast.Declarator {
	if inner != nil {
		lo = inner.Pos()
	}
	fd := &ast.FuncDeclarator{Inner: inner, Lparen: p.pos()}
	p.next()

	switch {
	case p.at(token.RPAREN):
		// f(): both Params and Idents nil.
	case p.isDeclSpecStart(p.tok()):
		for {
			start := p.i
			fd.Params = append(fd.Params, p.parseParamDecl())
			if p.i == start {
				p.advanceTo(parenFollow)
				break
			}
			if !p.at(token.COMMA) {
				break
			}
			p.next()
			if p.at(token.ELLIPSIS) {
				fd.Ellipsis = p.pos()
				p.next()
				break
			}
		}
	case p.at(token.IDENT): // K&R identifier list: plain identifiers only
		for p.at(token.IDENT) {
			fd.Idents = append(fd.Idents, p.ident())
			if !p.at(token.COMMA) {
				break
			}
			p.next()
		}
	default:
		p.errHere("expected parameter declaration or ')'")
		p.advanceTo(parenFollow)
	}

	fd.Rparen = p.expect(token.RPAREN)
	fd.Span = p.span(lo)
	return fd
}

func (p *parser) parseParamDecl() *ast.ParamDecl {
	lo := p.pos()
	pd := &ast.ParamDecl{Specs: p.parseDeclSpecs(false)}
	if len(pd.Specs) == 0 {
		p.errHere("expected parameter declaration")
	}
	if p.startsDeclaratorTail(modeEither) {
		pd.Decl = p.parseDeclarator(modeEither)
	}
	pd.Span = p.span(lo)
	return pd
}

// parseTypeName: SpecifierQualifierList [AbstractDeclarator].
func (p *parser) parseTypeName() *ast.TypeName {
	lo := p.pos()
	tn := &ast.TypeName{Specs: p.parseDeclSpecs(true)}
	if len(tn.Specs) == 0 {
		p.errHere("expected type name")
	}
	switch p.kind() {
	case token.MUL, token.LPAREN, token.LBRACK:
		tn.Decl = p.parseDeclarator(modeAbstract)
	}
	tn.Span = p.span(lo)
	return tn
}

// ---- initializers ----

func (p *parser) parseInitializer() ast.Expr {
	if p.at(token.LBRACE) {
		return p.parseInitList()
	}
	return p.parseAssign()
}

func (p *parser) parseInitList() *ast.InitList {
	lo := p.pos()
	il := &ast.InitList{Lbrace: p.pos()}
	p.next()
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		start := p.i
		il.Items = append(il.Items, p.parseInitItem())
		if p.at(token.COMMA) {
			c := p.pos()
			p.next()
			if p.at(token.RBRACE) {
				il.Comma = c // trailing comma, kept
			}
		} else if !p.at(token.RBRACE) {
			p.errHere("expected ',' or '}' in initializer list")
			p.advanceTo(braceFollow)
			if p.at(token.COMMA) {
				p.next()
			}
		}
		if p.i == start {
			p.next()
		}
	}
	il.Rbrace = p.expect(token.RBRACE)
	il.Span = p.span(lo)
	return il
}

func (p *parser) parseInitItem() *ast.InitItem {
	lo := p.pos()
	it := &ast.InitItem{}
	for { // designators, in written order
		if p.at(token.LBRACK) {
			dlo := p.pos()
			d := &ast.IndexDesignator{Lbrack: p.pos()}
			p.next()
			d.Index = p.parseCond()
			if p.at(token.ELLIPSIS) {
				// gcc's designator range: [lo ... hi] initializes every
				// element between the two, both included.
				p.next()
				d.High = p.parseCond()
			}
			d.Rbrack = p.expect(token.RBRACK)
			d.Span = p.span(dlo)
			it.Designators = append(it.Designators, d)
			continue
		}
		if p.at(token.PERIOD) {
			dlo := p.pos()
			d := &ast.FieldDesignator{Dot: p.pos()}
			p.next()
			d.Name = p.expectIdent()
			d.Span = p.span(dlo)
			it.Designators = append(it.Designators, d)
			continue
		}
		break
	}
	if len(it.Designators) > 0 {
		it.Assign = p.expect(token.ASSIGN)
	}
	it.Value = p.parseInitializer()
	it.Span = p.span(lo)
	return it
}

// ---- table maintenance ----

func (p *parser) declareDeclarator(d ast.Declarator, isTypedef bool) {
	if id := declNameOf(d); id != nil {
		p.declare(id.Name(p.f), isTypedef)
	}
}

// declareParams brings parameter names (prototype and K&R) into the
// scope that becomes the function body's, so a parameter shadowing a
// typedef disambiguates correctly inside the body.
func (p *parser) declareParams(d ast.Declarator) {
	if d == nil {
		return
	}
	ast.Inspect(d, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ParamDecl:
			if id := declNameOf(n.Decl); id != nil {
				p.declare(id.Name(p.f), false)
			}
		case *ast.FuncDeclarator:
			for _, id := range n.Idents {
				p.declare(id.Name(p.f), false)
			}
		}
		return true
	})
}

func declNameOf(d ast.Declarator) *ast.Ident {
	if d == nil {
		return nil
	}
	return d.DeclName()
}

func hasKeyword(specs ast.DeclSpecs, k token.Kind) bool {
	for _, s := range specs {
		if ks, ok := s.(*ast.KeywordSpec); ok && ks.Kind == k {
			return true
		}
	}
	return false
}
