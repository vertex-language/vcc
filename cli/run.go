package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/vertex-language/vcc"
)

// cmdRun builds a program into a temporary directory and runs it.
//
// Everything after `--` is the program's, which is why the flag set stops
// there rather than at the first non-flag: `vcc run p.c -- -o x` passes -o to
// the program, and vcc's own -o would be meaningless here anyway — the image
// is temporary by definition.
func cmdRun(args []string, stdout, stderr io.Writer) int {
	args, progArgs := splitDashDash(args)

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var b buildFlags
	b.register(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	names := fs.Args()
	if len(names) == 0 {
		fmt.Fprintln(stderr, "vcc run: needs a file; standard input has nothing to run")
		return exitUsage
	}

	// Building for another target is fine; running the result here is not.
	// The library will happily produce it — this is the one thing that is
	// this machine's business rather than the compiler's.
	if b.pp.target != vcc.HostName() {
		fmt.Fprintf(stderr, "vcc run: %s is not this machine; build it and run it there\n", b.pp.target)
		return exitUsage
	}

	c, err := b.pp.compiler()
	if err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}
	in, err := inputs(names)
	if err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}

	prog, err := c.Program(b.params(in, ""))
	if err != nil {
		return report(err, stderr)
	}
	defer prog.Close()

	cmd := prog.Command(progArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// The program's exit code is the answer, not vcc's: a program
			// that exits 1 did not fail to build.
			return ee.ExitCode()
		}
		fmt.Fprintln(stderr, "vcc run:", err)
		return exitUsage
	}
	return exitOK
}

// splitDashDash cuts the argument list at the first bare "--".
func splitDashDash(args []string) (mine, theirs []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}
