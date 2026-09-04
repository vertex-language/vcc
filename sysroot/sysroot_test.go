package sysroot

import (
	"sort"
	"strings"
	"testing"
)

// fake is a host built from tables: env, directories, files, and the two
// tools this package asks anything of.
//
// Directories name themselves; the entries inside one are derived from the
// table rather than listed a second time, so a test that adds a version
// directory does not have to remember to add it twice.
type fake struct {
	env     map[string]string
	dirs    map[string]bool
	files   map[string]string
	xcrun   string // "" means the tool fails
	vswhere string // "" means the tool fails
}

func (f fake) Getenv(k string) string { return f.env[k] }
func (f fake) IsDir(p string) bool    { return f.dirs[p] }

func (f fake) ReadFile(p string) (string, error) {
	if s, ok := f.files[p]; ok {
		return s, nil
	}
	return "", errFake
}

// ReadDir returns the immediate children of p among the known directories.
func (f fake) ReadDir(p string) ([]string, error) {
	if !f.dirs[p] {
		return nil, errFake
	}
	var out []string
	for d := range f.dirs {
		rest, ok := strings.CutPrefix(d, p+`\`)
		if ok && !strings.Contains(rest, `\`) {
			out = append(out, rest)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (f fake) Run(name string, args ...string) (string, error) {
	switch {
	case name == "xcrun" && f.xcrun != "":
		return f.xcrun + "\n", nil
	case strings.HasSuffix(name, `\vswhere.exe`) && f.vswhere != "":
		return f.vswhere + "\r\n", nil
	}
	return "", errFake
}

// dirsUnder is the set of every prefix of each path, which is what a real
// filesystem gives ReadDir for free and a table has to be told.
func dirsUnder(paths ...string) map[string]bool {
	out := map[string]bool{}
	for _, p := range paths {
		for i := len(p); i > 0; i = strings.LastIndexByte(p[:i], '\\') {
			out[p[:i]] = true
		}
	}
	return out
}

var errFake = errString("fake: tool not available")

type errString string

func (e errString) Error() string { return string(e) }

func names(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFreestanding(t *testing.T) {
	es, notes := resolve(fake{}, "linux", "x86_64-linux", false)
	if !equal(names(es), []string{"<builtin>"}) || len(notes) != 0 {
		t.Fatalf("freestanding = %v (%v), want builtins alone", names(es), notes)
	}
}

func TestLinuxOrder(t *testing.T) {
	h := fake{dirs: map[string]bool{
		"/usr/local/include":            true,
		"/usr/include/x86_64-linux-gnu": true,
		"/usr/include":                  true,
	}}
	es, _ := resolve(h, "linux", "x86_64-linux", true)
	want := []string{"<builtin>", "/usr/local/include",
		"/usr/include/x86_64-linux-gnu", "/usr/include"}
	if !equal(names(es), want) {
		t.Fatalf("linux = %v, want %v", names(es), want)
	}
	for _, e := range es {
		if !e.System {
			t.Errorf("%s: System = false, want true", e.Name)
		}
	}
}

func TestLinuxNoMultiarch(t *testing.T) {
	// Fedora-shaped: no tuple directory. Absent directory, absent entry.
	h := fake{dirs: map[string]bool{"/usr/include": true}}
	es, _ := resolve(h, "linux", "x86_64-linux", true)
	if !equal(names(es), []string{"<builtin>", "/usr/include"}) {
		t.Fatalf("linux = %v", names(es))
	}
}

func TestDarwinSDKRootWinsOverXcrun(t *testing.T) {
	h := fake{
		env:   map[string]string{"SDKROOT": "/SDKs/MacOSX.sdk"},
		dirs:  map[string]bool{"/SDKs/MacOSX.sdk/usr/include": true},
		xcrun: "/WRONG.sdk", // must not be consulted
	}
	es, notes := resolve(h, "darwin", "aarch64-macos", true)
	if !equal(names(es), []string{"<builtin>", "/SDKs/MacOSX.sdk/usr/include"}) || len(notes) != 0 {
		t.Fatalf("darwin = %v (%v)", names(es), notes)
	}
}

func TestDarwinXcrunFallback(t *testing.T) {
	h := fake{
		dirs:  map[string]bool{"/X.sdk/usr/include": true},
		xcrun: "/X.sdk",
	}
	es, _ := resolve(h, "darwin", "aarch64-macos", true)
	if !equal(names(es), []string{"<builtin>", "/X.sdk/usr/include"}) {
		t.Fatalf("darwin = %v", names(es))
	}
}

func TestDarwinNoSDKNotes(t *testing.T) {
	es, notes := resolve(fake{}, "darwin", "aarch64-macos", true)
	if !equal(names(es), []string{"<builtin>"}) || len(notes) != 1 {
		t.Fatalf("darwin = %v (%v), want builtins plus one note", names(es), notes)
	}
}

// The paths a real installation uses, short enough to read in a failure.
const (
	vsRoot  = `C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools`
	msvc    = vsRoot + `\VC\Tools\MSVC\14.44.35207`
	kits    = `C:\Program Files (x86)\Windows Kits\10`
	sdkVer  = "10.0.26100.0"
	sdkInc  = kits + `\Include\` + sdkVer
	sdkLibs = kits + `\Lib\` + sdkVer
)

// windowsHost is a machine with both installations and nothing in the
// environment: an ordinary shell, which is where vcc has to do the work.
func windowsHost() fake {
	dirs := dirsUnder(
		msvc+`\include`, msvc+`\lib\x64`,
		sdkInc+`\ucrt`, sdkInc+`\shared`, sdkInc+`\um`, sdkInc+`\winrt`, sdkInc+`\cppwinrt`,
		sdkLibs+`\ucrt\x64`, sdkLibs+`\um\x64`,
	)
	return fake{
		env:  map[string]string{"ProgramFiles(x86)": `C:\Program Files (x86)`},
		dirs: dirs,
		files: map[string]string{
			vsRoot + `\VC\Auxiliary\Build\Microsoft.VCToolsVersion.default.txt`: "14.44.35207\r\n",
		},
		vswhere: vsRoot,
	}
}

func windowsWant() []string {
	return []string{"<builtin>", msvc + `\include`,
		sdkInc + `\ucrt`, sdkInc + `\shared`, sdkInc + `\um`,
		sdkInc + `\winrt`, sdkInc + `\cppwinrt`}
}

// A plain shell: no INCLUDE, both installations found by vswhere and by
// probing the Kits root.
func TestWindowsDiscovery(t *testing.T) {
	es, notes := resolve(windowsHost(), "windows", "x86_64-windows", true)
	if !equal(names(es), windowsWant()) || len(notes) != 0 {
		t.Fatalf("windows = %v (%v), want %v", names(es), notes, windowsWant())
	}
	for _, e := range es[1:] {
		if !e.System {
			t.Errorf("%s: System = false, want true", e.Name)
		}
	}
}

// A Developer Command Prompt: INCLUDE names exactly what discovery would,
// so the list is INCLUDE's and nothing is repeated.
func TestWindowsVcvarsIsNotDoubled(t *testing.T) {
	h := windowsHost()
	h.env["INCLUDE"] = strings.Join([]string{
		msvc + `\include`, sdkInc + `\ucrt`, sdkInc + `\shared`,
		sdkInc + `\um`, sdkInc + `\winrt`, sdkInc + `\cppwinrt`,
	}, ";")
	h.env["VCToolsInstallDir"] = msvc + `\`
	h.env["WindowsSdkDir"] = kits + `\`
	h.env["WindowsSDKVersion"] = sdkVer + `\`

	es, notes := resolve(h, "windows", "x86_64-windows", true)
	if !equal(names(es), windowsWant()) || len(notes) != 0 {
		t.Fatalf("windows = %v (%v), want %v", names(es), notes, windowsWant())
	}
}

// A Developer Command Prompt whose SDK half came up empty — vcvars sets the
// toolset and no ucrt, which is what a machine gets when its SDK arrived
// outside the Visual Studio installer. The environment's answer stays
// first; the missing half is appended.
func TestWindowsVcvarsWithoutSDK(t *testing.T) {
	h := windowsHost()
	h.env["INCLUDE"] = msvc + `\include`
	h.env["VCToolsInstallDir"] = msvc + `\`

	es, notes := resolve(h, "windows", "x86_64-windows", true)
	if !equal(names(es), windowsWant()) || len(notes) != 0 {
		t.Fatalf("windows = %v (%v), want %v", names(es), notes, windowsWant())
	}
}

// An uninstalled SDK leaves its version directory behind with no ucrt in
// it. Newest-wins must skip it, or every #include reports missing.
func TestWindowsSkipsEmptySDKVersion(t *testing.T) {
	h := windowsHost()
	for _, d := range []string{kits + `\Include\10.0.99999.0`, kits + `\Include\10.0.99999.0\um`} {
		h.dirs[d] = true
	}
	es, _ := resolve(h, "windows", "x86_64-windows", true)
	if !equal(names(es), windowsWant()) {
		t.Fatalf("windows = %v, want the version that still has a ucrt", names(es))
	}
}

// Versions compare by component: 9841 is older than 26100, and string
// order says the opposite.
func TestWindowsSDKVersionIsNumeric(t *testing.T) {
	h := windowsHost()
	old := kits + `\Include\10.0.9841.0`
	for _, d := range []string{old, old + `\ucrt`, old + `\um`} {
		h.dirs[d] = true
	}
	es, _ := resolve(h, "windows", "x86_64-windows", true)
	if !equal(names(es), windowsWant()) {
		t.Fatalf("windows = %v, want %s", names(es), sdkVer)
	}
}

// Neither installation present: builtins alone, and a note for each half.
func TestWindowsNoToolchain(t *testing.T) {
	es, notes := resolve(fake{}, "windows", "x86_64-windows", true)
	if !equal(names(es), []string{"<builtin>"}) || len(notes) != 2 {
		t.Fatalf("windows = %v (%v), want builtins plus two notes", names(es), notes)
	}
}

// The library half: %LIB% first, then the toolset and the SDK, split by
// architecture where the headers are not.
func TestWindowsLibraryDirs(t *testing.T) {
	got := libraryDirs(windowsHost(), "windows", "x86_64-windows", true)
	want := []string{msvc + `\lib\x64`, sdkLibs + `\ucrt\x64`, sdkLibs + `\um\x64`}
	if !equal(got, want) {
		t.Fatalf("libs = %v, want %v", got, want)
	}
}

// A cross-build from Windows gets no MSVC libraries, the same answer
// defaultLibraries gives it.
func TestWindowsLibraryDirsCrossTarget(t *testing.T) {
	if got := libraryDirs(windowsHost(), "windows", "x86_64-linux", true); len(got) != 0 {
		t.Fatalf("libs = %v, want none for a cross target", got)
	}
}

func TestUserPathIsNotSystem(t *testing.T) {
	h := fake{
		env:  map[string]string{"VCC_INCLUDE_PATH": "/opt/mylibs/include"},
		dirs: map[string]bool{"/opt/mylibs/include": true, "/usr/include": true},
	}
	es, _ := resolve(h, "linux", "x86_64-linux", true)
	want := []string{"/opt/mylibs/include", "<builtin>", "/usr/include"}
	if !equal(names(es), want) {
		t.Fatalf("order = %v, want %v", names(es), want)
	}
	if es[0].System {
		t.Error("VCC_INCLUDE_PATH entry marked System; user dirs must warn every time")
	}
}
