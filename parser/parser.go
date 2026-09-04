// Package parser turns a *token.File into an *ast.File plus a sorted
// diagnostic slice.
//
// Recursive descent for declarations and statements, precedence
// climbing for expressions, and one typedef table instead of any
// rollback machinery — C's ambiguities are semantic, not structural.
//
// The parser interprets nothing except typedef names. It decides
// which production applies and where each node begins and ends; it
// does not decode literals or check §6.7 constraints beyond the
// production admitting the tokens.
//
// A partial parse is a usable one: every entry point returns a node —
// a Bad* placeholder if it must — so consumers read a tree, not a
// success flag.
package parser

import (
	"fmt"

	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/scanner"
	"github.com/vertex-language/vcc/token"
)

// Mode controls optional parser behavior.
type Mode uint

const (
	// ParseComments retains comment tokens on the File.
	ParseComments Mode = 1 << iota
	// SkipBodies skips function bodies balanced, not parsed —
	// declarations, prototypes and typedefs still land. A fast
	// structural pass.
	SkipBodies
	// Tolerant keeps going past the resync budget. For editors;
	// wasteful in batch builds.
	Tolerant
)

// DefaultMode is zero.
const DefaultMode Mode = 0

const (
	maxResync = 100  // recovery attempts before going dead
	maxDepth  = 1000 // nesting cap: declarators, statements, types, expressions
)

// ParseFile runs the scanner itself and parses the unit. The tree is
// never nil; diagnostics from phases 1–2, scanning and parsing arrive
// merged and sorted. Input is expected to be preprocessed C.
func ParseFile(f *token.File, mode Mode) (*ast.File, []token.Diagnostic) {
	var sm scanner.Mode
	if mode&ParseComments != 0 {
		sm = scanner.ScanComments
	}
	toks, diags := scanner.Scan(f, sm)

	p := &parser{f: f, mode: mode, diags: diags}
	file := &ast.File{Unit: f}
	file.SetReleaser(&arena{})

	if mode&ParseComments != 0 {
		for _, t := range toks {
			if t.Kind == token.COMMENT {
				file.Comments = append(file.Comments, t)
			} else {
				p.toks = append(p.toks, t)
			}
		}
	} else {
		p.toks = toks
	}

	p.pushScope() // file scope
	for !p.at(token.EOF) {
		start := p.i
		file.Decls = append(file.Decls, p.parseExternalDecl())
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

	lo := p.toks[0].Pos
	file.Span = ast.Span{Lo: lo, Hi: p.widen(lo, p.toks[len(p.toks)-1].End)}
	token.SortDiagnostics(p.diags)
	return file, p.diags
}

// arena is the seam through which node storage will be batched; ast
// sees it only as a Releaser. Today it releases nothing — the promise
// (every node is invalid after Release) is the API; batching is an
// optimization this type reserves the right to add without changing
// any signature.
type arena struct{}

func (*arena) Release() {}

type parser struct {
	f    *token.File
	toks []token.Token
	i    int
	mode Mode

	diags   []token.Diagnostic
	quiet   bool      // reported; no token consumed since
	lastErr token.Pos // never report twice at one position
	resyncs int
	dead    bool // past the budget, not Tolerant: run to EOF silently
	depth   int

	scopes []map[string]bool // name → is-typedef

	// asmLabel is the most recent `__asm__("name")` a run of tolerated
	// spellings consumed. takeAsmLabel says why it lives here.
	asmLabel *ast.StringLit

	// declAttrs is the attributes a run of tolerated spellings consumed on
	// the way through a declarator, and lives here for the reason asmLabel
	// does. Most of them say nothing; aligned decides where the object
	// starts, and `short data[64] __attribute__((aligned(16)))` — every SIMD
	// library in C writes its buffers that way — faults an aligned vector
	// load if it is dropped.
	declAttrs []*ast.Attr
}

// ---- token access ----

// takeAsmLabel returns the assembler label the last run of tolerated
// spellings held, and forgets it.
//
// It is parser state rather than a return value because the label is
// consumed deep inside parseDeclarator — the suffix loop skips the tolerated
// spellings before it looks for `[` or `(` — and only two callers, the two
// that build a declaration, have anywhere to put it. Everywhere else it is
// dropped, which is what happened to it before it was recorded at all.
func (p *parser) takeAsmLabel() *ast.StringLit {
	l := p.asmLabel
	p.asmLabel = nil
	return l
}

// takeDeclAttrs is takeAsmLabel for the attributes, under the same rule and
// for the same reason: they are consumed in the same loop.
func (p *parser) takeDeclAttrs() []*ast.Attr {
	a := p.declAttrs
	p.declAttrs = nil
	return a
}

func (p *parser) tok() token.Token     { return p.toks[p.i] }
func (p *parser) kind() token.Kind     { return p.toks[p.i].Kind }
func (p *parser) at(k token.Kind) bool { return p.kind() == k }
func (p *parser) pos() token.Pos       { return p.toks[p.i].Pos }

func (p *parser) peekTok(n int) token.Token {
	if p.i+n >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.i+n]
}

func (p *parser) next() {
	if p.kind() != token.EOF {
		p.i++
		p.quiet = false
	}
}

func (p *parser) prevEnd() token.Pos {
	if p.i == 0 {
		return p.toks[0].Pos
	}
	return p.toks[p.i-1].End
}

// span closes a node's extent at the last consumed token; non-empty
// even when nothing was consumed.
func (p *parser) span(lo token.Pos) ast.Span {
	return ast.Span{Lo: lo, Hi: p.widen(lo, p.prevEnd())}
}

// widen returns end, or a one-column span at pos when end closes at or
// before it, so a node built from nothing still underlines something.
//
// The clamp is the whole reason this is a method. EOF sits at the file's
// one-past-the-end position — the largest Pos a File converts back to an
// offset — and widening there produces a Pos the file cannot answer for:
// a panic in the renderer, one diagnostic later, at a position no source
// byte corresponds to. A span with nowhere to grow stays empty, and
// printSnippetAt already draws a single caret under an empty one.
func (p *parser) widen(pos, end token.Pos) token.Pos {
	if end > pos {
		return end
	}
	return min(pos+1, p.f.Pos(p.f.Size()))
}

func (p *parser) name(t token.Token) string {
	return string(p.f.Slice(t.Pos, t.End))
}

func (p *parser) ident() *ast.Ident {
	t := p.tok()
	p.next()
	return &ast.Ident{Span: ast.Span{Lo: t.Pos, Hi: t.End}}
}

// ---- diagnostics: one recoverable diagnostic, never a cascade ----

// errHere reports at the current token, then goes quiet until a
// token is consumed. It never reports twice at one position, and
// reports nothing once dead.
func (p *parser) errHere(msg string) {
	if p.dead || p.quiet {
		return
	}
	t := p.tok()
	p.quiet = true
	if t.Pos <= p.lastErr {
		return
	}
	p.lastErr = t.Pos
	p.diags = append(p.diags, token.Diagnostic{
		Pos: t.Pos, End: p.widen(t.Pos, t.End), Severity: token.Error, Message: msg,
	})
}

// warnHere reports a warning at the current token. It shares none of
// errHere's cascade machinery, deliberately: a warning is not a parse
// failure, so it does not set quiet (an error at the same token must
// still report) and does not touch lastErr (sharing it would let a
// warning suppress that error). It respects dead — a parser past the
// resync budget is silent entirely. Callers are expected to consume
// the token they warned about, which is what keeps one site from
// warning twice.
func (p *parser) warnHere(msg string) {
	if p.dead {
		return
	}
	t := p.tok()
	p.diags = append(p.diags, token.Diagnostic{
		Pos: t.Pos, End: p.widen(t.Pos, t.End), Severity: token.Warn, Message: msg,
	})
}

func (p *parser) expect(k token.Kind) token.Pos {
	if p.at(k) {
		pos := p.pos()
		p.next()
		return pos
	}
	p.errHere(fmt.Sprintf("expected '%s'", k))
	return token.NoPos
}

func (p *parser) expectSemi() token.Pos {
	if p.at(token.SEMI) {
		pos := p.pos()
		p.next()
		return pos
	}
	p.errHere("expected ';'")
	p.advanceTo(declFollow)
	if p.at(token.SEMI) {
		pos := p.pos()
		p.next()
		return pos
	}
	return token.NoPos
}

func (p *parser) expectIdent() *ast.Ident {
	if p.at(token.IDENT) {
		return p.ident()
	}
	p.errHere("expected identifier")
	return nil
}

// ---- recovery ----

var (
	declFollow  = map[token.Kind]bool{token.SEMI: true, token.RBRACE: true}
	fieldFollow = map[token.Kind]bool{token.SEMI: true, token.RBRACE: true}
	parenFollow = map[token.Kind]bool{token.RPAREN: true, token.SEMI: true}
	braceFollow = map[token.Kind]bool{token.COMMA: true, token.RBRACE: true}
)

// advanceTo resyncs to a follow set, stepping over balanced bracket
// groups. Past maxResync attempts it stops reporting and runs to EOF,
// unless Tolerant.
func (p *parser) advanceTo(follow map[token.Kind]bool) {
	p.resyncs++
	if p.resyncs > maxResync && p.mode&Tolerant == 0 {
		p.dead = true
	}
	if p.dead {
		p.i = len(p.toks) - 1 // the EOF token
		return
	}
	for {
		k := p.kind()
		if k == token.EOF || follow[k] {
			return
		}
		switch k {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			p.skipBalanced()
		default:
			p.next()
		}
	}
}

// skipBalanced consumes an opener through its matching closer.
func (p *parser) skipBalanced() {
	depth := 0
	for {
		switch p.kind() {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
		case token.EOF:
			return
		}
		p.next()
		if depth <= 0 {
			return
		}
	}
}

// tooDeep is the maxDepth guard; on breach it reports once and
// consumes a token so callers returning Bad* still make progress.
func (p *parser) tooDeep() bool {
	if p.depth <= maxDepth {
		return false
	}
	p.errHere("nesting too deep")
	if !p.at(token.EOF) {
		p.next()
	}
	return true
}

// ---- the typedef table ----
//
// A per-scope set of names — nothing about the types they denote.
// It follows scope exactly, including immediate visibility after a
// declarator: names are declared before their initializers parse.

func (p *parser) pushScope() { p.scopes = append(p.scopes, map[string]bool{}) }
func (p *parser) popScope()  { p.scopes = p.scopes[:len(p.scopes)-1] }

func (p *parser) declare(name string, isTypedef bool) {
	p.scopes[len(p.scopes)-1][name] = isTypedef
}

func (p *parser) isTypedefName(name string) bool {
	for i := len(p.scopes) - 1; i >= 0; i-- {
		if t, ok := p.scopes[i][name]; ok {
			return t
		}
	}
	return false
}

// ---- classification ----

// isTypeSpecStart: can this token open a specifier-qualifier list?
// Tolerated builtin types count: they open one wherever a typedef
// name would, which is what keeps `extern _Float16 f(_Float16);`
// reading its parens as a parameter list.
func (p *parser) isTypeSpecStart(t token.Token) bool {
	switch t.Kind {
	case token.VOID, token.CHAR, token.SHORT, token.INT, token.LONG,
		token.FLOAT, token.DOUBLE, token.SIGNED, token.UNSIGNED,
		token.BOOL, token.COMPLEX, token.INT128, token.INT64, token.INT32, token.INT16, token.INT8, token.AUTO_TYPE, token.STRUCT,
		token.UNION, token.ENUM, token.ATOMIC, token.CONST, token.RESTRICT,
		token.VOLATILE:
		return true
	case token.IDENT:
		name := p.name(t)
		if typeofSpellings[name] {
			// Only when a paren follows. The underscored spellings are
			// reserved and could be nothing else, but plain `typeof` is an
			// ordinary identifier until C23 and a program may have declared
			// one; `int typeof;` still declares an int.
			return p.peekTok(p.offsetOf(t)+1).Kind == token.LPAREN
		}
		return toleratedType[name] || p.isTypedefName(name)
	}
	return false
}

// offsetOf finds a token's index relative to the cursor, for the one-token
// lookahead isTypeSpecStart needs. The callers pass either the current token
// or the one after it, so the search is over two positions and not a scan.
func (p *parser) offsetOf(t token.Token) int {
	if p.i < len(p.toks) && p.toks[p.i].Pos == t.Pos {
		return 0
	}
	return 1
}

// isDeclSpecStart adds the declaration-only specifiers.
//
// The tolerated spellings count, and have to: `__declspec(align(16)) char
// buf[32];` inside a function is a declaration, and a statement that opens
// with an identifier this function does not recognize is parsed as an
// expression instead — which then fails on the type that follows, three
// diagnostics deep, none of them naming the reason. MSVC writes local
// alignment that way and so does every Windows header that needs it.
func (p *parser) isDeclSpecStart(t token.Token) bool {
	switch t.Kind {
	case token.TYPEDEF, token.EXTERN, token.STATIC, token.THREAD_LOCAL,
		token.AUTO, token.REGISTER, token.INLINE, token.NORETURN,
		token.ALIGNAS:
		return true
	case token.IDENT:
		// Only the attribute forms, and only with their paren. The bare
		// tolerated spellings are not enough: __cdecl opens no declaration
		// on its own and appears inside declarators — `void (__cdecl*)(void)`
		// is a parameter, and reading it as a nested declaration is a worse
		// answer than the one this function is here to give.
		if name := p.name(t); toleratedParen[name] && !isAsmKeyword(name) {
			return p.peekTok(p.offsetOf(t)+1).Kind == token.LPAREN
		}
	}
	return p.isTypeSpecStart(t)
}

// isDeclStartHere settles declaration vs. expression statement by the
// first token.
func (p *parser) isDeclStartHere() bool {
	if p.at(token.STATIC_ASSERT) {
		return true
	}
	return p.isDeclSpecStart(p.tok())
}
