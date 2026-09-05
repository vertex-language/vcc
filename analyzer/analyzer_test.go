package analyzer

import (
	"strings"
	"testing"

	"github.com/vertex-language/vcc/parser"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

func check(t *testing.T, src string) (*Info, []token.Diagnostic) {
	t.Helper()
	unit := token.NewFile("a.c", []byte(src+"\n"))
	file, diags := parser.ParseFile(unit, parser.DefaultMode)
	for _, d := range diags {
		if d.Severity == token.Error {
			t.Fatalf("parse: %v", diags)
		}
	}
	return Check(unit, file, types.LP64(), nil)
}

func wantErrs(t *testing.T, src string, frags ...string) {
	t.Helper()
	_, diags := check(t, src)
	var errs []token.Diagnostic
	for _, d := range diags {
		if d.Severity == token.Error {
			errs = append(errs, d)
		}
	}
	if len(errs) != len(frags) {
		t.Fatalf("%q: got %d errors %v, want %d", src, len(errs), errs, len(frags))
	}
	for i, f := range frags {
		if !strings.Contains(errs[i].Message, f) {
			t.Errorf("%q: error %d = %q, want a mention of %q", src, i, errs[i].Message, f)
		}
	}
}

func TestDecodeIntTypes(t *testing.T) {
	m := types.LP64()
	silent := func(string) {}
	for text, want := range map[string]types.Kind{
		"1":          types.Int,
		"2147483648": types.Long, // decimal: skips uint (signed only)
		"0x80000000": types.UInt, // hex: unsigned steps allowed
		"1u":         types.UInt,
		"1l":         types.Long,
		"0x1Fu":      types.UInt,
	} {
		if v := DecodeIntConst(text, m, silent); v.Type.Kind() != want {
			t.Errorf("%s: type = %s, want kind %d", text, v.Type, want)
		}
	}
	if v := DecodeIntConst("0x10", m, silent); v.Value != 16 {
		t.Errorf("0x10 = %d", v.Value)
	}
	reported := 0
	DecodeIntConst("99999999999999999999999", m, func(string) { reported++ })
	if reported != 1 {
		t.Errorf("overflow reported %d times", reported)
	}
}

func TestDecodeCharAndString(t *testing.T) {
	m := types.LP64()
	silent := func(string) {}
	if v := DecodeCharConst(`'\n'`, m, silent); v.Value != 10 || v.Type.Kind() != types.Int {
		t.Errorf(`'\n' = %+v`, v)
	}
	if v := DecodeCharConst(`'ab'`, m, silent); v.Value != ('a'<<8)|'b' {
		t.Errorf("'ab' = %d", v.Value)
	}
	reported := 0
	DecodeCharConst(`'\777'`, m, func(string) { reported++ }) // 511 > unsigned char
	if reported != 1 {
		t.Errorf(`'\777' reported %d times, want 1`, reported)
	}
	// Escape range and NUL termination through the checker:
	wantErrs(t, `char s[] = "a\777b";`, "octal escape")
}

func TestStringConcatAndPrefixes(t *testing.T) {
	_, diags := check(t, `char a[] = u8"x" "y"; int n;`)
	for _, d := range diags {
		if d.Severity == token.Error {
			t.Fatalf("plain + u8 must combine cleanly: %v", diags)
		}
	}
	wantErrs(t, `int x = sizeof(u"x" U"y");`, "differing encoding prefixes")
}

func TestEnumValues(t *testing.T) {
	info, diags := check(t, "enum E { A, B = 5, C };")
	for _, d := range diags {
		if d.Severity == token.Error {
			t.Fatalf("diags: %v", diags)
		}
	}
	got := map[int64]bool{}
	for _, v := range info.Enums {
		got[v] = true
	}
	for _, want := range []int64{0, 5, 6} {
		if !got[want] {
			t.Fatalf("enum values = %v, want {0,5,6}", info.Enums)
		}
	}
	wantErrs(t, "enum E { A, A };", "redeclared")
}

// An enumerator past INT_MAX is outside §6.7.2.2p2, and gcc, clang and MSVC
// all widen the enumeration rather than refuse it — which is what makes
// <windows.h> openable, since wingdi.h writes thirty-four of them.
func TestEnumWidensPastInt(t *testing.T) {
	// The value is kept, not truncated, and the enumeration is unsigned:
	// 0x80000000 as an int would be negative and compare the wrong way.
	wantErrs(t, `enum E { A = 2147483647, B };
		_Static_assert(B == 2147483648u, "value");
		_Static_assert(B > A, "unsigned");
		_Static_assert(sizeof(enum E) == 4, "still four bytes");`)

	// A list that needs the sign and the range takes 64 bits.
	wantErrs(t, `enum W { N = -1, P = 4294967295 };
		_Static_assert(sizeof(enum W) == 8, "widened");
		_Static_assert(N < 0, "still signed");`)

	// Everything that fits stays int, which is every enumeration a
	// strictly conforming program can write.
	wantErrs(t, `enum S { X = -1, Y = 2147483647 };
		_Static_assert(sizeof(enum S) == 4, "int");
		_Static_assert(X < 0, "signed");`)
}

func TestEnumConstantsFoldEverywhere(t *testing.T) {
	// Enum constants feed array sizes: N+1 elements, not a VLA.
	wantErrs(t, "enum { N = 4 }; int a[N + 1]; _Static_assert(sizeof(int[N + 1]) == 20, \"sz\");")
}

func TestStaticAssert(t *testing.T) {
	wantErrs(t, `_Static_assert(1 == 2, "one is not two");`, "one is not two")
	wantErrs(t, `_Static_assert(sizeof(int) == 4, "lp64");`)
	wantErrs(t, "int n; _Static_assert(n, \"x\");", "constant expression")
}

func TestLabels(t *testing.T) {
	wantErrs(t, "void f(void) { goto out; out: ; }")
	wantErrs(t, "void f(void) { goto nowhere; }", "undefined label")
	wantErrs(t, "void f(void) { x: ; x: ; }", "duplicate label")
	wantErrs(t, "void f(int a) { case 1: ; }", "outside a switch")
	wantErrs(t, "void f(void) { break; }", "outside a loop")
	wantErrs(t, "void f(int a) { switch (a) { case 1: break; default: break; } }")
}

func TestBitFields(t *testing.T) {
	wantErrs(t, "struct S { int a : 3; unsigned b : 29; };")
	wantErrs(t, "struct S { int a : 33; };", "exceeds the width")
	wantErrs(t, "struct S { int a : 0; };", "nonzero width")
	wantErrs(t, "struct S { double d : 3; };", "bit-field has type")
	wantErrs(t, "struct S { int : 0; int a : 1; };") // unnamed zero-width: fine
}

func TestTags(t *testing.T) {
	wantErrs(t, "struct S { int x; }; struct S s;")
	wantErrs(t, "struct S { int x; }; union S u;", "different kind of tag",
		"has incomplete type") // u's type never completed
	wantErrs(t, "struct S { int x; }; struct S { int y; };", "redefinition")
	wantErrs(t, "void f(void) { struct S { int x; } s; } struct S t;",
		"has incomplete type") // inner tag doesn't leak out
}

func TestRedeclaration(t *testing.T) {
	wantErrs(t, "void f(void) { int x; int x; }", "redeclared in the same scope")
	wantErrs(t, "void f(void) { int x; { int x; } }") // shadowing is fine
	wantErrs(t, "int x; int x;")                      // file scope: tentative
	wantErrs(t, "typedef int T; void f(void) { float T; }")
	wantErrs(t, "int x; void f(void) { extern int x; }")
}

func TestVLAPlacement(t *testing.T) {
	wantErrs(t, "int n; int a[n];", "variably modified")
	wantErrs(t, "void f(int n) { int a[n]; }")
	wantErrs(t, "void f(int n) { static int a[n]; }", "variably modified")
}

func TestKRMatching(t *testing.T) {
	wantErrs(t, "int f(a, b) int a; int b; { return a + b; }")
	wantErrs(t, "int f(a) int a; int c; { return a; }", "not in the parameter list")
	// Undeclared K&R parameter defaults to int, no diagnostic.
	wantErrs(t, "int f(a, b) int a; { return b; }")
}

func TestIncompleteObjects(t *testing.T) {
	wantErrs(t, "struct S; struct S s;", "incomplete type")
	wantErrs(t, "struct S; extern struct S s;") // extern: deferred
	wantErrs(t, "int a[] = { 1, 2, 3 };")       // completed by init (arity later)
	wantErrs(t, "void v;", "incomplete type")
	wantErrs(t, "typedef int T; T x = 3;")
}

func TestStorageClasses(t *testing.T) {
	wantErrs(t, "register int x;", "cannot be auto or register")
	wantErrs(t, "typedef int T = 3;", "cannot be initialized")
	wantErrs(t, "_Thread_local register int x;", "may combine only", "cannot be auto or register")
	wantErrs(t, "inline int x;", "apply only to functions")
}
