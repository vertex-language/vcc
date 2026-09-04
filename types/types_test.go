package types_test

import (
	"testing"

	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/parser"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// stub Resolver: no typedefs, no tags, constant = INT_LIT spelling.
type stub struct {
	unit    *token.File
	reports []string
}

func (s *stub) Typedef(*ast.Ident) types.Type { return types.Typ(types.Int) }
func (s *stub) Tag(ast.Expr) types.Type       { return &types.Record{Complete: true} }
func (s *stub) Eval(e ast.Expr) (int64, bool) {
	if l, ok := e.(*ast.BasicLit); ok && l.Kind == token.INT_LIT {
		var n int64
		for _, c := range s.unit.Slice(l.Lo, l.Hi) {
			n = n*10 + int64(c-'0')
		}
		return n, true
	}
	return 0, false
}
func (s *stub) Report(_ ast.Node, msg string) { s.reports = append(s.reports, msg) }

// TypeOf is typeof's resolver. These tests build types without an analyzer,
// so there is nothing here that could type an expression; the cases that
// need one live in the analyzer's tests.
func (s *stub) TypeOf(_ ast.Expr) types.Type { return nil }

// declOf parses one declaration and returns its pieces.
func declOf(t *testing.T, src string) (*token.File, *stub, ast.DeclSpecs, ast.Declarator) {
	t.Helper()
	unit := token.NewFile("a.c", []byte(src+"\n"))
	file, diags := parser.ParseFile(unit, parser.DefaultMode)
	for _, d := range diags {
		if d.Severity == token.Error {
			t.Fatalf("parse: %v", diags)
		}
	}
	g := file.Decls[0].(*ast.GenDecl)
	return unit, &stub{unit: unit}, g.Specs, g.List[0].Decl
}

func build(t *testing.T, src string) (types.Type, *stub) {
	t.Helper()
	unit, r, specs, d := declOf(t, src)
	sp := types.BuildSpecs(unit, specs, r)
	typ, _ := types.BuildDeclarator(unit, sp.Type, d, false, r)
	return typ, r
}

func TestMultisets(t *testing.T) {
	for src, want := range map[string]string{
		"unsigned long long int x;": "unsigned long long",
		"long int signed x;":        "long", // written order is free
		"char x;":                   "char",
	} {
		typ, r := build(t, src)
		if typ.String() != want || len(r.reports) != 0 {
			t.Errorf("%s = %s (%v), want %s", src, typ, r.reports, want)
		}
	}
	// The complex spellings are in the multiset table and are recognized as
	// what they are; BuildSpecs then declines them, because vcc defines
	// __STDC_NO_COMPLEX__. The diagnostic is the point: without it the type
	// reaches lower and fails there as an internal error.
	for _, src := range []string{
		"float _Complex x;", "double _Complex x;", "long double _Complex x;",
	} {
		typ, r := build(t, src)
		if len(r.reports) != 1 {
			t.Errorf("%s: reports = %v, want exactly one", src, r.reports)
		}
		if types.IsComplex(typ) {
			t.Errorf("%s = %s, want a type lower can emit", src, typ)
		}
	}
	for _, src := range []string{"long char x;", "signed void x;", "short long x;"} {
		_, r := build(t, src)
		if len(r.reports) != 1 {
			t.Errorf("%s: reports = %v, want exactly one", src, r.reports)
		}
	}
}

func TestDeclaratorInsideOut(t *testing.T) {
	// int *(*f[3])(void): f is array 3 of pointer to function
	// (no params) returning pointer to int.
	typ, r := build(t, "int *(*f[3])(void);")
	if len(r.reports) != 0 {
		t.Fatalf("reports: %v", r.reports)
	}
	arr, ok := typ.(*types.Array)
	if !ok || arr.Len != 3 {
		t.Fatalf("outer = %s, want array of 3", typ)
	}
	ptr := arr.Elem.(*types.Pointer)
	fn := ptr.Elem.(*types.Func)
	if !fn.Proto || len(fn.Params) != 0 {
		t.Fatalf("fn = %s, want (void) prototype", fn)
	}
	if fn.Ret.(*types.Pointer).Elem.Kind() != types.Int {
		t.Fatalf("ret = %s", fn.Ret)
	}
}

func TestParamAdjustment(t *testing.T) {
	typ, r := build(t, "void f(int a[const 10], int g(int));")
	if len(r.reports) != 0 {
		t.Fatalf("reports: %v", r.reports)
	}
	fn := typ.(*types.Func)
	p0 := fn.Params[0].Type
	if types.Unqualify(p0).Kind() != types.PointerKind || types.QualsOf(p0)&types.QConst == 0 {
		t.Errorf("param 0 = %s, want const-qualified pointer", p0)
	}
	if fn.Params[1].Type.Kind() != types.PointerKind {
		t.Errorf("param 1 = %s, want pointer to function", fn.Params[1].Type)
	}
}

func TestDerivationErrors(t *testing.T) {
	for src, want := range map[string]int{
		"int f(void)[3];":     1, // function returning array
		"typedef int T; void x;": 0, // parsed; 'void x' completeness is the analyzer's
		"int a[3](void);":     1, // array of functions
		"int a[0];":           0, // gcc's zero-length array, which headers use
		"int a[static 3];":    1, // static outside a parameter
	} {
		_, r := build(t, src)
		if len(r.reports) != want {
			t.Errorf("%s: reports = %v, want %d", src, r.reports, want)
		}
	}
}

func TestVoidParam(t *testing.T) {
	typ, r := build(t, "int f(void);")
	fn := typ.(*types.Func)
	if !fn.Proto || len(fn.Params) != 0 || len(r.reports) != 0 {
		t.Fatalf("f(void) = %s (%v)", fn, r.reports)
	}
	_, r = build(t, "int f(void, int);")
	if len(r.reports) == 0 {
		t.Fatal("f(void, int): want a report")
	}
}

func TestLayout(t *testing.T) {
	m := types.LP64()
	rec := &types.Record{Complete: true, Fields: []types.Field{
		{Name: "c", Type: types.Typ(types.Char)},
		{Name: "x", Type: types.Typ(types.Int)},
		{Name: "a", Type: types.Typ(types.Int), BitField: true, Width: 5},
		{Name: "b", Type: types.Typ(types.Int), BitField: true, Width: 30}, // crosses: new unit
	}}
	size, ok := m.Sizeof(rec)
	if !ok || size != 16 {
		t.Fatalf("sizeof = %d ok=%v, want 16", size, ok)
	}
	if sz, _ := m.Sizeof(&types.Pointer{Elem: types.Typ(types.Void)}); sz != 8 {
		t.Fatalf("sizeof(void*) = %d", sz)
	}
}