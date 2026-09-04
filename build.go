package vcc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildParams is one build: what to compile and link, and where to put it.
type BuildParams struct {
	// Output is where the executable goes. Build requires one; Program builds
	// to a temporary path and ignores this field.
	Output string

	// Inputs are the sources and objects to build, in link order.
	//
	// Order is the caller's and is preserved exactly, including where a
	// compiled source falls among the objects around it. A static link is
	// order-sensitive, and reordering it would be vcc deciding something the
	// caller said — so a caller that builds this slice from a map or a set
	// produces a link that works on their machine and not on someone else's.
	//
	// An input vcc compiles is a .c or .i file; anything else — a .o, a .a —
	// is passed to the linker in place, which is what makes an input list of
	// "main.c" and "libfoo.a" mean what it looks like.
	Inputs []Input

	Entry  string // the program's entry symbol; "" is the platform's
	Static bool   // link a static image

	// Libs and LibDirs are -l and -L: library names to link against, and the
	// directories to look for them in before the platform's own.
	//
	// A name resolves to a file the way every C toolchain resolves one —
	// libfoo.so or libfoo.tbd before libfoo.a, the archive alone under
	// Static, LibDirs before the platform — and the search list is what
	// Env.Libraries reports. A library that already has a path needs none of
	// this and is an Input like any other, linked exactly where it appears.
	Libs    []string
	LibDirs []string
}

// Build compiles every source input and links the results with everything
// else, in order.
//
// The program being wrong is a *DiagnosticError. Every other error means vcc
// could not run.
func (c *Compiler) Build(p BuildParams) error {
	if p.Output == "" {
		return fmt.Errorf("build needs an output path")
	}
	t, err := c.target()
	if err != nil {
		return err
	}

	objs := make([]Input, 0, len(p.Inputs))
	var diags []Diagnostic
	for _, in := range p.Inputs {
		if !in.isSource() {
			objs = append(objs, in)
			continue
		}
		// No temporary files: the linkers take (name, bytes), so an object
		// this call just produced goes straight to the link.
		data, ds, err := c.Object(in)
		diags = append(diags, ds...)
		if err != nil {
			return err
		}
		if data == nil {
			continue // it did not compile; its diagnostics say why
		}
		objs = append(objs, ObjectBytes(objectName(in), data))
	}
	if HasErrors(diags) {
		return &DiagnosticError{Diagnostics: diags}
	}

	return link(t, linkParams{
		Objects:      objs,
		Output:       imageName(t, p.Output),
		LibDirs:      p.LibDirs,
		Libs:         p.Libs,
		Entry:        p.Entry,
		Static:       p.Static,
		Freestanding: c.Freestanding,
		Host:         c.Host,
	})
}

// A Program is a built executable and the temporary directory holding it.
//
// It exists for the one piece of running a program that is not os/exec's:
// nobody wants to write MkdirTemp plus defer RemoveAll plus a name that will
// not collide in order to run a C file.
type Program struct {
	path string
	dir  string
}

// Program compiles and links to a temporary path and hands back the artifact.
// The caller owns it until Close.
//
// It builds for whatever target the compiler names and does not check that
// this machine can execute the result: a cross-built program is a legitimate
// thing to want on disk. Running one is the caller's business, and it is
// os/exec that reports what happens.
func (c *Compiler) Program(p BuildParams) (*Program, error) {
	t, err := c.target()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "vcc")
	if err != nil {
		return nil, err
	}
	p.Output = imageName(t, filepath.Join(dir, programName(p.Inputs)))
	if err := c.Build(p); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &Program{path: p.Output, dir: dir}, nil
}

// Path is where the executable is.
func (p *Program) Path() string { return p.path }

// Command is an unstarted exec.Cmd for the program, with args as its argv.
//
// Execution is os/exec's job, so the caller configures it with the type they
// already know: Stdin, Stdout, Env, Dir, CombinedOutput, exec.CommandContext
// for a program that might not stop.
func (p *Program) Command(args ...string) *exec.Cmd { return exec.Command(p.path, args...) }

// Close removes the program and the directory holding it.
func (p *Program) Close() error {
	if p.dir == "" {
		return nil
	}
	dir := p.dir
	p.dir, p.path = "", ""
	return os.RemoveAll(dir)
}

// objectName is what a compiled source is called on its way to the linker. It
// is a name in a link, not a path: nothing writes it.
func objectName(in Input) string {
	stem := in.moduleName()
	if stem == "" {
		stem = "a"
	}
	return stem + ".o"
}

// imageName is where a link actually writes.
//
// On Windows a program is called .exe. Not because the loader cares — it reads
// the header and will run a file called anything — but because everything
// around it does: cmd will not start an extensionless file, Explorer will not
// either, and Go's own exec.Command resolves a path with no extension by
// trying the ones in %PATHEXT% and nothing else. A build that wrote "hello"
// would produce something the machine that built it cannot run, which is the
// same behaviour MinGW's gcc and link.exe's /OUT: both decline.
//
// A name that already carries an extension is left alone: -o hello.bin means
// hello.bin.
func imageName(t Target, path string) string {
	if t.format != FormatPE || filepath.Ext(path) != "" {
		return path
	}
	return path + ".exe"
}

// programName is what a temporary build is called: after its first source, so
// argv[0] and a process listing say what is running.
func programName(inputs []Input) string {
	for _, in := range inputs {
		if in.isSource() {
			if stem := in.moduleName(); stem != "" {
				return stem
			}
			return "a"
		}
	}
	return "a.out"
}
