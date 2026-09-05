// Command vcc is the Vertex C compiler.
//
// It has nothing of its own: everything it does is cli.Run, which is a
// wrapper over the vcc package, which is the phases composed. Three lines
// here rather than none because a command is a module's entry point and a
// library is not, and keeping them apart is what lets a program embed the
// compiler without embedding the command.
package main

import (
	"os"

	"github.com/vertex-language/vcc/cli"
)

func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr)) }
