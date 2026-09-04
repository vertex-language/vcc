package ast

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vertex-language/vcc/token"
)

// tree hand-builds the AST for "int x = 1;" over a real token.File.
//
//	int x = 1;
//	0123456789
func tree(t *testing.T) (*token.File, *GenDecl) {
	t.Helper()
	f := token.NewFile("a.c", []byte("int x = 1;\n"))
	sp := func(lo, hi int) Span { return Span{f.Pos(lo), f.Pos(hi)} }
	return f, &GenDecl{
		Span:  sp(0, 10),
		Specs: DeclSpecs{&KeywordSpec{sp(0, 3), token.INT}},
		List: []*InitDeclarator{{
			Span:   sp(4, 9),
			Decl:   &NameDeclarator{sp(4, 5), &Ident{sp(4, 5)}},
			Assign: f.Pos(6),
			Init:   &BasicLit{sp(8, 9), token.INT_LIT},
		}},
		Semi: f.Pos(9),
	}
}

func TestWalkOrder(t *testing.T) {
	_, d := tree(t)
	var got []string
	Inspect(d, func(n Node) bool {
		got = append(got, nodeName(n))
		return true
	})
	want := []string{"GenDecl", "KeywordSpec", "InitDeclarator",
		"NameDeclarator", "Ident", "BasicLit"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("walk order = %v, want %v", got, want)
	}
}

func TestInspectSkipsSubtree(t *testing.T) {
	_, d := tree(t)
	var got []string
	Inspect(d, func(n Node) bool {
		got = append(got, nodeName(n))
		return nodeName(n) != "InitDeclarator" // skip its children
	})
	for _, name := range got {
		if name == "Ident" || name == "BasicLit" {
			t.Fatalf("subtree not skipped: %v", got)
		}
	}
}

func TestTypedNilsSkipped(t *testing.T) {
	_, d := tree(t)
	s := &IfStmt{
		Span: d.Span,
		Cond: (*BadExpr)(nil), // typed nil in an interface field
		Then: &EmptyStmt{Span: d.Span, Semi: 1},
		Else: nil,
	}
	var got []string
	Inspect(s, func(n Node) bool {
		got = append(got, nodeName(n))
		return true
	})
	want := "IfStmt EmptyStmt"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %q", got, want)
	}
}

func TestFuncDeclNameNotWalkedTwice(t *testing.T) {
	f := token.NewFile("a.c", []byte("int main(void) {}\n"))
	sp := func(lo, hi int) Span { return Span{f.Pos(lo), f.Pos(hi)} }
	name := &Ident{sp(4, 8)}
	fn := &FuncDecl{
		Span:  sp(0, 17),
		Specs: DeclSpecs{&KeywordSpec{sp(0, 3), token.INT}},
		Decl: &FuncDeclarator{
			Span:   sp(4, 14),
			Inner:  &NameDeclarator{sp(4, 8), name},
			Lparen: f.Pos(8),
			Params: []*ParamDecl{{
				Span:  sp(9, 13),
				Specs: DeclSpecs{&KeywordSpec{sp(9, 13), token.VOID}},
			}},
			Rparen: f.Pos(13),
		},
		Name: name, // alias, tagged ast:"-"
		Body: &CompoundStmt{Span: sp(15, 17), Lbrace: f.Pos(15), Rbrace: f.Pos(16)},
	}
	idents := 0
	Inspect(fn, func(n Node) bool {
		if _, ok := n.(*Ident); ok {
			idents++
		}
		return true
	})
	if idents != 1 {
		t.Fatalf("Ident visited %d times, want 1", idents)
	}
	if fn.Name.Name(f) != "main" {
		t.Fatalf("Name = %q", fn.Name.Name(f))
	}
}

func TestDeclName(t *testing.T) {
	f := token.NewFile("a.c", []byte("int *(*f[3])(void);\n"))
	sp := func(lo, hi int) Span { return Span{f.Pos(lo), f.Pos(hi)} }
	id := &Ident{sp(7, 8)}
	// *(*f[3])(void) read inside-out: f, array 3, pointer, parens, func, pointer
	var d Declarator = &PtrDeclarator{sp(4, 18), f.Pos(4), nil,
		&FuncDeclarator{Span: sp(5, 18),
			Inner: &ParenDeclarator{sp(5, 12), f.Pos(5),
				&PtrDeclarator{sp(6, 11), f.Pos(6), nil,
					&ArrayDeclarator{Span: sp(7, 11),
						Inner:  &NameDeclarator{sp(7, 8), id},
						Lbrack: f.Pos(8),
						Len:    &BasicLit{sp(9, 10), token.INT_LIT},
						Rbrack: f.Pos(10),
					}},
				f.Pos(11)},
			Lparen: f.Pos(12), Rparen: f.Pos(17)},
	}
	if got := d.DeclName(); got != id {
		t.Fatalf("DeclName = %v, want the inner ident", got)
	}
	if got := d.DeclName().Name(f); got != "f" {
		t.Fatalf("DeclName spelling = %q, want f", got)
	}
	abstract := &PtrDeclarator{sp(4, 5), f.Pos(4), nil, nil}
	if abstract.DeclName() != nil {
		t.Fatal("abstract declarator should have nil DeclName")
	}
}

func TestRelease(t *testing.T) {
	calls := 0
	file := &File{}
	file.Release() // no releaser: safe
	file.SetReleaser(releaserFunc(func() { calls++ }))
	file.Release()
	file.Release() // twice: safe
	if calls != 1 {
		t.Fatalf("releaser called %d times, want 1", calls)
	}
}

type releaserFunc func()

func (f releaserFunc) Release() { f() }

func TestFdump(t *testing.T) {
	f, d := tree(t)
	var b bytes.Buffer
	if err := Fdump(&b, f, d); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"GenDecl 1:1", "Ident 1:5 x", "BasicLit 1:9 INT_LIT 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("dump missing %q:\n%s", want, out)
		}
	}
}

func TestStringLitSegments(t *testing.T) {
	f := token.NewFile("a.c", []byte(`u8"a" "b"` + "\n"))
	s := &StringLit{
		Span: Span{f.Pos(0), f.Pos(9)},
		Segs: []Span{{f.Pos(0), f.Pos(5)}, {f.Pos(6), f.Pos(9)}},
	}
	// Segments are extents, not children: nothing to walk into.
	count := 0
	Inspect(s, func(Node) bool { count++; return true })
	if count != 1 {
		t.Fatalf("StringLit walked %d nodes, want 1", count)
	}
	if got := string(f.Slice(s.Segs[0].Lo, s.Segs[0].Hi)); got != `u8"a"` {
		t.Fatalf("segment 0 = %q", got)
	}
}