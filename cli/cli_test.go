package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vertex-language/vcc"
)

// image is where a -o without an extension actually lands. A Windows program
// is called .exe, and vcc appends it for the same reason MinGW gcc does: cmd,
// Explorer and Go's own exec all resolve an extensionless path by trying the
// extensions in %PATHEXT% and nothing else.
func image(path string) string {
	if runtime.GOOS == "windows" {
		return path + ".exe"
	}
	return path
}

// lines is a program's stdout with the host's line endings normalized: a C
// program writing a newline to a text-mode stream writes CR LF on Windows.
func lines(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// The command line's own rules, which the C suite cannot see: it runs the
// binary on programs that compile, and proves the compiler. What is left over
// is everything a wrapper decides — where an artifact lands, what -o means
// with two inputs, which exit code a verb returns — and Run takes its writers
// as arguments precisely so it can be tested here.

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func write(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o666); err != nil {
		t.Fatal(err)
	}
	return path
}

func hostOnly(t *testing.T) {
	t.Helper()
	if _, ok := vcc.HostTarget(); !ok {
		t.Skip("this host is not a target vcc models")
	}
}

func TestUsageAndVerbs(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		code   int
		stdout string // substring, if any
		stderr string // substring, if any
	}{
		{"no arguments", nil, exitUsage, "", "Usage:"},
		{"help", []string{"help"}, exitOK, "the Vertex C Compiler", ""},
		{"unknown verb", []string{"frobnicate"}, exitUsage, "", `unknown command "frobnicate"`},
		{"fmt is not yet", []string{"fmt"}, exitUsage, "", "not yet"},
		{"env takes no files", []string{"env", "a.c"}, exitUsage, "", "takes no files"},
		{"ast takes one file", []string{"ast", "a.c", "b.c"}, exitUsage, "", "one file at a time"},
		{"tokens takes one file", []string{"tokens", "a.c", "b.c"}, exitUsage, "", "one file at a time"},
		{"unknown target", []string{"check", "-target", "frobnitz", "a.c"}, exitUsage, "", `unknown target "frobnitz"`},
		{"unknown emit", []string{"build", "--emit", "wat", "a.c"}, exitUsage, "", `unknown --emit "wat"`},
		{"emit asm says why not", []string{"build", "--emit", "asm", "a.c"}, exitUsage, "", "encoders"},
		{"object needs a path", []string{"build", "--emit", "obj", "-o", "-", "a.c"}, exitUsage, "", "needs a path"},
		{"executable needs a path", []string{"build", "-o", "-", "a.c"}, exitUsage, "", "needs a path"},
		{"one artifact per input", []string{"build", "--emit", "obj", "-o", "x.o", "a.c", "b.c"}, exitUsage, "", "takes one input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != tt.code {
				t.Errorf("exit = %d, want %d (stderr: %s)", code, tt.code, stderr)
			}
			if tt.stdout != "" && !strings.Contains(stdout, tt.stdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout, tt.stdout)
			}
			if tt.stderr != "" && !strings.Contains(stderr, tt.stderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.stderr)
			}
		})
	}
}

func TestCheckExitCodes(t *testing.T) {
	dir := t.TempDir()
	good := write(t, dir, "good.c", "int main(void){return 0;}\n")
	bad := write(t, dir, "bad.c", "int main(void){return zzz;}\n")
	missing := filepath.Join(dir, "nope.c")

	if code, _, stderr := run(t, "check", good); code != exitOK {
		t.Errorf("a program with no errors exited %d: %s", code, stderr)
	}
	code, _, stderr := run(t, "check", bad)
	if code != exitDiags {
		t.Errorf("a program with errors exited %d, want %d", code, exitDiags)
	}
	if !strings.Contains(stderr, "zzz") || !strings.Contains(stderr, "^") {
		t.Errorf("stderr = %q, want the identifier and a caret", stderr)
	}
	if code, _, _ := run(t, "check", missing); code != exitUsage {
		t.Errorf("an unreadable file exited %d, want %d", code, exitUsage)
	}
	// The worst of several inputs is the exit code.
	if code, _, _ := run(t, "check", good, bad); code != exitDiags {
		t.Errorf("exit = %d, want %d", code, exitDiags)
	}
}

func TestEmitToStdout(t *testing.T) {
	dir := t.TempDir()
	src := write(t, dir, "a.i", "int x = 1;\n")

	code, stdout, stderr := run(t, "build", "--emit", "i", "-o", "-", src)
	if code != exitOK {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if stdout != "int x = 1;\n" {
		t.Errorf("stdout = %q, want the input unchanged", stdout)
	}

	code, stdout, stderr = run(t, "build", "--emit", "vir", "-o", "-", src)
	if code != exitOK {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "x") {
		t.Errorf("stdout = %q, want a module", stdout)
	}
}

// Without -o, each input gets its own artifact, named for the input and
// written to the working directory — where cc -c puts one.
func TestOneArtifactPerInput(t *testing.T) {
	hostOnly(t)
	dir := t.TempDir()
	write(t, dir, "a.c", "int a(void){return 1;}\n")
	write(t, dir, "b.c", "int b(void){return 2;}\n")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	if code, _, stderr := run(t, "build", "--emit", "obj", "a.c", "b.c"); code != exitOK {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	for _, name := range []string{"a.o", "b.o"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestBuildAndRun(t *testing.T) {
	hostOnly(t)
	dir := t.TempDir()
	src := write(t, dir, "hello.c", "#include <stdio.h>\nint main(int argc, char **argv){printf(\"argc=%d\\n\", argc);return argc == 1 ? 0 : 4;}\n")
	out := filepath.Join(dir, "hello")

	if code, _, stderr := run(t, "build", "-o", out, src); code != exitOK {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	if _, err := os.Stat(image(out)); err != nil {
		t.Fatalf("no executable: %v", err)
	}

	code, stdout, stderr := run(t, "run", src)
	if code != exitOK {
		t.Fatalf("run exited %d: %s", code, stderr)
	}
	if lines(stdout) != "argc=1\n" {
		t.Errorf("stdout = %q", stdout)
	}

	// The program's exit code is the answer, not vcc's.
	if code, _, _ := run(t, "run", src, "--", "one", "two"); code != 4 {
		t.Errorf("run exited %d, want the program's 4", code)
	}
}

// A build for another target links; running one is this machine's business.
func TestRunRefusesAnotherTarget(t *testing.T) {
	dir := t.TempDir()
	src := write(t, dir, "a.c", "int main(void){return 0;}\n")

	other := "x86_64-linux"
	if host, ok := vcc.HostTarget(); ok && host.Name() == other {
		other = "aarch64-linux"
	}
	code, _, stderr := run(t, "run", "-target", other, src)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "not this machine") {
		t.Errorf("stderr = %q", stderr)
	}
}

// -l reaches the link, and its failure is an invocation error.
func TestLibraryFlags(t *testing.T) {
	hostOnly(t)
	dir := t.TempDir()
	src := write(t, dir, "a.c", "int main(void){return 0;}\n")

	code, _, stderr := run(t, "build", "-o", filepath.Join(dir, "prog"), "-l", "definitelynotalibrary", src)
	if code != exitUsage {
		t.Errorf("exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "cannot find -ldefinitelynotalibrary") {
		t.Errorf("stderr = %q", stderr)
	}
	if !strings.Contains(stderr, "-L") && !strings.ContainsAny(stderr, `/`+string(filepath.Separator)) {
		t.Errorf("stderr = %q, want it to say where it looked", stderr)
	}
}

func TestEnv(t *testing.T) {
	hostOnly(t)
	code, stdout, stderr := run(t, "env", "-defines")
	if code != exitOK {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	for _, want := range []string{"target:", "std:", "search:", "libraries:", "predefines:", "-D __CHAR_BIT__=8"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout)
		}
	}
}

func TestASTAndTokens(t *testing.T) {
	dir := t.TempDir()
	src := write(t, dir, "a.i", "int main(void){return 0;}\n")

	if code, stdout, stderr := run(t, "ast", src); code != exitOK {
		t.Errorf("ast exited %d: %s", code, stderr)
	} else if !strings.Contains(stdout, "FuncDecl") {
		t.Errorf("ast stdout = %q", stdout)
	}
	if code, stdout, stderr := run(t, "tokens", src); code != exitOK {
		t.Errorf("tokens exited %d: %s", code, stderr)
	} else if !strings.Contains(stdout, "IDENT") {
		t.Errorf("tokens stdout = %q", stdout)
	}
}
