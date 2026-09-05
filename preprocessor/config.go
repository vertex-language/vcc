package preprocessor

import (
	"io/fs"
	"time"

	"github.com/vertex-language/vcc/token"
)

// Std is the language the preprocessor predefines for. It reaches phase 4
// because __STDC_VERSION__ is phase 4's to define, and because a diagnostic
// that cites chapter and verse needs to know which document it is citing.
type Std uint8

const (
	C11 Std = iota
	C17
)

// Version returns the __STDC_VERSION__ value for the standard.
func (s Std) Version() string {
	if s == C11 {
		return "201112L"
	}
	return "201710L"
}

func (s Std) String() string {
	if s == C11 {
		return "c11"
	}
	return "c17"
}

// Mount is one entry in the include search list: a filesystem and the name it
// is known by.
//
// The preprocessor reads fs.FS and never os. Where headers live is sysroot's
// question, answered once in the vcc package and handed here as data — which is what
// makes phase 4 testable against fstest.MapFS with no host involved, and what
// keeps `vcc env` able to print the resolved list before the build runs.
//
// Name is what a path resolved against this mount is spelled as in __FILE__,
// in diagnostics and in --deps output. It is the directory as given, never
// made absolute.
type Mount struct {
	Name string
	FS   fs.FS

	// System marks a mount whose headers are not the user's code: warnings
	// sited inside one are reported once per header rather than once per
	// inclusion, and the tolerated-spelling carve-out applies to what the
	// parser reads out of them.
	System bool
}

// PredefineKind distinguishes the two command-line operations.
type PredefineKind uint8

const (
	PredefineDefine PredefineKind = iota
	PredefineUndef
)

// Predefine is one -D or -U, or one macro the target model contributes.
//
// Text is the spelling after the flag: "NDEBUG", "VERSION=3", "MAX(a,b)=..."
// for a define, and a bare name for an undef. It is parsed by the same
// #define grammar directive.go runs, so -D and #define cannot drift apart.
//
// Target-dependent macros (__CHAR_BIT__, __SIZEOF_LONG__, __INT_MAX__ and
// kin) arrive through this list too. They are facts about types.Model, and
// preprocessor does not import types — the vcc package computes them and puts them
// here, the same inversion that keeps sysroot out of phase 4.
type Predefine struct {
	Kind PredefineKind
	Text string
}

// Config is everything phase 4 needs from its caller. The preprocessor is a
// pure function of it: same Config, same input, same output, always.
type Config struct {
	// Search is the include list, in the order §6.10.2 walks it: -I
	// directories first, then VCC_INCLUDE_PATH, then vcc's builtin headers,
	// then the platform's. There is no second list — #include "..." looks in
	// the including file's directory first and then walks this one, and
	// #include <...> skips that first step. That is the whole rule.
	Search []Mount

	// Source is the directory the primary input was read from, as a mount.
	//
	// §6.10.2p3 has a quoted #include look beside the file that wrote it, and
	// the primary source file is a file like any other: `#include "shared.h"`
	// at the top of src/a.c must find src/shared.h without an -I. Every
	// header reached from there gets this for free — its Origin carries the
	// mount it was found in — but the primary file has no Origin until one is
	// made, which is what this supplies.
	//
	// Zero is a source with no directory: stdin, or a caller working entirely
	// in memory. The search then starts at Search, which is what a quoted
	// include from nowhere can look in.
	Source Mount

	// Predefines are applied in order before the primary source file is read.
	Predefines []Predefine

	// PreIncludes are processed before the main input, in order, exactly as
	// if #include'd at the top of it. This is --include / -include / /FI.
	PreIncludes []string

	// Std selects __STDC_VERSION__ and the standard a diagnostic cites.
	Std Std

	// Hosted sets __STDC_HOSTED__. False is --freestanding, where Search
	// holds vcc's builtin headers alone.
	Hosted bool

	// Epoch clamps __DATE__ and __TIME__. Nil means the caller supplied no
	// SOURCE_DATE_EPOCH; determinism then depends on the caller, so the CLI
	// always supplies one. Stored as a value, never read from the clock here.
	Epoch *time.Time

	// MaxIncludeDepth caps nesting. §5.2.4.1 guarantees 15; the limit exists
	// to turn a cyclic include into one diagnostic instead of a stack
	// overflow, so it is generous.
	MaxIncludeDepth int

	// MaxExpansionDepth caps nested expansion. Prosser's hide sets already
	// guarantee termination for well-formed input; this catches the input
	// that is not, and the combinatorial blowups that terminate but not in
	// this decade.
	MaxExpansionDepth int

	// TrackDeps records every file #include reached, for --deps.
	TrackDeps bool

	// KeepComments retains COMMENT tokens in the output. --emit i does not
	// want them dropped silently; the parser never sees them.
	KeepComments bool
}

// Default fills in the limits a caller left zero. It does not invent a search
// list, predefines or an epoch: those are the caller's to supply, and a zero
// Config preprocesses a self-contained file with no headers and no macros,
// which is exactly what a test wants.
func (c Config) Default() Config {
	if c.MaxIncludeDepth == 0 {
		c.MaxIncludeDepth = 200
	}
	if c.MaxExpansionDepth == 0 {
		c.MaxExpansionDepth = 4000
	}
	return c
}

// Now returns the time __DATE__ and __TIME__ expand against: the configured
// epoch, or the zero time when none was supplied. Nothing in this package
// reads a clock, so a build with no epoch is still deterministic per run and
// the CLI is the only place that could make it otherwise.
func (c Config) Now() time.Time {
	if c.Epoch != nil {
		return c.Epoch.UTC()
	}
	return time.Time{}
}

var _ = token.NoPos
