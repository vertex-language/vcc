package cli

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/vertex-language/vcc"
	"github.com/vertex-language/vcc/preprocessor"
)

// The flag sets, and the one place a *vcc.Compiler is built.
//
// Everything here is command-line work: parsing repeatable flags, keeping
// their order, and reading the two environment variables a compiler must not
// read for itself. What the flags mean is the library's.

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// defineFlag appends to a shared slice so -D and -U keep command-line order.
// `-D FOO -U FOO -D FOO=2` has to mean what it says.
type defineFlag struct {
	list *[]preprocessor.Predefine
	kind preprocessor.PredefineKind
}

func (d defineFlag) String() string { return "" }
func (d defineFlag) Set(v string) error {
	*d.list = append(*d.list, preprocessor.Predefine{Kind: d.kind, Text: v})
	return nil
}

// ppFlags is the front-end flag set, shared by every verb that can read .c.
type ppFlags struct {
	includes     stringList
	defines      []preprocessor.Predefine
	preInc       stringList
	target       string
	march        string
	freestanding bool
	force        bool // -pp: preprocess even when the extension says otherwise
	raw          bool // -no-pp: never preprocess
	keepComments bool // set by cmdAST, not a flag: -comments implies it

	c *vcc.Compiler // built at most once, so the sysroot walk happens once
}

func (p *ppFlags) register(fs *flag.FlagSet) {
	fs.Var(&p.includes, "I", "add an include search directory (repeatable, in order)")
	fs.Var(defineFlag{&p.defines, preprocessor.PredefineDefine}, "D", "define a macro (repeatable)")
	fs.Var(defineFlag{&p.defines, preprocessor.PredefineUndef}, "U", "undefine a macro (repeatable)")
	fs.Var(&p.preInc, "include", "process a file before the main input (repeatable)")
	fs.StringVar(&p.target, "target", vcc.HostName(), "target to compile for")
	fs.StringVar(&p.march, "march", "", "processor to compile for: a level (x86-64-v2), a name (haswell), or +feature adjustments")
	fs.BoolVar(&p.freestanding, "freestanding", false, "builtin headers only; no platform directories")
	fs.BoolVar(&p.force, "pp", false, "preprocess regardless of extension (for stdin)")
	fs.BoolVar(&p.raw, "no-pp", false, "input is already preprocessed")
}

// compiler turns the flags into the library's compiler.
//
// The target is checked here rather than left to the library so the message
// can name the flag that fixes it — which is the one thing a library cannot
// say, and the one thing a person at a terminal wants to read.
func (p *ppFlags) compiler() (*vcc.Compiler, error) {
	if p.c != nil {
		return p.c, nil
	}
	if p.target == "" {
		return nil, fmt.Errorf("this host is not a target vcc models; name one with -target (known: %s)",
			strings.Join(vcc.Targets(), ", "))
	}
	if _, ok := vcc.LookupTarget(p.target); !ok {
		return nil, fmt.Errorf("unknown target %q (known: %s)", p.target, strings.Join(vcc.Targets(), ", "))
	}
	epoch, err := epoch()
	if err != nil {
		return nil, err
	}
	p.c = &vcc.Compiler{
		Target:       p.target,
		IncludeDirs:  p.includes,
		Defines:      p.defines,
		PreIncludes:  p.preInc,
		Freestanding: p.freestanding,
		KeepComments: p.keepComments,
		PP:           p.pp(),
		SourceDate:   epoch,
		March:        p.march,
	}
	return p.c, nil
}

// pp is what -pp and -no-pp say about phase 4. Neither is the extension's
// answer, which is what the library's zero value already means.
func (p *ppFlags) pp() vcc.Tristate {
	switch {
	case p.raw:
		return vcc.PPNever
	case p.force:
		return vcc.PPAlways
	}
	return vcc.PPAuto
}

// epoch reads SOURCE_DATE_EPOCH. The CLI always supplies an epoch — the Unix
// zero when the environment names none — so a build is deterministic with no
// flag saying so. The library never reads it: a compiler that behaves one way
// in a terminal and another in a test is a compiler nobody can trust.
func epoch() (*time.Time, error) {
	if s := os.Getenv("SOURCE_DATE_EPOCH"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("SOURCE_DATE_EPOCH: not a decimal timestamp: %q", s)
		}
		t := time.Unix(n, 0).UTC()
		return &t, nil
	}
	t := time.Unix(0, 0).UTC()
	return &t, nil
}

// buildFlags is `vcc build`, and — minus --emit and -o — `vcc run`.
type buildFlags struct {
	pp   ppFlags
	emit string
	out  string

	libDirs stringList
	libs    stringList
	entry   string
	static  bool
}

func (b *buildFlags) register(fs *flag.FlagSet) {
	b.pp.register(fs)
	fs.Var(&b.libDirs, "L", "add a library search directory (repeatable, in order)")
	fs.Var(&b.libs, "l", "link against a library (repeatable, in order)")
	fs.StringVar(&b.entry, "entry", "", "the program's entry symbol (default: the platform's)")
	fs.BoolVar(&b.static, "static", false, "link a static image")
}

// params is the build the flags describe, over inputs in command-line order.
func (b *buildFlags) params(inputs []vcc.Input, out string) vcc.BuildParams {
	return vcc.BuildParams{
		Output:  out,
		Inputs:  inputs,
		Libs:    b.libs,
		LibDirs: b.libDirs,
		Entry:   b.entry,
		Static:  b.static,
	}
}
