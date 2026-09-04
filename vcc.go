package vcc

import "github.com/vertex-language/vcc/preprocessor"

// Version is the compiler's version. One constant, so the .vir banner, the
// producer string an object carries, and anything a caller prints agree by
// construction.
const Version = "0.1.0"

// Build compiles and links files into an executable for this host. A path vcc
// does not compile — a .o, a .a — is passed to the linker in place, in the
// order given.
//
// It is the shorthand: a zero Compiler and one call, for the case with no
// options in it. Everything past that — a target, include directories,
// libraries, macros — is Compiler and BuildParams, which this is three lines
// over.
func Build(out string, paths ...string) error {
	inputs := make([]Input, 0, len(paths))
	for _, p := range paths {
		inputs = append(inputs, File(p))
	}
	var c Compiler
	return c.Build(BuildParams{Output: out, Inputs: inputs})
}

// Run compiles a source file, runs it, and returns what it printed on standard
// output. Nothing is left behind.
//
// The two ways it can fail are the two a caller cares about, and they are told
// apart by type: a *DiagnosticError means it did not compile, and an
// *exec.ExitError means it ran and exited non-zero — carrying the status in
// ExitCode and standard error in Stderr, exactly as exec.Cmd.Output does.
//
// The output is buffered, so a program that never stops, or prints without
// bound, hangs the caller. This is the shorthand for a program you already
// believe terminates; Program hands back an *exec.Cmd, and with it
// exec.CommandContext and everything else, for one that you do not.
func Run(source string) ([]byte, error) {
	var c Compiler
	prog, err := c.Program(BuildParams{Inputs: []Input{File(source)}})
	if err != nil {
		return nil, err
	}
	defer prog.Close()
	return prog.Command().Output()
}

// Define is a -D: a macro the preprocessor starts with. An empty value defines
// the macro as 1, which is what -D FOO on a command line means.
func Define(name, value string) preprocessor.Predefine {
	text := name
	if value != "" {
		text = name + "=" + value
	}
	return preprocessor.Predefine{Kind: preprocessor.PredefineDefine, Text: text}
}

// Undefine is a -U. Order matters against Define: Compiler.Defines is one
// list, and `-D FOO -U FOO -D FOO=2` has to mean what it says.
func Undefine(name string) preprocessor.Predefine {
	return preprocessor.Predefine{Kind: preprocessor.PredefineUndef, Text: name}
}
