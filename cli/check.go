package cli

import (
	"flag"
	"fmt"
	"io"
)

func cmdCheck(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var pp ppFlags
	pp.register(fs)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	names := fs.Args()
	if len(names) == 0 {
		names = []string{"-"}
	}
	c, err := pp.compiler()
	if err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}

	code := exitOK
	for _, name := range names {
		in, err := input(name)
		if err != nil {
			fmt.Fprintln(stderr, "vcc:", err)
			code = max(code, exitUsage)
			continue
		}
		diags, err := c.Check(in)
		if err != nil {
			fmt.Fprintln(stderr, "vcc:", err)
			code = max(code, exitUsage)
			continue
		}
		if printDiags(stderr, diags) {
			code = max(code, exitDiags)
		}
	}
	return code
}
