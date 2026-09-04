package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vertex-language/ir/text"

	"github.com/vertex-language/vcc"
)

func cmdBuild(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var b buildFlags
	b.register(fs)
	fs.StringVar(&b.emit, "emit", "exe", `stop after a phase and write its output: "exe", "obj", "vir", "i"`)
	fs.StringVar(&b.out, "o", "", `write output here ("-" is standard output, for i and vir)`)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	names := fs.Args()
	if len(names) == 0 {
		names = []string{"-"} // standard input, as every other verb reads it
	}

	switch b.emit {
	case "i", "vir", "obj":
		if b.out != "" && len(names) > 1 {
			fmt.Fprintf(stderr, "vcc build: --emit %s writes one artifact per input, so -o takes one input (got %d)\n", b.emit, len(names))
			return exitUsage
		}
	case "exe":
	case "asm":
		// The architecture packages encode; none of them prints. This is a
		// missing package, not a missing flag, so it says so.
		fmt.Fprintln(stderr, "vcc build: --emit asm has nothing to call — the architecture packages are encoders and none of them disassembles")
		return exitUsage
	default:
		fmt.Fprintf(stderr, "vcc build: unknown --emit %q (known: exe, obj, vir, i)\n", b.emit)
		return exitUsage
	}

	c, err := b.pp.compiler()
	if err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}

	switch b.emit {
	case "i":
		return eachInput(names, b.out, ".i", true, stderr, func(in vcc.Input, out string) int {
			return emitPP(c, in, out, stdout, stderr)
		})
	case "vir":
		return eachInput(names, b.out, ".vir", true, stderr, func(in vcc.Input, out string) int {
			return emitVIR(c, in, out, stdout, stderr)
		})
	case "obj":
		return eachInput(names, b.out, ".o", false, stderr, func(in vcc.Input, out string) int {
			return emitObj(c, in, out, stderr)
		})
	}
	return buildExe(c, &b, names, b.out, stderr)
}

// eachInput runs one artifact-per-input emission. With -o there is exactly one
// input and -o is where it goes. Without one, text goes to standard output
// when there is a single input to write — that is what a pipe expects — and
// everything else is named for its input under ext.
func eachInput(names []string, out, ext string, stdoutOK bool, stderr io.Writer, emit func(in vcc.Input, out string) int) int {
	code := exitOK
	for _, name := range names {
		dst := out
		if dst == "" {
			if stdoutOK && len(names) == 1 {
				dst = "-"
			} else {
				dst = artifactName(name, ext)
			}
		}
		in, err := input(name)
		if err != nil {
			fmt.Fprintln(stderr, "vcc:", err)
			return exitUsage
		}
		if c := emit(in, dst); c != exitOK {
			code = max(code, c)
		}
	}
	return code
}

// ---- --emit i ----

func emitPP(c *vcc.Compiler, in vcc.Input, outName string, stdout, stderr io.Writer) int {
	data, diags, err := c.Preprocess(in)
	if err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}
	if printDiags(stderr, diags) {
		return exitDiags // errors were printed; nothing is written
	}
	if err := writeOut(outName, stdout, data); err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}
	return exitOK
}

// ---- --emit vir ----

// emitVIR prints the lowered module as .vir text.
//
// The module is printed even when diagnostics are errors: a partial module is
// what broken input should produce under --emit vir, the same contract
// `vcc ast` keeps for a broken parse.
func emitVIR(c *vcc.Compiler, in vcc.Input, outName string, stdout, stderr io.Writer) int {
	mod, diags, err := c.IR(in)
	if err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}
	hadErrors := printDiags(stderr, diags)

	var b strings.Builder
	if err := text.Print(&b, mod); err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}
	if err := writeOut(outName, stdout, []byte(b.String())); err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}

	if hadErrors {
		return exitDiags
	}
	return exitOK
}

// ---- --emit obj ----

func emitObj(c *vcc.Compiler, in vcc.Input, outName string, stderr io.Writer) int {
	if isStdout(outName) {
		fmt.Fprintln(stderr, "vcc build: an object file needs a path; -o - is for i and vir")
		return exitUsage
	}
	data, diags, err := c.Object(in)
	if err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}
	if printDiags(stderr, diags) {
		return exitDiags
	}
	if err := os.WriteFile(outName, data, 0o666); err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}
	return exitOK
}

// ---- --emit exe ----

// buildExe compiles every source input and links the results with whatever
// else was named.
//
// An input vcc does not compile — a .o, a .a, anything the linker takes — is
// passed through in place, which is what makes `vcc build main.c libfoo.a`
// mean what it looks like. Order is the command line's, and the library keeps
// it: a static link is order-sensitive, and reordering it would be vcc
// deciding something the user said.
func buildExe(c *vcc.Compiler, b *buildFlags, names []string, out string, stderr io.Writer) int {
	if out == "" {
		out = "a.out"
	}
	if isStdout(out) {
		fmt.Fprintln(stderr, "vcc build: an executable needs a path; -o - is for i and vir")
		return exitUsage
	}
	inputs, err := inputs(names)
	if err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}
	return report(c.Build(b.params(inputs, out)), stderr)
}

// report turns one library error into an exit code: a wrong program is
// diagnostics, and everything else is the invocation or the machine.
func report(err error, stderr io.Writer) int {
	var de *vcc.DiagnosticError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &de):
		printDiags(stderr, de.Diagnostics)
		return exitDiags
	default:
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}
}

// artifactName is what an input's artifact is called: the input's stem under
// ext, in the working directory — where cc -c puts one, and not beside the
// source. Standard input has no name to derive one from and becomes "a".
func artifactName(name, ext string) string {
	stem := ""
	if name != "" && name != "-" {
		base := filepath.Base(name)
		stem = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if stem == "" {
		stem = "a"
	}
	if ext == "" {
		return stem
	}
	return stem + ext
}
