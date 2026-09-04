package vcc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The resolution rule, without a linker: which filename wins, in which
// directory, and what the failure says.

func touch(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("not really a library"), 0o666); err != nil {
		t.Fatal(err)
	}
	return path
}

func machoTarget(t *testing.T) Target {
	t.Helper()
	tgt, ok := LookupTarget("aarch64-macos")
	if !ok {
		t.Fatal("aarch64-macos is not modelled")
	}
	return tgt
}

func elfTarget(t *testing.T) Target {
	t.Helper()
	tgt, ok := LookupTarget("x86_64-linux")
	if !ok {
		t.Fatal("x86_64-linux is not modelled")
	}
	return tgt
}

// The shared form wins over the archive, and the platform's spelling is the
// target's rather than the host's.
func TestLibraryNamePreference(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "libfoo.a")
	tbd := touch(t, dir, "libfoo.tbd")

	got, err := findLibrary(machoTarget(t), "foo", []string{dir}, false)
	if err != nil {
		t.Fatalf("findLibrary: %v", err)
	}
	if got.Name != tbd {
		t.Errorf("resolved %s, want the stub at %s", got.Name, tbd)
	}

	// Static asks for the archive, and asks for nothing else.
	got, err = findLibrary(machoTarget(t), "foo", []string{dir}, true)
	if err != nil {
		t.Fatalf("findLibrary static: %v", err)
	}
	if filepath.Base(got.Name) != "libfoo.a" {
		t.Errorf("static resolved %s, want libfoo.a", got.Name)
	}

	// An ELF target never looks for a .tbd, so the archive is all there is.
	got, err = findLibrary(elfTarget(t), "foo", []string{dir}, false)
	if err != nil {
		t.Fatalf("findLibrary elf: %v", err)
	}
	if filepath.Base(got.Name) != "libfoo.a" {
		t.Errorf("elf resolved %s, want libfoo.a", got.Name)
	}
}

// A directory earlier in the list wins, even against a better filename later:
// -L is an override, not a hint.
func TestLibraryDirectoryOrder(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	want := touch(t, first, "libfoo.a")
	touch(t, second, "libfoo.tbd")

	got, err := findLibrary(machoTarget(t), "foo", []string{first, second}, false)
	if err != nil {
		t.Fatalf("findLibrary: %v", err)
	}
	if got.Name != want {
		t.Errorf("resolved %s, want %s", got.Name, want)
	}
}

// The library is read at resolution, so what reaches the linker is bytes and
// not a path it has to open for itself.
func TestLibraryIsReadNotNamed(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "libfoo.a")

	got, err := findLibrary(elfTarget(t), "foo", []string{dir}, false)
	if err != nil {
		t.Fatalf("findLibrary: %v", err)
	}
	if len(got.Data) == 0 {
		t.Error("resolved library carries no bytes")
	}
}

// The failure names what it looked for and where. "cannot find -lm" alone
// sends the reader to strace.
func TestLibraryNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := findLibrary(machoTarget(t), "m", []string{dir}, false)
	if err == nil {
		t.Fatal("no error for a library that does not exist")
	}
	for _, want := range []string{"cannot find -lm", "libm.tbd", "libm.dylib", "libm.a", dir} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q is missing %q", err, want)
		}
	}

	// With nowhere to look, say that rather than printing an empty list.
	_, err = findLibrary(elfTarget(t), "m", nil, false)
	if err == nil || !strings.Contains(err.Error(), "-L") {
		t.Errorf("error = %v, want it to name the flag that fixes it", err)
	}
}

// -L comes before the platform's own directories, and a freestanding link has
// no platform directories at all.
func TestLibrarySearchList(t *testing.T) {
	tgt, ok := HostTarget()
	if !ok {
		t.Skip("this host is not a target vcc models")
	}
	p := linkParams{LibDirs: []string{"/opt/mine"}}
	dirs := p.libraryDirs(tgt)
	if len(dirs) == 0 || dirs[0] != "/opt/mine" {
		t.Errorf("search list = %v, want it to start with -L", dirs)
	}

	p.Freestanding = true
	dirs = p.libraryDirs(tgt)
	if len(dirs) != 1 || dirs[0] != "/opt/mine" {
		t.Errorf("freestanding search list = %v, want the caller's alone", dirs)
	}
}

// The default C runtime is linked after whatever -l named, not instead of
// it: `vcc build -l user32 hello.c` is a program that wants user32 as well
// as a C runtime. -freestanding is how a caller says none.
func TestDefaultRuntimeIsAppended(t *testing.T) {
	tgt, ok := LookupTarget("x86_64-windows")
	if !ok {
		t.Fatal("x86_64-windows is not modelled")
	}
	// The name resolution below needs a real directory, so the check is on
	// the list this builds rather than on the files it would find. What
	// libraries() does with the names is TestLibraryNamePreference's.
	def := defaultLibs(t, tgt, nil)
	if len(def) == 0 {
		t.Fatal("a hosted Windows link gets no default runtime")
	}

	got := defaultLibs(t, tgt, []string{"user32"})
	if len(got) != len(def)+1 || got[0] != "user32" {
		t.Fatalf("with -l user32 = %v, want user32 then %v", got, def)
	}

	// A runtime the caller named is not added twice, and keeps its place.
	got = defaultLibs(t, tgt, []string{def[0], "user32"})
	if len(got) != len(def)+1 || got[0] != def[0] || got[1] != "user32" {
		t.Fatalf("with -l %s = %v, want it once and first", def[0], got)
	}

	if got := defaultLibs(t, tgt, nil, freestanding); len(got) != 0 {
		t.Fatalf("freestanding = %v, want no libraries", got)
	}
}

type linkOpt func(*linkParams)

func freestanding(p *linkParams) { p.Freestanding = true }

// defaultLibs is the -l list one link resolves, names only.
func defaultLibs(t *testing.T, tgt Target, libs []string, opts ...linkOpt) []string {
	t.Helper()
	p := linkParams{Libs: libs}
	for _, o := range opts {
		o(&p)
	}
	return p.libraryNames(tgt)
}
