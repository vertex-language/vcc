package cli

import (
	"flag"
	"fmt"
	"io"
)

// cmdEnv prints the resolved configuration: the search list, in the order
// #include walks it, plus the notes sysroot raised and — with -defines — the
// target's predefined macros. The point is the invariant the READMEs promise:
// header search is data, inspectable before the build runs.
func cmdEnv(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var pp ppFlags
	pp.register(fs)
	defines := fs.Bool("defines", false, "also print the target's predefined macros")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "vcc env: takes no files")
		return exitUsage
	}

	c, err := pp.compiler()
	if err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}
	env, err := c.Env()
	if err != nil {
		fmt.Fprintln(stderr, "vcc:", err)
		return exitUsage
	}

	fmt.Fprintf(stdout, "target: %s\nstd: %s\nhosted: %v\n", env.Target.Name(), env.Std, env.Hosted)

	fmt.Fprintln(stdout, "\nsearch:")
	for _, m := range env.Search {
		tag := ""
		if m.System {
			tag = "  [system]"
		}
		fmt.Fprintf(stdout, "  %s%s\n", m.Name, tag)
	}
	if len(env.Libraries) > 0 {
		fmt.Fprintln(stdout, "\nlibraries:")
		for _, dir := range env.Libraries {
			fmt.Fprintf(stdout, "  %s\n", dir)
		}
	}
	for _, n := range env.Notes {
		fmt.Fprintf(stdout, "\nnote: %s\n", n)
	}

	if *defines {
		fmt.Fprintln(stdout, "\npredefines:")
		for _, d := range env.Predefines {
			fmt.Fprintf(stdout, "  -D %s\n", d.Text)
		}
	}
	return exitOK
}
