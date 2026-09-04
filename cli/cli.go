// Package cli implements the vcc command line.
//
// It is a wrapper. Everything about compiling C — the phases, the targets,
// the predefines, the search list, the link — is the vcc package's, and this
// package is what a command adds to it: flags, where an artifact lands,
// standard input, the caret under a diagnostic, and an exit code.
//
// The rule is that nothing here decides anything a library caller would also
// have to decide. Two copies of the pipeline is two places for the phases to
// drift apart, which is the failure mode the phase model exists to prevent —
// so when a verb needs something the library does not expose, the fix is to
// expose it, not to reach around.
//
// Run is the entire API. Everything else in the package is unexported: the
// CLI is a consumer of the library, never a library itself.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/vertex-language/vcc"
)

// Exit codes, gcc-shaped so `vcc check f.c && echo ok` means what it looks
// like.
const (
	exitOK    = 0 // no error diagnostics
	exitDiags = 1 // the input had errors
	exitUsage = 2 // the invocation or I/O had errors
)

const usage = `vcc — the Vertex C Compiler

Usage:

    vcc build  [flags] [files...]  compile and link; with --emit, stop earlier
    vcc run    [flags] [file] [-- args...]   build to a temporary path and run it
    vcc check  [flags] [files...]  preprocess, parse, analyze; print diagnostics
    vcc ast    [flags] [file]      parse and dump the syntax tree
    vcc tokens [flags] [file]      dump the token stream
    vcc env    [flags]             print the resolved include list and predefines

A file of "-" (or no file) reads standard input.

A .c file runs through vcc's own preprocessor; a .i file (or stdin)
enters the pipeline above it. -pp and -no-pp override the extension.

Common flags:
    -target T       target to compile for (default: this host)
    -I dir          add an include search directory (repeatable, in order)
    -D name[=val]   define a macro (repeatable)
    -U name         undefine a macro (repeatable)
    -include file   process a file before the main input (repeatable)
    -freestanding   builtin headers only; no platform directories
    -pp / -no-pp    force preprocessing on or off

Flags for build and run:
    --emit exe      compile and link (the default)
    --emit obj      compile to an object file, one per input
    --emit vir      stop after phase 7 and print the lowered VIR module
    --emit i        stop after phase 4 and print preprocessed source
    -o file         write output here ("-" is standard output, for i and vir)
    -L dir          add a library search directory (repeatable, in order)
    -l name         link against a library (repeatable, in order)
    -entry sym      the program's entry symbol (default: the platform's)
    -static         link a static image

Flags for ast:
    -skip-bodies    skip function bodies (fast structural pass)
    -comments       retain comments on the tree

Flags for env:
    -defines        also print the target's predefined macros

The linker is vcc's own, so -target builds and links an executable for
any target vcc models, given that platform's headers and libraries.
Running one is another matter: vcc run needs this machine's target.

Not yet: --emit asm, .vir as an input, fmt.

Exit codes: 0 no errors, 1 diagnostics with errors, 2 usage or I/O.
`

// Run executes one vcc invocation and returns its exit code. It is the
// package's entire surface: cmd/vcc calls it and nothing else.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "build":
		return cmdBuild(rest, stdout, stderr)
	case "check":
		return cmdCheck(rest, stderr)
	case "ast":
		return cmdAST(rest, stdout, stderr)
	case "tokens":
		return cmdTokens(rest, stdout, stderr)
	case "env":
		return cmdEnv(rest, stdout, stderr)
	case "run":
		return cmdRun(rest, stdout, stderr)
	case "fmt":
		fmt.Fprintln(stderr, "vcc fmt: not yet — formatting needs the printer")
		return exitUsage
	case "help", "-h", "--help", "-help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "vcc: unknown command %q\n\n%s", verb, usage)
		return exitUsage
	}
}

// input names one thing to compile. "" and "-" mean standard input, which the
// library has no way to read for itself — a pipe is the command line's, and
// what reaches the library is the bytes it carried.
func input(name string) (vcc.Input, error) {
	if name == "" || name == "-" {
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			return vcc.Input{}, fmt.Errorf("<stdin>: %w", err)
		}
		return vcc.Text("<stdin>", src), nil
	}
	return vcc.File(name), nil
}

// inputs is input over a command line, in order.
func inputs(names []string) ([]vcc.Input, error) {
	out := make([]vcc.Input, 0, len(names))
	for _, name := range names {
		in, err := input(name)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, nil
}

// writeOut sends one artifact to a file or to standard output. "-" and ""
// mean standard output, which is what `-o -` in the README's examples asks
// for.
func writeOut(name string, stdout io.Writer, data []byte) error {
	if isStdout(name) {
		_, err := stdout.Write(data)
		return err
	}
	return os.WriteFile(name, data, 0o666)
}

func isStdout(name string) bool { return name == "" || name == "-" }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
