package scanner

import (
	"testing"

	"github.com/vertex-language/vcc/token"
)

func scan(t *testing.T, src string, mode Mode) ([]token.Token, []token.Diagnostic) {
	t.Helper()
	return Scan(token.NewFile("a.c", []byte(src+"\n")), mode)
}

func wantKinds(t *testing.T, src string, want ...token.Kind) []token.Diagnostic {
	t.Helper()
	toks, diags := scan(t, src, 0)
	want = append(want, token.EOF)
	if len(toks) != len(want) {
		t.Fatalf("%q: got %d tokens %v, want %d", src, len(toks), toks, len(want))
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Fatalf("%q: token %d = %v, want %v", src, i, toks[i].Kind, k)
		}
	}
	return diags
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

func warnCount(ds []token.Diagnostic) int {
	n := 0
	for _, d := range ds {
		if d.Severity == token.Warn {
			n++
		}
	}
	return n
}

func TestMaximalMunch(t *testing.T) {
	wantKinds(t, "a+++b", token.IDENT, token.INC, token.ADD, token.IDENT)
	wantKinds(t, "a+++++b", token.IDENT, token.INC, token.INC, token.ADD, token.IDENT)
	wantKinds(t, "..", token.PERIOD, token.PERIOD)
	wantKinds(t, "...", token.ELLIPSIS)
	wantKinds(t, "a<<=b", token.IDENT, token.SHL_ASSIGN, token.IDENT)
	wantKinds(t, "a>>b", token.IDENT, token.SHR, token.IDENT)
}

func TestKeywordsAndIdents(t *testing.T) {
	wantKinds(t, "typedef T _Bool _bool",
		token.TYPEDEF, token.IDENT, token.BOOL, token.IDENT)
}

func TestDigraphs(t *testing.T) {
	toks, diags := scan(t, "<: :> <% %> a %: b", 0)
	want := []token.Kind{token.LBRACK, token.RBRACK, token.LBRACE, token.RBRACE,
		token.IDENT, token.HASH, token.IDENT, token.EOF}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Fatalf("token %d = %v, want %v", i, toks[i].Kind, k)
		}
	}
	for _, i := range []int{0, 1, 2, 3, 5} {
		if !toks[i].Flags.Has(token.FlagDigraph) {
			t.Errorf("token %d (%v): FlagDigraph not set", i, toks[i].Kind)
		}
	}
	if len(diags) != 0 {
		t.Errorf("unexpected diagnostics: %v", diags)
	}
}

func TestNumbers(t *testing.T) {
	cases := []struct {
		src   string
		kind  token.Kind
		diags int
	}{
		{"0x1Fu", token.INT_LIT, 0},
		{"0779", token.INT_LIT, 1},
		{"0779.5", token.FLOAT_LIT, 0}, // decimal float, not octal
		{"0x1.8", token.FLOAT_LIT, 1},  // missing binary exponent
		{"0x1.8p3", token.FLOAT_LIT, 0},
		{"0x1p+4f", token.FLOAT_LIT, 0},
		{"1e+5", token.FLOAT_LIT, 0},
		{".5", token.FLOAT_LIT, 0},
		{"1e", token.FLOAT_LIT, 1},
		{"1ul", token.INT_LIT, 0},
		{"2llu", token.INT_LIT, 0},
		{"3ULL", token.INT_LIT, 0},
		{"4lul", token.INT_LIT, 1},
		{"5lL", token.INT_LIT, 1},
	}
	for _, c := range cases {
		toks, diags := scan(t, c.src, 0)
		if toks[0].Kind != c.kind {
			t.Errorf("%q: kind = %v, want %v", c.src, toks[0].Kind, c.kind)
		}
		if len(toks) != 2 { // literal + EOF: one run, one token
			t.Errorf("%q: %d tokens, want 2", c.src, len(toks))
		}
		if n := errCount(diags); n != c.diags {
			t.Errorf("%q: %d error diagnostics %v, want %d", c.src, n, diags, c.diags)
		}
	}
}

func TestLiteralsStayUndecoded(t *testing.T) {
	f := token.NewFile("a.c", []byte("0x1Fu\n"))
	toks, _ := Scan(f, 0)
	if got := string(f.Slice(toks[0].Pos, toks[0].End)); got != "0x1Fu" {
		t.Errorf("literal span = %q, want the five raw bytes", got)
	}
}

func TestDigitSeparatorIsNotC11(t *testing.T) {
	diags := wantKinds(t, "1_024", token.INT_LIT, token.IDENT)
	if errCount(diags) != 0 {
		t.Errorf("unexpected diagnostics: %v", diags)
	}
}

func TestCharConstants(t *testing.T) {
	if d := wantKinds(t, "''", token.CHAR_LIT); errCount(d) != 1 {
		t.Errorf("'': %v, want one diagnostic", d)
	}
	if d := wantKinds(t, "'ab'", token.CHAR_LIT); errCount(d) != 0 {
		t.Errorf("'ab' should scan clean: %v", d)
	}
	if d := wantKinds(t, `'\q'`, token.CHAR_LIT); errCount(d) != 1 {
		t.Errorf(`'\q': %v, want one diagnostic`, d)
	}
	if d := wantKinds(t, `L'a'`, token.CHAR_LIT); errCount(d) != 0 {
		t.Errorf("L'a': %v", d)
	}
}

func TestStrings(t *testing.T) {
	// Adjacent literals are NOT concatenated: phase 6 is above us.
	wantKinds(t, `"a" "b"`, token.STRING_LIT, token.STRING_LIT)
	wantKinds(t, `u8"x"`, token.STRING_LIT)

	// A raw newline ends the literal: one report, token still emitted.
	toks, diags := scan(t, "\"abc\nx", 0)
	if toks[0].Kind != token.STRING_LIT || toks[1].Kind != token.IDENT {
		t.Fatalf("tokens = %v", toks)
	}
	if errCount(diags) != 1 {
		t.Errorf("diagnostics = %v, want exactly one", diags)
	}
}

func TestDirectiveLinesAreTrivia(t *testing.T) {
	toks, diags := scan(t, "# 1 \"a.c\"\nint x;\n# 2 \"a.c\"\n%: 3", 0)
	wantK := []token.Kind{token.INT, token.IDENT, token.SEMI, token.EOF}
	if len(toks) != len(wantK) {
		t.Fatalf("tokens = %v", toks)
	}
	for i, k := range wantK {
		if toks[i].Kind != k {
			t.Fatalf("token %d = %v, want %v", i, toks[i].Kind, k)
		}
	}
	if warnCount(diags) != 1 {
		t.Errorf("want the directive report once per file, got %v", diags)
	}
}

func TestBracketStack(t *testing.T) {
	for _, c := range []struct {
		src  string
		want int
	}{
		{"a)", 1},     // unmatched closer, once
		{"(]", 1},     // mismatch blames the opener, then quiet
		{"(] ) }", 1}, // ... and stays quiet
		{"((", 1},     // EOF: the innermost opener
		{"({[]})", 0},
		{"<: :>", 0}, // digraphs participate as their canonical kinds
	} {
		_, diags := scan(t, c.src, 0)
		if n := errCount(diags); n != c.want {
			t.Errorf("%q: %d error diagnostics %v, want %d", c.src, n, diags, c.want)
		}
	}
}

func TestFlags(t *testing.T) {
	toks, _ := scan(t, "a+ b\nc", 0)
	// a + b c EOF
	if !toks[1].Flags.Has(token.FlagAdjacent) {
		t.Error("'+' should be FlagAdjacent to 'a'")
	}
	if toks[2].Flags.Has(token.FlagAdjacent) {
		t.Error("'b' is separated by a space")
	}
	if !toks[3].Flags.Has(token.FlagNLBefore) {
		t.Error("'c' should have FlagNLBefore")
	}
	if toks[1].Flags.Has(token.FlagNLBefore) {
		t.Error("'+' should not have FlagNLBefore")
	}
}

func TestComments(t *testing.T) {
	toks, _ := scan(t, "/*x*/a//y", ScanComments)
	want := []token.Kind{token.COMMENT, token.IDENT, token.COMMENT, token.EOF}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Fatalf("token %d = %v, want %v", i, toks[i].Kind, k)
		}
	}
	toks, _ = scan(t, "/*x*/a//y", 0)
	if len(toks) != 2 || toks[0].Kind != token.IDENT {
		t.Fatalf("without ScanComments: %v", toks)
	}
	// Unterminated block comment: one diagnostic, runs to EOF.
	_, diags := scan(t, "/* open", 0)
	if errCount(diags) != 1 {
		t.Errorf("unterminated /*: %v", diags)
	}
}

func TestEOFToken(t *testing.T) {
	f := token.NewFile("a.c", []byte("x;\n"))
	toks, _ := Scan(f, 0)
	last := toks[len(toks)-1]
	if last.Kind != token.EOF || last.Pos != last.End || last.Pos != f.Pos(f.Size()) {
		t.Errorf("EOF token = %+v, want zero-width at Size()", last)
	}
}

func TestNoFinalNewlineScansIdentically(t *testing.T) {
	// EOF terminates the final line: same tokens, same flags, same
	// diagnostics, with or without the trailing newline. NewFile no
	// longer reports the missing newline — §5.1.1.2 leaves it
	// undefined and vcc defines it, because token-stream inclusion
	// makes the textual-fusion hazard the rule guards against
	// impossible. Covers the last-line cases that could plausibly
	// differ: a plain token, a // comment, a run to EOF, and a
	// directive line under ScanPP.
	for _, c := range []struct {
		src  string
		mode Mode
	}{
		{"int x;", 0},
		{"int x; // trailing", 0},
		{"int x; /* open", 0}, // unterminated /*: same one report both ways
		{"#define X 1", ScanPP},
		{"#if 1", ScanPP},
	} {
		a, ad := Scan(token.NewFile("a.c", []byte(c.src)), c.mode)
		b, bd := Scan(token.NewFile("a.c", []byte(c.src+"\n")), c.mode)
		if len(a) != len(b) {
			t.Fatalf("%q (mode %d): %d tokens without newline, %d with",
				c.src, c.mode, len(a), len(b))
		}
		for i := range a {
			if a[i].Kind != b[i].Kind {
				t.Errorf("%q (mode %d): token %d = %v without newline, %v with",
					c.src, c.mode, i, a[i].Kind, b[i].Kind)
			}
			if a[i].Kind != token.EOF && a[i].Flags != b[i].Flags {
				t.Errorf("%q (mode %d): token %d flags = %v without newline, %v with",
					c.src, c.mode, i, a[i].Flags, b[i].Flags)
			}
		}
		if errCount(ad) != errCount(bd) || warnCount(ad) != warnCount(bd) {
			t.Errorf("%q (mode %d): diagnostics differ: %v without newline, %v with",
				c.src, c.mode, ad, bd)
		}
	}
}

// ---- ScanPP: scanning source rather than preprocessed source ----

func TestScanPPDirectiveIsAToken(t *testing.T) {
	toks, diags := scan(t, "#define X 1\nint x = X;", ScanPP)

	if toks[0].Kind != token.HASH {
		t.Fatalf("token 0 = %v, want HASH", toks[0].Kind)
	}
	// The '#' opens a logical line even though nothing precedes it. This is
	// what nlBefore's ScanPP seed is for, and the preprocessor's whole
	// directive-vs-punctuator test rests on it.
	if !toks[0].Flags.Has(token.FlagNLBefore) {
		t.Error("a '#' in column 1 of line 1 opens a logical line")
	}
	if toks[1].Kind != token.IDENT {
		t.Errorf("token 1 = %v, want the identifier 'define'", toks[1].Kind)
	}
	if !toks[1].Flags.Has(token.FlagAdjacent) {
		t.Error("'define' is adjacent to '#'")
	}
	if warnCount(diags) != 0 {
		t.Errorf("a directive in .c input is not a mistake: %v", diags)
	}

	// Without ScanPP the same input is trivia plus the once-per-file report.
	toks, diags = scan(t, "#define X 1\nint x = X;", 0)
	if toks[0].Kind != token.INT {
		t.Fatalf("without ScanPP, token 0 = %v, want INT", toks[0].Kind)
	}
	if warnCount(diags) != 1 {
		t.Errorf("want the directive report once: %v", diags)
	}
}

func TestScanPPDigraphDirective(t *testing.T) {
	toks, _ := scan(t, "%:define X 1", ScanPP)
	if toks[0].Kind != token.HASH || !toks[0].Flags.Has(token.FlagDigraph) {
		t.Fatalf("token 0 = %v flags %v, want HASH with FlagDigraph",
			toks[0].Kind, toks[0].Flags)
	}
	if !toks[0].Flags.Has(token.FlagNLBefore) {
		t.Error("'%:' opening a line is a directive too")
	}
}

func TestScanPPHashHashSurvives(t *testing.T) {
	// '##' mid-line is a punctuator in both modes; ScanPP must not swallow it
	// along with the directive line, because subst() needs it.
	for _, mode := range []Mode{0, ScanPP} {
		toks, _ := scan(t, "a ## b %:%: c", mode)
		if toks[1].Kind != token.HASHHASH {
			t.Errorf("mode %d: token 1 = %v, want HASHHASH", mode, toks[1].Kind)
		}
		if toks[3].Kind != token.HASHHASH || !toks[3].Flags.Has(token.FlagDigraph) {
			t.Errorf("mode %d: token 3 = %v, want digraph HASHHASH", mode, toks[3].Kind)
		}
	}
}

func TestScanPPDoesNotBalanceBrackets(t *testing.T) {
	// Real headers define brackets as macros. That is not an error, and a
	// header is not a translation unit — bracket balance is a claim about
	// preprocessed source.
	for _, src := range []string{
		"#define BEGIN {\n#define END }",
		"#define OPEN (",
		"#if 0\n}\n#endif",
	} {
		if _, diags := scan(t, src, ScanPP); errCount(diags) != 0 {
			t.Errorf("%q under ScanPP: %v, want none", src, diags)
		}
	}
	// Without ScanPP the stack still reports, unchanged.
	if _, diags := scan(t, "int f(void) {", 0); errCount(diags) != 1 {
		t.Errorf("without ScanPP the stack still reports: %v", diags)
	}
}

func TestScanPPDefersMalformedLiterals(t *testing.T) {
	// These are deferred by phase 4's skipped-range filter. 0779 is a valid
	// pp-number and only fails when converted to a constant token.
	for _, src := range []string{"int a[0779];", "int a[0x1.8];", `char c = '\q';`} {
		if _, diags := scan(t, src, ScanPP); errCount(diags) != 0 {
			t.Errorf("%q under ScanPP: %v, want zero", src, diags)
		}
	}
}

func TestScanPPPPNumberIsOneToken(t *testing.T) {
	// The pp-number rule: one run of digits, letters, '.', and exponent
	// signs is one token, whatever it classifies as. Phase 4 needs the span
	// intact even when the classification is a diagnosis.
	toks, _ := scan(t, "0779", ScanPP)
	if len(toks) != 2 || toks[0].Kind != token.INT_LIT {
		t.Fatalf("tokens = %v, want one INT_LIT and EOF", toks)
	}
	f := token.NewFile("a.c", []byte("0x1p+4f\n"))
	toks, _ = Scan(f, ScanPP)
	if got := string(f.Slice(toks[0].Pos, toks[0].End)); got != "0x1p+4f" {
		t.Errorf("pp-number span = %q, want the whole run", got)
	}
}

func TestScanPPLineStructure(t *testing.T) {
	// takeLine finds the end of a logical line by FlagNLBefore, and phase 2
	// has already spliced backslash-newlines away — so a continued directive
	// is one line, which is what makes multi-line #defines work.
	toks, _ := scan(t, "#define M(a) \\\n  ((a)+1)\nint x;", ScanPP)
	nl := 0
	for _, tk := range toks {
		if tk.Kind != token.EOF && tk.Flags.Has(token.FlagNLBefore) {
			nl++
		}
	}
	if nl != 2 { // the '#', and the 'int' after the directive
		t.Errorf("%d line-opening tokens, want 2 (the '#', and the 'int' after the directive)", nl)
	}
}

// The last alternative of §6.4p1's pp-token grammar — "each non-white-space
// character that cannot be one of the above" — is a preprocessing token like
// any other. A backslash outside a UCN is one, and so is an @.
//
// What it is not is a token: §6.4p1's constraint on the conversion is phase
// 7's, so the diagnostic belongs where the character survives to and not
// where it was written. The Windows SDK's DriverSpecs.h writes a `param\t`
// into a macro replacement list that a Win32 program never expands, and a
// compiler that reports it there cannot open <windows.h>.
func TestScanPPDefersStrayCharacters(t *testing.T) {
	for _, src := range []string{
		`#define NEVER(a) f(#a, a\t)`,
		"#define AT @",
		"#if 0\n@\n#endif",
	} {
		toks, diags := scan(t, src, ScanPP)
		if errCount(diags) != 0 {
			t.Errorf("%q under ScanPP: %v, want zero", src, diags)
		}
		// Quiet, but present: phase 4 has to carry the token, or expanding
		// the macro would lose what phase 7 is meant to reject.
		found := false
		for _, tk := range toks {
			if tk.Kind == token.ILLEGAL {
				found = true
			}
		}
		if !found {
			t.Errorf("%q under ScanPP: no ILLEGAL token, want the pp-token kept", src)
		}
	}
}

// Phase 7 is where it is rejected, in both spellings.
func TestStrayCharactersReportAtPhase7(t *testing.T) {
	for _, src := range []string{`int x = a\t;`, "int x = @;"} {
		if _, diags := scan(t, src, 0); errCount(diags) != 1 {
			t.Errorf("%q: %d errors %v, want one", src, errCount(diags), diags)
		}
	}
}
