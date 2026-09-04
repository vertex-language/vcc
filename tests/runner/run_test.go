// Package runner_test runs the C suite: every program under tests/ is
// compiled by vcc, linked, and run, and passes by exiting zero.
//
// The programs check themselves. Each returns a distinct small number for
// each thing it tests, so a failure names the assertion rather than only the
// file, and prints one line on success. That is what makes them runnable
// against another compiler unchanged — which is how they were written:
//
//	clang -std=gnu11 -w -o a.out tests/expr/operators.c && ./a.out; echo $?
//
// must give the same status and the same output as vcc does. A test that
// disagrees with clang is a bug in one of them, and the comment in the file
// says which behaviour the standard requires.
//
// The layout is one directory per area, and three shapes:
//
//   - tests/<area>/*.c        one file, one program. Most of the suite.
//   - tests/link/<name>/*.c   linked together as one program, for what only
//     exists between translation units.
//   - tests/errors/*.c        programs that must NOT compile: each line is a
//     constraint violation annotated with the paragraph it
//     breaks, and `vcc check` is expected to fail.
//
// The runner is a package of its own because Go will not build a directory
// that holds C files, and the C files are the point of tests/.
package runner_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// suiteDir is where the C programs live, relative to this package.
const suiteDir = ".."

// notPrograms are the directories that are not areas of single-file
// programs: the runner itself, the negative suite, and the multi-unit one.
var notPrograms = map[string]bool{"runner": true, "errors": true, "link": true}

// vccPath builds the compiler once and returns the binary's path.
var vccPath = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "vcc-suite")
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "vcc")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "github.com/vertex-language/vcc/cmd/vcc")
	cmd.Dir = "../.."
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", &buildError{out: string(out), err: err}
	}
	return bin, nil
})

type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.out }

func compiler(t *testing.T) string {
	t.Helper()
	bin, err := vccPath()
	if err != nil {
		t.Fatalf("building vcc: %v", err)
	}
	return bin
}

// programs is every single-file program in the suite, named the way it is
// written in a -run pattern: "expr/operators.c".
func programs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(suiteDir)
	if err != nil {
		t.Fatalf("reading the suite: %v", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || notPrograms[e.Name()] {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(suiteDir, e.Name(), "*.c"))
		for _, f := range files {
			out = append(out, filepath.Join(e.Name(), filepath.Base(f)))
		}
	}
	if len(out) == 0 {
		t.Fatal("no test programs found")
	}
	return out
}

// path is where a program named "expr/operators.c" is on disk.
func path(name string) string { return filepath.Join(suiteDir, name) }

// noise is the SDK's own greeting to a compiler it has not heard of, plus the
// lines that decorate it. It says nothing about the program under test.
func trim(out string) string {
	var keep []string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "cdefs.h"),
			strings.Contains(line, "Unsupported compiler"),
			strings.Contains(line, "in file included from"),
			strings.TrimSpace(line) == "",
			strings.HasPrefix(strings.TrimSpace(line), "^"),
			strings.HasPrefix(strings.TrimSpace(line), "#warning"):
			continue
		}
		keep = append(keep, line)
	}
	if len(keep) > 12 {
		keep = keep[:12]
	}
	return strings.Join(keep, "\n")
}

// flagsPrefix marks a line that gives the compiler extra flags for one
// program. It is the first line of the file:
//
//	/* vcc-flags: -I preproc/inc_a -I preproc/inc_b */
//
// Paths in it are relative to tests/, which is where a reader of the file
// would expect them to be. Most programs need none; the ones that do are
// testing something about the search path itself.
const flagsPrefix = "/* vcc-flags:"

// extraFlags reads the vcc-flags line, if there is one.
//
// A line may name a host before its flags:
//
//	vcc-flags: windows: -l ws2_32
//
// and then applies on that host and nowhere else. tests/platform is what
// needs it: a program guarded by the platform macro compiles everywhere, but
// the library it links against exists in one place, and a -l ws2_32 on a Mac
// is a link nobody can satisfy. A line naming no host applies on all of
// them, which is every other file in the suite.
func extraFlags(t *testing.T, file string) []string {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	line, _, _ := strings.Cut(string(src), "\n")
	// Trimmed before the suffix comes off. A checked-out file carries the
	// host's line endings, so on Windows this line ends "*/\r" and cutting
	// "*/" from that leaves the comment terminator in the flag list — where
	// it becomes an input file nobody can open.
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, flagsPrefix) {
		return nil
	}
	line = strings.TrimSuffix(strings.TrimPrefix(line, flagsPrefix), "*/")
	fields := strings.Fields(line)
	if len(fields) > 0 && strings.HasSuffix(fields[0], ":") {
		if host := strings.TrimSuffix(fields[0], ":"); host != runtime.GOOS {
			return nil
		}
		fields = fields[1:]
	}
	// A path in the line is written relative to tests/; the runner runs from
	// tests/runner, so it is joined back.
	for i := 0; i < len(fields)-1; i++ {
		switch fields[i] {
		case "-I", "-L", "-include":
			fields[i+1] = filepath.Join(suiteDir, fields[i+1])
			i++
		}
	}
	return fields
}

// TestPrograms runs every single-file program.
func TestPrograms(t *testing.T) {
	vcc := compiler(t)
	for _, name := range programs(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := path(name)
			args := append([]string{"run"}, extraFlags(t, f)...)
			out, err := exec.Command(vcc, append(args, f)...).CombinedOutput()
			if err != nil {
				t.Fatalf("%v\n%s", err, trim(string(out)))
			}
		})
	}
}

// TestLinked links each directory under tests/link into one program. What it
// covers is everything that only exists between translation units: a
// definition in one file and a declaration in another, a quoted include
// beside the source, a tentative definition resolved at the end of a unit,
// and an inline definition that several units emit.
func TestLinked(t *testing.T) {
	vcc := compiler(t)
	dirs, _ := filepath.Glob(filepath.Join(suiteDir, "link", "*"))
	if len(dirs) == 0 {
		t.Skip("no multi-unit programs")
	}
	for _, d := range dirs {
		name := filepath.Base(d)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srcs, _ := filepath.Glob(filepath.Join(d, "*.c"))
			if len(srcs) == 0 {
				t.Skipf("link/%s holds no C files", name)
			}
			bin := filepath.Join(t.TempDir(), name)
			args := []string{"build", "-o", bin}
			for _, src := range srcs {
				args = append(args, extraFlags(t, src)...)
			}
			args = append(args, srcs...)
			if out, err := exec.Command(vcc, args...).CombinedOutput(); err != nil {
				t.Fatalf("build: %v\n%s", err, trim(string(out)))
			}
			if out, err := exec.Command(bin).CombinedOutput(); err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
		})
	}
}

// TestErrors checks the negative suite: each program is a constraint
// violation, and a compiler that accepts one is not diagnosing it.
//
// The check is that vcc fails, not what it says. What it says is in the file,
// annotated line by line with the paragraph each line violates, so the
// diagnostics can be read against the standard by running:
//
//	vcc check tests/errors/constraints.c
func TestErrors(t *testing.T) {
	vcc := compiler(t)
	files, _ := filepath.Glob(filepath.Join(suiteDir, "errors", "*.c"))
	if len(files) == 0 {
		t.Skip("no negative tests")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			out, err := exec.Command(vcc, "check", f).CombinedOutput()
			if err == nil {
				t.Fatalf("vcc check accepted it; every line is a constraint violation\n%s", trim(string(out)))
			}
		})
	}
}

// TestPreprocessedInput checks phase 4's round trip: `--emit i` output
// re-enters as `.i` input and produces a program that behaves the same.
//
// It is the property that lets vcc drop into a build that preprocesses
// separately, and it is easy to break — a `.i` file has no directives, so
// everything the preprocessor knew has to have been written into the text.
func TestPreprocessedInput(t *testing.T) {
	vcc := compiler(t)
	for _, name := range programs(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := path(name)
			dir := t.TempDir()
			pre := filepath.Join(dir, "pre.i")
			args := append([]string{"build", "--emit", "i", "-o", pre}, extraFlags(t, f)...)
			if out, err := exec.Command(vcc, append(args, f)...).CombinedOutput(); err != nil {
				t.Fatalf("--emit i: %v\n%s", err, trim(string(out)))
			}
			bin := filepath.Join(dir, "a.out")
			// The flags come along. Their -I half is spent, the
			// preprocessing having happened, but their -l half belongs to
			// the link and the link is still ahead.
			back := append([]string{"build", "-o", bin}, extraFlags(t, f)...)
			if out, err := exec.Command(vcc, append(back, pre)...).CombinedOutput(); err != nil {
				t.Fatalf("compiling the preprocessed form: %v\n%s", err, trim(string(out)))
			}
			if out, err := exec.Command(bin).CombinedOutput(); err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
		})
	}
}

// TestClangPreprocessedInput compiles clang's preprocessed output.
//
// This is the interoperation claim, and it is not the same as the round trip
// above: clang's output carries what clang's headers said, which on Darwin
// includes its own <stdarg.h>, its fortified string builtins, its nullability
// qualifiers, and attribute arguments that are not expressions. Every one of
// those was a bug here.
//
// Blocks are excluded because they are a language feature, not a spelling:
// `void (^)(void)` is a closure type, and vcc does not have one.
func TestClangPreprocessedInput(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang is not installed")
	}
	vcc := compiler(t)
	for _, name := range programs(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := path(name)
			dir := t.TempDir()
			pre := filepath.Join(dir, "pre.i")
			args := append([]string{"-E", "-std=gnu11", "-fno-blocks", "-o", pre}, extraFlags(t, f)...)
			if out, err := exec.Command(clang, append(args, f)...).CombinedOutput(); err != nil {
				t.Skipf("clang could not preprocess it: %v\n%s", err, out)
			}
			bin := filepath.Join(dir, "a.out")
			back := append([]string{"build", "-o", bin}, extraFlags(t, f)...)
			if out, err := exec.Command(vcc, append(back, pre)...).CombinedOutput(); err != nil {
				t.Fatalf("compiling clang's output: %v\n%s", err, trim(string(out)))
			}
			if out, err := exec.Command(bin).CombinedOutput(); err != nil {
				t.Fatalf("run: %v\n%s", err, out)
			}
		})
	}
}
