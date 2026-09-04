package vcc

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/vertex-language/vcc/preprocessor"
	"github.com/vertex-language/vcc/sysroot"
)

// Tristate is a three-valued switch: follow the input, or override it.
type Tristate uint8

const (
	PPAuto   Tristate = iota // .c is preprocessed, .i is not
	PPAlways                 // preprocess regardless of extension
	PPNever                  // the input is already preprocessed
)

// A Compiler is a target, a search list, and the machine to ask about both.
//
// The zero value compiles for this host with no extra include directories,
// which is what `vcc build` does with no flags:
//
//	var c vcc.Compiler
//	err := c.Build(vcc.BuildParams{Output: "hello", Inputs: []vcc.Input{vcc.File("hello.c")}})
//
// The configuration a target implies — its predefined macros and the include
// search list under it — is resolved once, on first use, and reused for every
// input after. That is deliberate on both counts: resolving it eagerly would
// break a caller who only wants to parse .i source on a machine vcc cannot
// model, and resolving it per file would walk the filesystem once per
// translation unit, which is fine for a command compiling one file and wrong
// for a build system compiling four hundred.
//
// A Compiler must not be mutated or copied after its first call. Concurrent
// calls on one Compiler are otherwise fine: everything a phase touches after
// that point is either read-only or per-unit.
type Compiler struct {
	// Target names the machine to compile for: "aarch64-macos", and so on
	// through Targets. "" is this host, and is an error on a host vcc does
	// not model — where a caller says which target it meant.
	Target string

	// IncludeDirs are searched before everything sysroot resolves, in order.
	IncludeDirs []string

	// Defines are -D and -U in one list, because their order is meaning:
	// `-D FOO -U FOO -D FOO=2` has to say what it says. The target's own
	// predefines precede all of them, so a caller can undefine one.
	Defines []preprocessor.Predefine

	// PreIncludes are processed before the main input, in order.
	PreIncludes []string

	// Freestanding compiles against vcc's builtin headers alone: no platform
	// directories, and __STDC_HOSTED__ is 0.
	Freestanding bool

	// KeepComments keeps COMMENT tokens through phase 4, for a caller that
	// asked the parser to retain them.
	KeepComments bool

	// PP decides whether phase 4 runs. The zero value follows the input's
	// extension.
	PP Tristate

	// SourceDate fixes __DATE__ and __TIME__. Nil is the Unix epoch, so a
	// build is reproducible with nothing said.
	//
	// Deliberately not read from SOURCE_DATE_EPOCH here: a library that reads
	// the environment behaves differently in a test than in a terminal. The
	// command line reads the variable and passes it in.
	SourceDate *time.Time

	// Host is every impurity behind header and SDK discovery — the
	// environment, directory existence, xcrun. Nil is the real machine. A
	// caller that supplies one gets a compiler that touches nothing outside
	// it, which is what makes a hermetic build hermetic and a test a test.
	Host sysroot.Host

	// OnDiagnostic receives every diagnostic as it is produced, warnings
	// included, in the order a compiler would print them. Nil is fine: each
	// phase returns its diagnostics anyway, and Build collects errors into a
	// *DiagnosticError.
	OnDiagnostic func(Diagnostic)

	// Producer is the toolchain string stamped into object files and the .vir
	// banner. "" is "vcc " + Version.
	Producer string

	// March is the processor to compile for, in the spelling
	// amd64/feature.Parse reads: a level ("x86-64-v2", "haswell"), a list of
	// features ("+popcnt,+lzcnt"), or a level with adjustments. "" is the
	// architecture's baseline, which on x86-64 is v1 — SSE2 and nothing
	// above it, the same floor MSVC's default /arch:SSE2 sets.
	//
	// It decides what the *compiler* may choose, and not what the program
	// may ask for by name. An intrinsic that names an instruction —
	// __popcnt, __lzcnt — emits it whichever level this is, because writing
	// the name is the statement that the target has it; that is MSVC's rule
	// and gcc's, and a compiler that refused would be refusing the only
	// thing the intrinsic is for.
	March string

	once  sync.Once
	cfg   preprocessor.Config
	notes []string
	cfgTt Target
	cfgEr error
}

// An Env is the resolved configuration: what #include will search, in the
// order it walks, and what the target defines before the first line is read.
// It is what `vcc env` prints, and the point of it is the invariant the
// READMEs promise — header search is data, inspectable before the build runs.
type Env struct {
	Target     Target
	Std        string
	Hosted     bool
	Search     []preprocessor.Mount
	Predefines []preprocessor.Predefine

	// Libraries is where a -l name is looked for, in order, after any
	// directory the caller named. The link's half of Search.
	Libraries []string

	// Notes is sysroot's advice where something expected was missing: no SDK,
	// no vcvars environment. They are never errors — a host with no system
	// headers still resolves, to the builtins.
	Notes []string
}

// Env resolves the configuration and reports it.
func (c *Compiler) Env() (Env, error) {
	cfg, notes, err := c.config()
	if err != nil {
		return Env{}, err
	}
	t, _ := c.target()
	return Env{
		Target:     t,
		Std:        cfg.Std.String(),
		Hosted:     cfg.Hosted,
		Search:     cfg.Search,
		Predefines: cfg.Predefines,
		Libraries:  sysroot.LibraryDirs(c.Host, t.Name(), cfg.Hosted),
		Notes:      notes,
	}, nil
}

// target resolves the target name.
//
// It is separate from config because a caller may need the type model without
// needing an include search list — and because indexing the table without
// checking gives a zero Model, which sizes every type at nothing and reports
// nonsense.
func (c *Compiler) target() (Target, error) {
	name := c.Target
	if name == "" {
		name = HostName()
	}
	if name == "" {
		return Target{}, fmt.Errorf("this host is not a target vcc models; name one (known: %s)", targetList())
	}
	t, ok := LookupTarget(name)
	if !ok {
		return Target{}, fmt.Errorf("unknown target %q (known: %s)", name, targetList())
	}
	return t, nil
}

// config composes phase 4's configuration, once: the target's model macros,
// the search list from IncludeDirs plus sysroot, and the epoch.
//
// The order of Search is the one preprocessor's config.go documents —
// IncludeDirs first, then what sysroot resolved (VCC_INCLUDE_PATH, the
// builtins, the platform), which is also the order Env reports.
func (c *Compiler) config() (preprocessor.Config, []string, error) {
	c.once.Do(func() {
		t, err := c.target()
		if err != nil {
			c.cfgEr = err
			return
		}
		c.cfgTt = t

		cfg := preprocessor.Config{
			Std:          preprocessor.C17,
			Hosted:       !c.Freestanding,
			PreIncludes:  c.PreIncludes,
			KeepComments: c.KeepComments,
		}

		// The target's model macros come first, then the caller's, in order —
		// so -U can remove a model macro and -D can shadow one.
		cfg.Predefines = append(t.Predefines(), c.Defines...)

		for _, dir := range c.IncludeDirs {
			cfg.Search = append(cfg.Search, preprocessor.Mount{Name: dir, FS: os.DirFS(dir)})
		}
		entries, notes := sysroot.ResolveWith(c.Host, t.Name(), cfg.Hosted)
		for _, e := range entries {
			cfg.Search = append(cfg.Search, preprocessor.Mount{Name: e.Name, FS: e.FS, System: e.System})
		}

		epoch := time.Unix(0, 0).UTC()
		if c.SourceDate != nil {
			epoch = c.SourceDate.UTC()
		}
		cfg.Epoch = &epoch

		c.cfg, c.notes = cfg, notes
	})
	return c.cfg, c.notes, c.cfgEr
}

// producer is what this compiler stamps into what it emits.
func (c *Compiler) producer() string {
	if c.Producer != "" {
		return c.Producer
	}
	return "vcc " + Version
}

// report hands each diagnostic to OnDiagnostic, and returns them unchanged so
// a phase can end with `return c.report(diags)`.
func (c *Compiler) report(diags []Diagnostic) []Diagnostic {
	if c.OnDiagnostic != nil {
		for _, d := range diags {
			c.OnDiagnostic(d)
		}
	}
	return diags
}
