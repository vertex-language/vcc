// Package sysroot answers the one question phase 4 cannot: where does
// this host keep the target's headers?
//
// Resolve produces the ordered include list for a hosted compilation:
// vcc's builtin headers first, then the platform's well-known
// directories, probed by stat rather than by distro table — an absent
// directory is an absent entry. The result is data: the vcc package
// converts it into preprocessor.Config.Search, and `vcc env` prints it
// before the build runs.
//
// This package imports the standard library only, and is imported by
// the vcc package alone. The preprocessor never learns what a distro
// is; this package never learns what a token is.
package sysroot

import (
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Entry is one resolved include directory: a filesystem, the name a
// path resolved against it is known by, and whether its headers are
// the system's rather than the user's.
//
// It mirrors preprocessor.Mount field for field, deliberately, but
// this package does not import preprocessor — the conversion is one
// loop in the vcc package, and it keeps the dependency arrow pointing the
// right way: sysroot is below the CLI, beside nothing.
type Entry struct {
	Name   string
	FS     fs.FS
	System bool
}

// Host is everything Resolve reads from the machine: environment
// variables, directory existence and contents, one small file, and one
// tool's output (xcrun, vswhere).
//
// The indirection exists because probing is impure by nature but the
// ordering logic is not. With Host injected, resolve is a pure
// function a test can drive as a Linux box with no multiarch, a Mac
// with no Xcode, or a Windows shell with no vcvars — on any machine.
//
// ReadDir and ReadFile exist for one platform. Linux and macOS name their
// header directories outright; Windows versions its, so the MSVC toolset
// and the Windows SDK are found under a directory whose name is a version
// number that has to be read to be known.
type Host interface {
	// Getenv returns the named variable, "" when unset.
	Getenv(key string) string
	// IsDir reports whether path exists and is a directory.
	IsDir(path string) bool
	// ReadDir returns the names of the entries in a directory, in any
	// order. An unreadable directory returns no names and an error.
	ReadDir(path string) ([]string, error)
	// ReadFile returns the contents of a small file.
	ReadFile(path string) (string, error)
	// Run executes a tool and returns its standard output.
	Run(name string, args ...string) (string, error)
}

// osHost is the real machine.
type osHost struct{}

func (osHost) Getenv(key string) string { return os.Getenv(key) }

func (osHost) IsDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func (osHost) ReadDir(path string) ([]string, error) {
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names, nil
}

func (osHost) ReadFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func (osHost) Run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

// Resolve produces the include search list for target on this host,
// in order, plus notes worth surfacing when something expected was
// not found (no SDK, no vcvars environment). Notes are advice for
// `vcc env` and diagnostics, never errors: a host with no system
// headers still resolves — to the builtins — and the failure that
// matters is the #include that does not find its file, reported
// there, with this list to point at.
//
// The order is the one preprocessor.Config.Search documents:
// VCC_INCLUDE_PATH, then vcc's builtin headers, then the platform's.
// (-I directories precede all of this; they are the CLI's to prepend.)
func Resolve(target string, hosted bool) (entries []Entry, notes []string) {
	return ResolveWith(nil, target, hosted)
}

// ResolveWith is Resolve with the host injected: a nil Host is the real
// machine, and any other is asked instead of it. A caller that supplies one
// resolves headers without reading this machine's environment or filesystem
// at all, which is what makes a hermetic build hermetic.
func ResolveWith(h Host, target string, hosted bool) (entries []Entry, notes []string) {
	if h == nil {
		h = osHost{}
	}
	return resolve(h, runtime.GOOS, target, hosted)
}

// resolve is Resolve with the impurities injected: the host to probe
// and the OS to probe it as. Tests call this; nothing else should.
func resolve(h Host, goos, target string, hosted bool) (entries []Entry, notes []string) {
	if !hosted {
		// --freestanding is step 1 alone: the headers ISO requires of
		// a freestanding implementation are exactly the ones vcc
		// carries in the binary.
		return []Entry{builtinEntry()}, nil
	}

	// VCC_INCLUDE_PATH holds user directories, so its entries are not
	// System: a warning in one is the user's to see every time.
	for _, dir := range splitList(h.Getenv("VCC_INCLUDE_PATH"), goos) {
		if h.IsDir(dir) {
			entries = append(entries, Entry{Name: dir, FS: os.DirFS(dir)})
		}
	}

	entries = append(entries, builtinEntry())

	var plat []Entry
	var pnotes []string
	switch goos {
	case "linux":
		plat = linuxEntries(h, target)
	case "darwin":
		plat, pnotes = darwinEntries(h)
	case "windows":
		plat, pnotes = windowsEntries(h)
	default:
		pnotes = []string{"hosted compilation is not wired up for " + goos + "; system headers need -I"}
	}
	return append(entries, plat...), append(notes, pnotes...)
}

// dirEntries probes a list of directories in order and mounts the
// ones that exist. All platform directories are System: their headers
// are not the user's code, so warnings sited in them report once per
// header and the tolerated-spelling carve-out applies.
func dirEntries(h Host, dirs []string) []Entry {
	var out []Entry
	for _, dir := range dirs {
		if h.IsDir(dir) {
			out = append(out, Entry{Name: dir, FS: os.DirFS(dir), System: true})
		}
	}
	return out
}

// splitList splits a PATH-shaped variable with the separator of the
// OS being resolved for — a parameter, not runtime.GOOS, so tests can
// exercise the Windows split on any machine.
func splitList(s, goos string) []string {
	if s == "" {
		return nil
	}
	sep := ":"
	if goos == "windows" {
		sep = ";"
	}
	var out []string
	for _, d := range strings.Split(s, sep) {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// archOf extracts the architecture from a target string. The target
// vocabulary is arc's — "x86_64-elf", "aarch64-linux" — and the
// architecture is everything before the first dash.
func archOf(target string) string {
	if i := strings.IndexByte(target, '-'); i >= 0 {
		return target[:i]
	}
	return target
}
