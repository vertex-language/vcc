package parser

import (
	"testing"

	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
)

func parse(t *testing.T, src string, mode Mode) (*token.File, *ast.File, []token.Diagnostic) {
	t.Helper()
	unit := token.NewFile("a.c", []byte(src+"\n"))
	file, diags := ParseFile(unit, mode)
	if file == nil {
		t.Fatal("tree is nil: the tree is never nil")
	}
	return unit, file, diags
}

func errCount(ds []token.Diagnostic) int {
	n := 0
	for _, d := range ds {
		if d.Severity == token.Error {
			n++
		}
	}
	return n
}

func mustClean(t *testing.T, ds []token.Diagnostic) {
	t.Helper()
	if errCount(ds) != 0 {
		t.Fatalf("unexpected diagnostics: %v", ds)
	}
}

func TestTypedefScopeAndImmediateVisibility(t *testing.T) {
	// The README's own example: the first T of `T T;` is a type; by
	// the next statement `T * x` multiplies.
	_, f, diags := parse(t, "typedef int T; void f(void) { T T; T * x; }", 0)
	mustClean(t, diags)
	body := f.Decls[1].(*ast.FuncDecl).Body
	if _, ok := body.Items[0].(*ast.DeclStmt); !ok {
		t.Fatalf("T T; parsed as %T, want DeclStmt", body.Items[0])
	}
	es, ok := body.Items[1].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("T * x; parsed as %T, want ExprStmt", body.Items[1])
	}
	if b, ok := es.X.(*ast.BinaryExpr); !ok || b.Op != token.MUL {
		t.Fatalf("T * x is %T, want BinaryExpr MUL", es.X)
	}
}

func TestCastVsParen(t *testing.T) {
	_, f, diags := parse(t, "typedef int T; int a; int b = (T) - a; int c = (a) - 1;", 0)
	mustClean(t, diags)
	init := func(i int) ast.Expr { return f.Decls[i].(*ast.GenDecl).List[0].Init }
	if _, ok := init(2).(*ast.CastExpr); !ok {
		t.Fatalf("(T) - a is %T, want CastExpr", init(2))
	}
	if b, ok := init(3).(*ast.BinaryExpr); !ok || b.Op != token.SUB {
		t.Fatalf("(a) - 1 is %T, want BinaryExpr SUB", init(3))
	}
}

func TestSizeof(t *testing.T) {
	_, f, diags := parse(t, "typedef int T; int a; int b = sizeof (T); int c = sizeof (a);", 0)
	mustClean(t, diags)
	sz := func(i int) *ast.SizeofExpr {
		return f.Decls[i].(*ast.GenDecl).List[0].Init.(*ast.SizeofExpr)
	}
	if sz(2).Type == nil || sz(2).X != nil {
		t.Error("sizeof (T): want the TypeName form")
	}
	if sz(3).X == nil || sz(3).Type != nil {
		t.Error("sizeof (a): want the operand form")
	}
}

func TestDanglingElse(t *testing.T) {
	_, f, diags := parse(t, "void f(int a) { if (a) if (a) ; else ; }", 0)
	mustClean(t, diags)
	outer := f.Decls[0].(*ast.FuncDecl).Body.Items[0].(*ast.IfStmt)
	inner := outer.Then.(*ast.IfStmt)
	if outer.Else != nil || inner.Else == nil {
		t.Fatal("else must bind to the nearest unmatched if")
	}
}

func TestOneDiagnosticNeverACascade(t *testing.T) {
	_, f, diags := parse(t, "int x = ; int y;", 0)
	if n := errCount(diags); n != 1 {
		t.Fatalf("got %d errors %v, want exactly 1", n, diags)
	}
	if len(f.Decls) != 2 {
		t.Fatalf("got %d decls, want recovery to reach y", len(f.Decls))
	}
	if _, ok := f.Decls[0].(*ast.GenDecl).List[0].Init.(*ast.BadExpr); !ok {
		t.Error("missing initializer should be a BadExpr")
	}
}


func TestSkipBodies(t *testing.T) {
	_, f, diags := parse(t, "void f(void) { int x = {; }; } typedef int T; T t;", SkipBodies)
	mustClean(t, diags) // garbage inside the skipped body never parses
	fn := f.Decls[0].(*ast.FuncDecl)
	if len(fn.Body.Items) != 0 {
		t.Error("body should be skipped, not parsed")
	}
	// ... but declarations and typedefs after it still land.
	g := f.Decls[2].(*ast.GenDecl)
	if _, ok := g.Specs[0].(*ast.TypedefType); !ok {
		t.Fatalf("T after skipped body is %T, want TypedefType", g.Specs[0])
	}
}

func TestKRDefinition(t *testing.T) {
	unit, f, diags := parse(t, "int f(a, b) int a; int b; { return a; }", 0)
	mustClean(t, diags)
	fn := f.Decls[0].(*ast.FuncDecl)
	fd := fn.Decl.(*ast.FuncDeclarator)
	if len(fd.Idents) != 2 || len(fn.KR) != 2 {
		t.Fatalf("idents=%d KR=%d, want 2 and 2", len(fd.Idents), len(fn.KR))
	}
	if fn.Name.Name(unit) != "f" {
		t.Errorf("Name = %q", fn.Name.Name(unit))
	}
}

func TestParenParamTieBreaker(t *testing.T) {
	// §6.7.6.3p11: in int f(int (T)), if T is a typedef the parens
	// read as an abstract function declarator.
	_, f, diags := parse(t, "typedef int T; int f(int (T)); int g(int (x));", 0)
	mustClean(t, diags)
	param := func(i int) ast.Declarator {
		fd := f.Decls[i].(*ast.GenDecl).List[0].Decl.(*ast.FuncDeclarator)
		return fd.Params[0].Decl
	}
	pf := param(1).(*ast.FuncDeclarator)
	if pf.Inner != nil || len(pf.Params) != 1 {
		t.Fatal("int (T): want an abstract function declarator taking T")
	}
	if got := param(2).DeclName(); got == nil {
		t.Fatal("int (x): want a grouped concrete declarator naming x")
	}
}

func TestAtomicTieBreaker(t *testing.T) {
	_, f, diags := parse(t, "_Atomic(int) x; int * _Atomic p;", 0)
	mustClean(t, diags)
	if _, ok := f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.AtomicType); !ok {
		t.Error("_Atomic( : want the type-specifier form")
	}
	ptr := f.Decls[1].(*ast.GenDecl).List[0].Decl.(*ast.PtrDeclarator)
	if len(ptr.Quals) != 1 || ptr.Quals[0].(*ast.KeywordSpec).Kind != token.ATOMIC {
		t.Error("* _Atomic: want the qualifier form")
	}
}

func TestToleratedSpellings(t *testing.T) {
	src := "__extension__ int __attribute__((unused)) x;\n" +
		"void f(void) __attribute__((noreturn));\n" +
		"int * __restrict p;"
	_, f, diags := parse(t, src, 0)
	mustClean(t, diags)
	if len(f.Decls) != 3 {
		t.Fatalf("got %d decls, want 3", len(f.Decls))
	}
	// The bare spellings leave no node; __attribute__ leaves one, because
	// two of its entries decide a record's layout and the analyzer has to
	// see them. Here: int, then the attribute list.
	specs := f.Decls[0].(*ast.GenDecl).Specs
	if len(specs) != 2 {
		t.Fatalf("specs = %d nodes, want the type and one attribute list", len(specs))
	}
	as, ok := specs[1].(*ast.AttrSpec)
	if !ok {
		t.Fatalf("specs[1] is %T, want *ast.AttrSpec", specs[1])
	}
	if len(as.Attrs) != 1 || as.Attrs[0].Name == nil {
		t.Fatalf("attrs = %v, want one named entry", as.Attrs)
	}
}

func TestAttributeArguments(t *testing.T) {
	src := "struct __attribute__((packed, aligned(8))) s { char c; int i; };\n" +
		"struct t { int i; } __attribute__((packed));\n" +
		"int fmt(const char *, ...) __attribute__((format(printf, 1, 2)));"
	_, f, diags := parse(t, src, 0)
	mustClean(t, diags)

	st := f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.StructType)
	if len(st.Attrs) != 2 {
		t.Fatalf("attrs before the tag = %d, want 2", len(st.Attrs))
	}
	if len(st.Attrs[1].Args) != 1 {
		t.Errorf("aligned takes %d arguments, want 1", len(st.Attrs[1].Args))
	}
	// The trailing position, after the closing brace.
	st2 := f.Decls[1].(*ast.GenDecl).Specs[0].(*ast.StructType)
	if len(st2.Attrs) != 1 {
		t.Fatalf("attrs after the brace = %d, want 1", len(st2.Attrs))
	}
	// An attribute whose arguments name nothing in scope still parses.
	for _, sp := range f.Decls[2].(*ast.GenDecl).Specs {
		if as, ok := sp.(*ast.AttrSpec); ok && len(as.Attrs[0].Args) != 3 {
			t.Errorf("format takes %d arguments, want 3", len(as.Attrs[0].Args))
		}
	}
}

func TestForInitDeclaration(t *testing.T) {
	_, f, diags := parse(t, "void f(void) { for (int i = 0; i < 3; i++) ; }", 0)
	mustClean(t, diags)
	fs := f.Decls[0].(*ast.FuncDecl).Body.Items[0].(*ast.ForStmt)
	if _, ok := fs.Init.(*ast.GenDecl); !ok {
		t.Fatalf("for-init is %T, want *GenDecl", fs.Init)
	}
	if fs.Semi1 != token.NoPos {
		t.Error("Semi1 must be NoPos: no ';' terminates the for-init clause")
	}
}

func TestEnumTrailingCommaAndConstants(t *testing.T) {
	_, f, diags := parse(t, "enum E { A, B, };", 0)
	mustClean(t, diags)
	e := f.Decls[0].(*ast.GenDecl).Specs[0].(*ast.EnumDecl)
	if len(e.List) != 2 || e.Comma == token.NoPos {
		t.Fatalf("enumerators=%d trailing-comma=%v", len(e.List), e.Comma)
	}
}

func TestDigraphs(t *testing.T) {
	_, f, diags := parse(t, "int a<:2:> = <%1, 2%>;", 0)
	mustClean(t, diags)
	d := f.Decls[0].(*ast.GenDecl).List[0]
	if _, ok := d.Decl.(*ast.ArrayDeclarator); !ok {
		t.Fatalf("declarator is %T, want ArrayDeclarator", d.Decl)
	}
	if _, ok := d.Init.(*ast.InitList); !ok {
		t.Fatalf("init is %T, want InitList", d.Init)
	}
}

func TestBadSpansNonEmpty(t *testing.T) {
	_, f, _ := parse(t, "42 43", 0)
	for _, d := range f.Decls {
		if d.End() <= d.Pos() {
			t.Fatalf("%T span is empty", d)
		}
	}
}

func TestLabelBeatsTypedef(t *testing.T) {
	_, f, diags := parse(t, "typedef int T; void f(void) { T: ; }", 0)
	mustClean(t, diags)
	if _, ok := f.Decls[1].(*ast.FuncDecl).Body.Items[0].(*ast.LabeledStmt); !ok {
		t.Fatal("T: must label — label names are their own namespace")
	}
}

func TestDesignatedInitializers(t *testing.T) {
	_, f, diags := parse(t, "struct P { int x, y; }; struct P p = { .y = 2, [0].x = 1 };", 0)
	if errCount(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	il := f.Decls[1].(*ast.GenDecl).List[0].Init.(*ast.InitList)
	if len(il.Items) != 2 || len(il.Items[1].Designators) != 2 {
		t.Fatal("designators must survive in written order")
	}
}

func TestToleratedBuiltinTypes(t *testing.T) {
	src := "__uint128_t v[32];\n" +
		"extern _Float16 __fabsf16(_Float16);\n" +
		"__int128_t mul(__int128_t a, __int128_t b);"
	_, f, diags := parse(t, src, 0)
	mustClean(t, diags)
	if len(f.Decls) != 3 {
		t.Fatalf("got %d decls, want 3", len(f.Decls))
	}
	specs := f.Decls[0].(*ast.GenDecl).Specs
	if len(specs) != 1 {
		t.Fatalf("specs = %d, want 1", len(specs))
	}
	if _, ok := specs[0].(*ast.TypedefType); !ok {
		t.Fatalf("__uint128_t spec is %T, want TypedefType", specs[0])
	}
	// And in parameter position: the FuncDeclarator must have parsed
	// _Float16 as opening a parameter list, not as a grouped name.
	fd := f.Decls[1].(*ast.GenDecl).List[0].Decl.(*ast.FuncDeclarator)
	if len(fd.Params) != 1 {
		t.Fatalf("params = %d, want 1", len(fd.Params))
	}
	// A tolerated type does not shadow a real declaration: a later
	// `typedef` or variable of the same name still wins the table.
	_, f2, d2 := parse(t, "typedef int _Float16; _Float16 x;", 0)
	mustClean(t, d2)
	if len(f2.Decls) != 2 {
		t.Fatalf("shadowing: got %d decls, want 2", len(f2.Decls))
	}
}

func warnCount(ds []token.Diagnostic) int {
	n := 0
	for _, d := range ds {
		if d.Severity == token.Warn {
			n++
		}
	}
	return n
}

func TestEmptyDecl(t *testing.T) {
	// A stray file-scope ';' is a C17 constraint violation that C23
	// legalized and deployed compilers accept — Apple's sys headers
	// ship them. Accepted under a warning, kept as an EmptyDecl.
	_, f, diags := parse(t, "; int x;", 0)
	if errCount(diags) != 0 {
		t.Fatalf("stray ; must not error: %v", diags)
	}
	if n := warnCount(diags); n != 1 {
		t.Fatalf("stray ; warned %d times, want 1: %v", n, diags)
	}
	e, ok := f.Decls[0].(*ast.EmptyDecl)
	if !ok {
		t.Fatalf("got %T, want EmptyDecl kept visible", f.Decls[0])
	}
	if e.End() <= e.Pos() {
		t.Error("EmptyDecl span must be non-empty")
	}
	// Two in a row — the shape the SDK actually ships (`};;`) —
	// is two warnings, two EmptyDecls, still zero errors.
	_, f2, d2 := parse(t, "struct S { int x; };; int y;", 0)
	if errCount(d2) != 0 || warnCount(d2) != 1 {
		t.Fatalf("};; : errs=%d warns=%d %v", errCount(d2), warnCount(d2), d2)
	}
	if _, ok := f2.Decls[1].(*ast.EmptyDecl); !ok {
		t.Fatalf("second decl is %T, want EmptyDecl", f2.Decls[1])
	}
}
// A diagnostic sited at EOF must stay inside the file. EOF's Pos is the
// file's one-past-the-end position, and the widening that gives a
// zero-width token a column to underline has nowhere to go there: a span
// past it is a Pos the File will not convert, which is a panic in whoever
// renders the diagnostic rather than in whoever built it.
func TestSpansStayInsideTheFile(t *testing.T) {
	// Each of these runs out of tokens mid-production, so the error and
	// the nodes above it close at EOF.
	for _, src := range []string{"", "int f(", "int f(void) {", "int x = "} {
		unit, f, diags := parse(t, src, 0)
		limit := unit.Pos(unit.Size())

		if f.End() > limit {
			t.Errorf("%q: file span ends at %d, past the file's %d", src, f.End(), limit)
		}
		for _, d := range diags {
			if d.End > limit {
				t.Errorf("%q: %q ends at %d, past the file's %d", src, d.Message, d.End, limit)
			}
			// The renderer's own call, which is where this used to panic.
			unit.Raw(d.Pos, d.End)
		}
	}
}
