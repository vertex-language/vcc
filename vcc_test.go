package vcc_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vertex-language/ir/text"

	"github.com/vertex-language/vcc"
	"github.com/vertex-language/vcc/parser"
	"github.com/vertex-language/vcc/preprocessor"
	"github.com/vertex-language/vcc/token"
)

// hostOnly skips a test that has to produce a binary for this machine.
func hostOnly(t *testing.T) {
	t.Helper()
	if _, ok := vcc.HostTarget(); !ok {
		t.Skip("this host is not a target vcc models")
	}
}

// lines is a program's stdout with the host's line endings normalized.
//
// A C program that writes a newline to a text-mode stream writes CR LF on
// Windows, because that is what the C runtime does there and what every other
// program on the platform expects to read. The tests below are about what the
// program computed, not about which convention its stdout carries.
func lines(b []byte) string { return strings.ReplaceAll(string(b), "\r\n", "\n") }

func write(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o666); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildShorthand(t *testing.T) {
	hostOnly(t)
	dir := t.TempDir()
	src := write(t, dir, "hello.c", "#include <stdio.h>\nint main(void){printf(\"hi\\n\");return 0;}\n")
	out := filepath.Join(dir, "hello")

	if err := vcc.Build(out, src); err != nil {
		t.Fatalf("Build: %v", err)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("running the result: %v", err)
	}
	if lines(got) != "hi\n" {
		t.Errorf("output = %q, want %q", got, "hi\n")
	}
}

// TestRunExitError is the whole contract of the shorthand: stdout is the
// value, and a non-zero exit is an *exec.ExitError carrying the status.
func TestRunExitError(t *testing.T) {
	hostOnly(t)
	dir := t.TempDir()
	src := write(t, dir, "exits.c", "#include <stdio.h>\nint main(void){printf(\"out\\n\");return 3;}\n")

	out, err := vcc.Run(src)
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v, want *exec.ExitError", err)
	}
	if ee.ExitCode() != 3 {
		t.Errorf("exit = %d, want 3", ee.ExitCode())
	}
	if lines(out) != "out\n" {
		t.Errorf("output = %q, want %q", out, "out\n")
	}
}

// TestInMemory compiles source that never touches the filesystem and links the
// object without writing it either.
func TestInMemory(t *testing.T) {
	hostOnly(t)
	var c vcc.Compiler

	obj, diags, err := c.Object(vcc.Text("mem.c", []byte("#include <stdio.h>\nint main(void){printf(\"mem\\n\");return 0;}\n")))
	if err != nil {
		t.Fatalf("Object: %v", err)
	}
	if vcc.HasErrors(diags) {
		t.Fatalf("diagnostics: %v", diags)
	}
	if len(obj) == 0 {
		t.Fatal("Object returned no bytes")
	}

	prog, err := c.Program(vcc.BuildParams{Inputs: []vcc.Input{vcc.ObjectBytes("mem.o", obj)}})
	if err != nil {
		t.Fatalf("Program: %v", err)
	}
	defer prog.Close()

	got, err := prog.Command().Output()
	if err != nil {
		t.Fatalf("running the result: %v", err)
	}
	if lines(got) != "mem\n" {
		t.Errorf("output = %q, want %q", got, "mem\n")
	}

	path := prog.Path()
	if err := prog.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Close left %s behind", path)
	}
}

// TestCheckSitesDiagnostics is the reparse bridge's whole point: an error in a
// unit that included a header reports at the line the user wrote, in the file
// they wrote it in, not at a line the file only has after preprocessing.
func TestCheckSitesDiagnostics(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "hdr.h", "#define ONE 1\n")
	src := write(t, dir, "use.c", "#include \"hdr.h\"\nint main(void){return ONE + nope;}\n")

	var c vcc.Compiler
	diags, err := c.Check(vcc.File(src))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !vcc.HasErrors(diags) {
		t.Fatal("no error reported for an undeclared identifier")
	}
	d := diags[0]
	if d.Site.Origin == nil || d.Site.Origin.Name() != src {
		t.Fatalf("sited in %q, want %q", vcc.SiteString(d.Site), src)
	}
	if p := d.Site.Origin.File.Position(d.Site.Pos); p.Line != 2 {
		t.Errorf("reported line %d, want 2 — the position map did not map", p.Line)
	}
}

// TestBuildDiagnosticError checks the split every caller depends on: a wrong
// program is a *DiagnosticError, and nothing else is.
func TestBuildDiagnosticError(t *testing.T) {
	dir := t.TempDir()
	var c vcc.Compiler

	err := c.Build(vcc.BuildParams{
		Output: filepath.Join(dir, "out"),
		Inputs: []vcc.Input{vcc.Text("bad.c", []byte("int main(void){return zzz;}\n"))},
	})
	var de *vcc.DiagnosticError
	if !errors.As(err, &de) {
		t.Fatalf("err = %v, want *vcc.DiagnosticError", err)
	}
	if !strings.Contains(de.Error(), "zzz") {
		t.Errorf("Error() = %q, want it to name the identifier", de.Error())
	}

	if err := c.Build(vcc.BuildParams{Inputs: []vcc.Input{vcc.Text("a.c", nil)}}); err == nil {
		t.Error("Build with no output path returned nil")
	} else if errors.As(err, &de) {
		t.Error("a missing output path is an invocation error, not a diagnostic one")
	}
}

// TestPreprocessIdentity is the round trip --emit i promises: .i input comes
// back byte for byte, pragma lines and whitespace included.
func TestPreprocessIdentity(t *testing.T) {
	const src = "int x = 1;\n#pragma pack(1)\nint    y   =    2;\n"
	var c vcc.Compiler
	out, diags, err := c.Preprocess(vcc.Text("a.i", []byte(src)))
	if err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("diagnostics: %v", diags)
	}
	if string(out) != src {
		t.Errorf("Preprocess(.i) = %q, want it unchanged", out)
	}
}

func TestParseKeepsTheTree(t *testing.T) {
	var c vcc.Compiler
	tree, diags, err := c.Parse(vcc.Text("a.i", []byte("int a; int b;\n")), parser.DefaultMode)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer tree.Release()
	if len(diags) != 0 {
		t.Errorf("diagnostics: %v", diags)
	}
	if len(tree.Decls) != 2 {
		t.Errorf("%d decls, want 2", len(tree.Decls))
	}
	if tree.Unit == nil {
		t.Error("the tree carries no position space")
	}
}

// TestSourceIsWhatTheParserReads: the rung `vcc tokens` stops at.
func TestSource(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "d.h", "#define D 42\n")
	src := write(t, dir, "s.c", "#include \"d.h\"\nint x = D;\n")

	var c vcc.Compiler
	f, diags, err := c.Source(vcc.File(src))
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if vcc.HasErrors(diags) {
		t.Fatalf("diagnostics: %v", diags)
	}
	if !strings.Contains(string(f.Source()), "int x = 42") {
		t.Errorf("preprocessed unit = %q, want the macro expanded", f.Source())
	}
}

func TestTargets(t *testing.T) {
	names := vcc.Targets()
	if len(names) == 0 {
		t.Fatal("no targets")
	}
	for _, n := range names {
		tgt, ok := vcc.LookupTarget(n)
		if !ok {
			t.Fatalf("Targets lists %q and LookupTarget does not know it", n)
		}
		if tgt.Name() != n {
			t.Errorf("LookupTarget(%q).Name() = %q", n, tgt.Name())
		}
		if tgt.Model().SizeInt == 0 {
			t.Errorf("%s has no type model", n)
		}
		if len(tgt.Predefines()) == 0 {
			t.Errorf("%s predefines nothing", n)
		}
	}
	if _, ok := vcc.LookupTarget("frobnitz"); ok {
		t.Error("LookupTarget invented a target")
	}

	// Whether a modelled target can actually be built for is answered before
	// anything is compiled, so a caller finds out from Supports rather than
	// from a backend three phases down. Every target vcc models is buildable
	// today; the check is that each one says so, not that none refuses.
	for _, n := range vcc.Targets() {
		tgt, _ := vcc.LookupTarget(n)
		if err := tgt.Supports(); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
}

// TestHostInjection: a compiler given a Host reads nothing outside it, which
// is what makes a hermetic build hermetic.
type emptyHost struct{}

func (emptyHost) Getenv(string) string                  { return "" }
func (emptyHost) IsDir(string) bool                     { return false }
func (emptyHost) ReadDir(string) ([]string, error)      { return nil, errors.New("no directories") }
func (emptyHost) ReadFile(string) (string, error)       { return "", errors.New("no files") }
func (emptyHost) Run(string, ...string) (string, error) { return "", errors.New("no tools") }

func TestHostInjection(t *testing.T) {
	c := vcc.Compiler{Target: "x86_64-linux", Host: emptyHost{}}
	env, err := c.Env()
	if err != nil {
		t.Fatalf("Env: %v", err)
	}
	if len(env.Search) != 1 {
		t.Fatalf("search list = %d entries, want only the builtins", len(env.Search))
	}
	if !env.Hosted {
		t.Error("Hosted = false, want true")
	}
	if len(env.Predefines) == 0 {
		t.Error("no predefines")
	}
}

func TestUnknownTargetIsAnInvocationError(t *testing.T) {
	c := vcc.Compiler{Target: "frobnitz"}
	_, _, err := c.Object(vcc.Text("a.i", []byte("int main(void){return 0;}\n")))
	if err == nil {
		t.Fatal("no error for an unknown target")
	}
	var de *vcc.DiagnosticError
	if errors.As(err, &de) {
		t.Error("an unknown target is not a diagnostic")
	}
	if !strings.Contains(err.Error(), "frobnitz") {
		t.Errorf("error = %v, want it to name the target", err)
	}
}

// TestIRIsNeverNil: --emit vir prints a module even for input the front end
// rejected, so the module has to exist.
func TestIRIsNeverNil(t *testing.T) {
	var c vcc.Compiler
	mod, diags, err := c.IR(vcc.Text("bad.i", []byte("int main(void){return zzz;}\n")))
	if err != nil {
		t.Fatalf("IR: %v", err)
	}
	if mod == nil {
		t.Fatal("IR returned a nil module for a rejected tree")
	}
	if !vcc.HasErrors(diags) {
		t.Error("a rejected tree produced no error diagnostics")
	}
}

func TestOnDiagnostic(t *testing.T) {
	var seen []vcc.Diagnostic
	c := vcc.Compiler{OnDiagnostic: func(d vcc.Diagnostic) { seen = append(seen, d) }}

	diags, err := c.Check(vcc.Text("bad.i", []byte("int main(void){return zzz;}\n")))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(seen) != len(diags) {
		t.Errorf("hook saw %d diagnostics, Check returned %d", len(seen), len(diags))
	}
	if len(seen) == 0 || seen[0].Severity != token.Error {
		t.Errorf("hook saw %v, want the error", seen)
	}
}

// TestConcurrentObject is the promise the Compiler doc makes: a build system
// will drive one compiler from a worker pool, so this runs under -race.
func TestConcurrentObject(t *testing.T) {
	hostOnly(t)
	var c vcc.Compiler
	var wg sync.WaitGroup
	errs := make([]error, 16)
	sizes := make([]int, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			src := fmt.Sprintf("#include <stdio.h>\nint f%d(int x){return x+%d;}\nint main(void){printf(\"%%d\\n\", f%d(1));return 0;}\n", i, i, i)
			obj, diags, err := c.Object(vcc.Text(fmt.Sprintf("u%d.c", i), []byte(src)))
			if err == nil && vcc.HasErrors(diags) {
				err = fmt.Errorf("diagnostics: %v", diags)
			}
			errs[i], sizes[i] = err, len(obj)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("unit %d: %v", i, err)
		} else if sizes[i] == 0 {
			t.Errorf("unit %d: no object", i)
		}
	}
}

func TestDefineAndUndefine(t *testing.T) {
	c := vcc.Compiler{Defines: []preprocessor.Predefine{
		vcc.Define("WANTED", ""),
		vcc.Define("VALUE", "7"),
		vcc.Undefine("__CHAR_BIT__"),
	}}
	out, diags, err := c.Preprocess(vcc.Text("a.c", []byte("#ifdef WANTED\nint w = VALUE;\n#endif\n#ifdef __CHAR_BIT__\nint bits;\n#endif\n")))
	if err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if vcc.HasErrors(diags) {
		t.Fatalf("diagnostics: %v", diags)
	}
	got := string(out)
	if !strings.Contains(got, "int w = 7") {
		t.Errorf("output = %q, want the defines to have applied", got)
	}
	if strings.Contains(got, "int bits") {
		t.Errorf("output = %q, want -U to have removed a target predefine", got)
	}
}

// A .c file whose every line is inside a false conditional preprocesses to
// nothing. Phase 4 returns no tokens for it, which is not the same thing as
// not having run — reading it that way handed the parser the raw file, and
// the scanner then met a directive it does not know.
func TestPreprocessedToNothing(t *testing.T) {
	const src = "#ifdef NEVER\nint bad = NEVER;\n#endif\n"
	var c vcc.Compiler

	out, diags, err := c.Preprocess(vcc.Text("d.c", []byte(src)))
	if err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("diagnostics: %v", diags)
	}
	if strings.Contains(string(out), "#ifdef") {
		t.Errorf("Preprocess = %q, want the conditional gone", out)
	}

	if diags, err = c.Check(vcc.Text("d.c", []byte(src))); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("Check reported %v on a file that preprocesses to nothing", diags)
	}
}

// TestLinkAgainstArchive is -L and -l end to end: a real archive, resolved by
// name, linked, and run.
func TestLinkAgainstArchive(t *testing.T) {
	hostOnly(t)
	ar, err := exec.LookPath("ar")
	if err != nil {
		t.Skip("no ar on this machine to build an archive with")
	}
	dir := t.TempDir()
	libs := filepath.Join(dir, "libs")
	if err := os.Mkdir(libs, 0o777); err != nil {
		t.Fatal(err)
	}

	var c vcc.Compiler
	obj, diags, err := c.Object(vcc.Text("greet.c", []byte(
		"#include <stdio.h>\nvoid greet(void){printf(\"from libgreet\\n\");}\n")))
	if err != nil || vcc.HasErrors(diags) {
		t.Fatalf("Object: %v %v", err, diags)
	}
	objPath := filepath.Join(libs, "greet.o")
	if err := os.WriteFile(objPath, obj, 0o666); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(ar, "rcs", filepath.Join(libs, "libgreet.a"), objPath).CombinedOutput(); err != nil {
		t.Fatalf("ar: %v: %s", err, out)
	}

	main := write(t, dir, "main.c", "void greet(void);\nint main(void){greet();return 0;}\n")
	out := filepath.Join(dir, "prog")
	if err := c.Build(vcc.BuildParams{
		Output:  out,
		Inputs:  []vcc.Input{vcc.File(main)},
		LibDirs: []string{libs},
		Libs:    []string{"greet"},
	}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("running the result: %v", err)
	}
	if string(got) != "from libgreet\n" {
		t.Errorf("output = %q", got)
	}
}

// A library that does not exist is an invocation error, not a diagnostic.
func TestMissingLibrary(t *testing.T) {
	hostOnly(t)
	dir := t.TempDir()
	var c vcc.Compiler
	err := c.Build(vcc.BuildParams{
		Output: filepath.Join(dir, "prog"),
		Inputs: []vcc.Input{vcc.Text("a.c", []byte("int main(void){return 0;}\n"))},
		Libs:   []string{"definitelynotalibrary"},
	})
	if err == nil {
		t.Fatal("no error for a library that does not exist")
	}
	var de *vcc.DiagnosticError
	if errors.As(err, &de) {
		t.Error("a missing library is not a diagnostic")
	}
	if !strings.Contains(err.Error(), "cannot find -ldefinitelynotalibrary") {
		t.Errorf("error = %v", err)
	}
}

// Linkage has to survive the trip below vir, or a static function is a global
// symbol and two units that both include <stdio.h> do not link.
func TestFunctionLinkage(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"external", "int f(void){return 0;}\n", "export func @"},
		{"static", "static int f(void){return 0;}\nint g(void){return f();}\n", "internal func @"},

		// An inline definition that is emitted is emitted weak: a header is
		// where inline definitions live, every unit including it provides
		// one, and the linkers coalesce weak definitions rather than
		// refusing them.
		{"extern inline", "extern inline int f(void){return 0;}\n", "export weak func @"},

		// gcc's spelling of the same specifier. Discarding it is what turned
		// Darwin's __sputc into an ordinary definition in every unit.
		{"extern __inline", "extern __inline int f(void){return 0;}\n", "export weak func @"},
		{"static __inline", "static __inline int f(void){return 0;}\nint g(void){return f();}\n", "internal func @"},
	}
	host, ok := vcc.HostTarget()
	if !ok {
		t.Skip("this host is not a target vcc models")
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c vcc.Compiler
			mod, diags, err := c.IR(vcc.Text("a.i", []byte(tt.src)))
			if err != nil {
				t.Fatalf("IR: %v", err)
			}
			if vcc.HasErrors(diags) {
				t.Fatalf("diagnostics: %v", diags)
			}
			var b strings.Builder
			if err := text.Print(&b, mod); err != nil {
				t.Fatal(err)
			}
			// The symbol wears the target's prefix: "_f" on Mach-O.
			want := tt.want + host.SymbolPrefix() + "f"
			if !strings.Contains(b.String(), want) {
				t.Errorf("module does not contain %q:\n%s", want, b.String())
			}
		})
	}
}
