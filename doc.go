// Package vcc is the Vertex C Compiler as a library: C source in, an
// executable out, and every phase between reachable on its own.
//
//	err := vcc.Build("hello", "hello.c")
//	out, err := vcc.Run("hello.c")
//
// Past those two shorthands everything is a Compiler and a parameter struct.
// The zero Compiler builds for this host; a caller that wants a target, an
// include list, macros or libraries says so in fields.
//
// # What this package is
//
// Composition, and nothing else. The phases live in their own packages —
// token, scanner, preprocessor, parser, ast, analyzer, types, lower — and each
// of them does one thing and exports a clean surface. What was missing was the
// layer that spells the order they run in, decides what a target name means to
// each of them, and knows the rules that are correctness requirements rather
// than conveniences: that the predefines are not optional, that the include
// search order is load-bearing, that a tree the front end rejected must not be
// lowered, that verify.Module runs between lowering and the backend. That
// layer is this package. A program that wants only an AST should keep
// importing ast and parser directly and pay for nothing else.
//
// # The ladder
//
// Each rung is the one below it plus one step, and a caller stops where it
// likes:
//
//	Source      the translation unit as the parser reads it
//	Preprocess  phase 4's output as C source
//	Parse       the syntax tree
//	Check       the front end complete, with its diagnostics
//	IR          the lowered module
//	Object      an object file's bytes
//	Build       an executable
//
// Diagnostics come back as a slice, sited in the file the caller wrote; an
// error means vcc could not run rather than that the program was wrong. Build,
// which has no diagnostic slot, reports a wrong program as a *DiagnosticError.
//
// # Below vir
//
// The three hops under an *ir.Module — ir/lower selects instructions for one
// architecture, that architecture's obj/{elf,macho} writes them into a
// container, and a linker turns objects into an image — are in this package
// too, in target.go, codegen.go, link.go and register.go. Those files import
// ir and the backend repositories and nothing of vcc's front end, deliberately
// and permanently: the day a second front end wants them they lift out into
// their own module unchanged.
//
// They are here rather than in a package of their own because a target name
// decides things on both sides of vir at once — a type model above it, an
// architecture and a container below — and holding the two halves apart meant
// looking one name up twice, in two tables, that a test had to hold in step.
//
// # The linker is vcc's own
//
// There is no cc. Link is three vertex-language linkers, one per container
// format, and it takes a target rather than assuming the host: an executable
// for another platform is built the same way as one for this machine, given
// that platform's headers and libraries to link against. Nothing external is
// required — no toolchain on PATH, nothing to version-check — so a Go program
// that imports this package can produce an executable on a machine with no C
// compiler installed at all.
//
// Resolving a library name is this package's job rather than a linker's, since
// only the PE one has a search path of its own: BuildParams.Libs and LibDirs
// are -l and -L, a name resolves the way every C toolchain resolves one, and
// Env.Libraries reports where it will look.
//
// Neither is anything below vir bound to the filesystem: the linkers take
// bytes and return bytes, so an object this package just produced reaches the
// link without being written anywhere.
//
// # The machine
//
// Everything impure — the environment, whether a directory exists, what xcrun
// answers — goes through Compiler.Host, which is sysroot's injectable Host and
// defaults to the real machine. A caller that supplies one gets a compiler
// that reads nothing outside it. SOURCE_DATE_EPOCH is not read here either:
// SourceDate is a field, and the command line is what reads the variable.
//
// # What is not here
//
// No assembly output: the architecture packages are encoders and none of them
// prints. No COFF writing, no DWARF, and no optimization levels — decisions
// further down that have not been made yet.
package vcc
