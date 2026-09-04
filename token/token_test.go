package token

import (
	"bytes"
	"testing"
)

func TestKeywordCount(t *testing.T) {
	if n := int(std_keyword_end - keyword_beg - 1); n != 44 {
		t.Fatalf("got %d standard keywords, want 44", n)
	}
}

func TestLookup(t *testing.T) {
	for name, want := range map[string]Kind{
		"typedef": TYPEDEF, "_Thread_local": THREAD_LOCAL,
		"_Imaginary": IMAGINARY, "T": IDENT, "Auto": IDENT,
		"__int128": INT128, "__int128_t": IDENT, "": IDENT,
		"__auto_type": AUTO_TYPE,
	} {
		if got := Lookup(name); got != want {
			t.Errorf("Lookup(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestPrecedence(t *testing.T) {
	order := []Kind{COMMA, LOR, LAND, OR, XOR, AND, EQL, LSS, SHL, ADD, MUL}
	for i := 1; i < len(order); i++ {
		if order[i-1].Precedence() >= order[i].Precedence() {
			t.Errorf("%v (%d) should bind looser than %v (%d)",
				order[i-1], order[i-1].Precedence(), order[i], order[i].Precedence())
		}
	}
	for _, k := range []Kind{ASSIGN, QUESTION, INC, IDENT, NOT} {
		if k.Precedence() != LowestPrec {
			t.Errorf("%v.Precedence() = %d, want LowestPrec", k, k.Precedence())
		}
	}
}

func TestFastPath(t *testing.T) {
	f := NewFile("a.c", []byte("int x = 1;\n"))
	if f.rawLo != nil {
		t.Fatal("expected fast path (nil mapping)")
	}
	if got := string(f.Slice(f.Pos(0), f.Pos(3))); got != "int" {
		t.Errorf("Slice = %q", got)
	}
	if got := string(f.Raw(f.Pos(0), f.Pos(3))); got != "int" {
		t.Errorf("Raw = %q", got)
	}
	if len(f.Diagnostics()) != 0 {
		t.Errorf("unexpected diagnostics: %v", f.Diagnostics())
	}
}

func TestSplice(t *testing.T) {
	f := NewFile("a.c", []byte("in\\\nt x;\n"))
	if got := string(f.Text()); got != "int x;\n" {
		t.Fatalf("Text = %q", got)
	}
	if got := string(f.Slice(f.Pos(0), f.Pos(3))); got != "int" {
		t.Errorf("Slice = %q", got)
	}
	if got := string(f.Raw(f.Pos(0), f.Pos(3))); got != "in\\\nt" {
		t.Errorf("Raw = %q, want the splice widened in", got)
	}
	p := f.Position(f.Pos(2)) // the 't'
	if p.Line != 2 || p.Column != 1 {
		t.Errorf("Position of t = %d:%d, want 2:1", p.Line, p.Column)
	}
	if len(f.Diagnostics()) != 0 {
		t.Errorf("unexpected diagnostics: %v", f.Diagnostics())
	}
}

func TestTrigraph(t *testing.T) {
	f := NewFile("a.c", []byte("??<\n"))
	if got := string(f.Text()); got != "{\n" {
		t.Fatalf("Text = %q", got)
	}
	if got := string(f.Raw(f.Pos(0), f.Pos(1))); got != "??<" {
		t.Errorf("Raw = %q, want the whole trigraph", got)
	}
	ds := f.Diagnostics()
	if len(ds) != 1 || ds[0].Severity != Warn {
		t.Fatalf("want one Warn diagnostic, got %v", ds)
	}
}

func TestTrigraphSplice(t *testing.T) {
	// ??/ becomes backslash in phase 1, then splices in phase 2.
	f := NewFile("a.c", []byte("in??/\nt;\n"))
	if got := string(f.Text()); got != "int;\n" {
		t.Fatalf("Text = %q", got)
	}
}


func TestBetween(t *testing.T) {
	f := NewFile("a.c", []byte("a \\\n b;\n"))
	// tokens: 'a' at [0,1), 'b' at [2,3) in translated text "a  b;\n"
	prev := Token{Kind: IDENT, Pos: f.Pos(0), End: f.Pos(1)}
	next := Token{Kind: IDENT, Pos: f.Pos(3), End: f.Pos(4)}
	if got := f.Between(prev, next); !bytes.Equal(got, []byte(" \\\n ")) {
		t.Errorf("Between = %q, want the splice kept as trivia", got)
	}
}

func TestSortDiagnostics(t *testing.T) {
	ds := []Diagnostic{
		{Pos: 5, End: 6, Message: "b"},
		{Pos: 1, End: 3, Message: "z"},
		{Pos: 5, End: 6, Message: "a"},
		{Pos: 1, End: 2, Message: "y"},
	}
	SortDiagnostics(ds)
	want := []string{"y", "z", "a", "b"}
	for i, m := range want {
		if ds[i].Message != m {
			t.Fatalf("order = %v", ds)
		}
	}
}
// An extension spelling of a keyword resolves to that keyword, which is what
// makes it the same operator rather than a name to be discarded.
func TestKeywordAliases(t *testing.T) {
	for spelling, want := range map[string]Kind{
		"__alignof":   ALIGNOF,
		"__alignof__": ALIGNOF,
		"__thread":    THREAD_LOCAL,

		// The keywords themselves are unaffected, and an ordinary
		// identifier that merely looks like one is still an identifier.
		"_Alignof":      ALIGNOF,
		"_Thread_local": THREAD_LOCAL,
		"__alignofx":    IDENT,
		"thread":        IDENT,
	} {
		if got := Lookup(spelling); got != want {
			t.Errorf("Lookup(%q) = %v, want %v", spelling, got, want)
		}
	}
}
