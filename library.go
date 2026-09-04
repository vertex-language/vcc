package vcc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vertex-language/vcc/sysroot"
)

// -l and -L: how a library name becomes a file.
//
// The vertex-language linkers take bytes, and only the PE one has a search
// path of its own. So resolving a -l name is vcc's job — which is the right
// place for it anyway, because the spelling a name resolves through is a fact
// of the target's platform and vcc is what knows the target.
//
// The rule is the one every C toolchain has: -L directories in the order they
// were given, then the platform's own, and within each directory the shared
// form before the static one unless the link is static. A name that resolves
// in an earlier directory wins; a name that resolves nowhere is an error
// naming what was looked for and where, because "cannot find -lm" without the
// search list is a message that sends the reader to strace.

// libraryDirs is where a -l name is looked for: the caller's first.
func (p linkParams) libraryDirs(t Target) []string {
	dirs := make([]string, 0, len(p.LibDirs)+4)
	dirs = append(dirs, p.LibDirs...)
	return append(dirs, sysroot.LibraryDirs(p.Host, t.Name(), !p.Freestanding)...)
}

// libraryNames is the filenames "-l name" can mean on this target, in the
// order they are tried.
//
// Mach-O looks for a .tbd first because that is what a modern macOS SDK ships
// and what the linker can actually read: the dylib itself lives in the shared
// cache, and a .dylib on disk needs an exports reader that is not written.
// Static asks for the archive alone, everywhere: it is the whole content of
// the request.
func libraryNames(t Target, name string, static bool) []string {
	archive := "lib" + name + ".a"
	if t.format == FormatPE {
		// MSVC's convention is the reverse of Unix's: foo.lib is the import
		// library that binds to foo.dll, and libfoo.lib is the static one.
		// So the shared form is the bare name and the static form is the one
		// carrying the prefix, and -static reverses this pair rather than
		// cutting it down to an archive — there is no ".a" in a Microsoft
		// toolchain at all. MinGW's lib<name>.a is tried last either way.
		if static {
			return []string{"lib" + name + ".lib", archive, name + ".lib"}
		}
		return []string{name + ".lib", "lib" + name + ".lib", archive}
	}
	if static {
		return []string{archive}
	}
	switch t.format {
	case FormatMachO:
		return []string{"lib" + name + ".tbd", "lib" + name + ".dylib", archive}
	case FormatELF:
		return []string{"lib" + name + ".so", archive}
	}
	return []string{archive}
}

// findLibrary resolves one -l name against the search list and reads it.
func findLibrary(t Target, name string, dirs []string, static bool) (Input, error) {
	names := libraryNames(t, name, static)
	for _, dir := range dirs {
		for _, base := range names {
			path := filepath.Join(dir, base)
			data, err := os.ReadFile(path)
			switch {
			case err == nil:
				return Input{Name: path, Data: data}, nil
			case os.IsNotExist(err):
				continue
			default:
				// Found and unreadable is not "keep looking": a directory the
				// caller named holds a library it cannot open, and searching
				// past it would report the wrong problem.
				return Input{}, fmt.Errorf("%s: %w", path, err)
			}
		}
	}
	return Input{}, fmt.Errorf("cannot find -l%s: no %s in %s",
		name, orList(names), dirList(dirs))
}

// libraries resolves every -l name for one link, in order: the ones the
// caller named, then the platform's default C runtime.
//
// The runtime comes last and comes always, because a program that says
// nothing about libraries still has to reach main and a program that does
// say something still has to. `vcc build -l user32 hello.c` is the ordinary
// Windows link, and a rule that read one -l as "and nothing else" would take
// printf away from it. -freestanding is how a caller says none, and a
// runtime the caller already named is not added twice.
//
// Last is also where a static link wants it: an archive satisfies the
// references to its left, so the CRT after the libraries that call into it
// resolves and the reverse does not. See sysroot.DefaultLibraries.
func (p linkParams) libraries(t Target) ([]Input, error) {
	names := p.libraryNames(t)
	if len(names) == 0 {
		return nil, nil
	}
	dirs := p.libraryDirs(t)
	out := make([]Input, 0, len(names))
	for _, name := range names {
		lib, err := findLibrary(t, name, dirs, p.Static)
		if err != nil {
			return nil, err
		}
		out = append(out, lib)
	}
	return out, nil
}

// libraryNames is the -l list one link resolves, in order: the caller's, then
// the platform's default runtime for the ones it did not already name.
func (p linkParams) libraryNames(t Target) []string {
	names := append([]string(nil), p.Libs...)
	named := make(map[string]bool, len(names))
	for _, n := range names {
		named[n] = true
	}
	for _, n := range sysroot.DefaultLibraries(p.Host, t.Name(), !p.Freestanding) {
		if !named[n] {
			names = append(names, n)
		}
	}
	return names
}

func orList(names []string) string {
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " or " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

func dirList(dirs []string) string {
	if len(dirs) == 0 {
		return "no library directories (name one with -L)"
	}
	return strings.Join(dirs, ", ")
}
