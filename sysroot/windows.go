package sysroot

import (
	"strconv"
	"strings"
)

// The Windows toolchain is two installations, not one. The MSVC toolset
// carries the compiler's own headers and libraries — the C++ standard
// library, the vcruntime, the intrinsics — and the Windows SDK carries the
// platform's: ucrt (the C standard library), shared and um (the Win32 API),
// winrt and cppwinrt (C++-only, but vcc opens only what is #include'd, so
// they cost nothing). Both are versioned, side by side, under directories
// named for the version, and neither can be named without being looked up.
//
// vcvars64.bat resolves both into %INCLUDE% and %LIB%, and where it has
// there is nothing to do. Where it has not — an ordinary shell, or a
// Developer Command Prompt whose SDK half came up empty, which is what a
// machine whose SDK was installed outside the Visual Studio installer
// gets — vcc finds them the way the platform's own tools do, and the rest
// of this file is that walk.

// windowsEntries resolves the include directories of the MSVC toolset and
// the Windows SDK.
//
// %INCLUDE% comes first: it is what a vcvars environment answers this
// question with, so honoring it means vcc composes inside one, and a
// caller who set it by hand has said which headers to use.
//
// What vcc finds itself is appended rather than substituted, because
// %INCLUDE% is not always the whole answer. vcvars derives its SDK half
// from a registry value its own installer does not always write, and the
// Developer Command Prompt that results holds the toolset and no stdio.h
// anywhere in it. Appending repairs that shell without overriding it: a
// directory the environment already named is not repeated, so a complete
// %INCLUDE% leaves nothing to add.
func windowsEntries(h Host) (entries []Entry, notes []string) {
	dirs := splitList(h.Getenv("INCLUDE"), "windows")

	tc := windowsToolchain(h)
	if tc.msvc != "" {
		dirs = appendDir(dirs, winJoin(tc.msvc, "include"))
	}
	if inc := tc.sdkInclude(); inc != "" {
		for _, sub := range []string{"ucrt", "shared", "um", "winrt", "cppwinrt"} {
			dirs = appendDir(dirs, winJoin(inc, sub))
		}
	}

	if tc.msvc == "" {
		notes = append(notes,
			"no MSVC toolset found: install the Visual Studio Build Tools with the C++ workload, or run vcc from a Developer Command Prompt")
	}
	if tc.sdkInclude() == "" {
		notes = append(notes,
			"no Windows SDK found: install it alongside the Build Tools, or set WindowsSdkDir and WindowsSDKVersion")
	}
	if len(dirs) == 0 {
		return nil, notes
	}
	return dirEntries(h, dirs), notes
}

// windowsLibraryDirs is LibraryDirs' Windows half, and windowsEntries' rule
// applied to the other list: %LIB% first, then what vcc finds, deduplicated.
//
// The toolset and the SDK split their libraries by architecture where they
// do not split their headers, so this half needs the target and that one
// does not. A target this host has no architecture name for — a Linux
// cross-build from Windows — gets neither installation's libraries, which
// is the same answer defaultLibraries gives it.
func windowsLibraryDirs(h Host, target string) []string {
	dirs := splitList(h.Getenv("LIB"), "windows")

	arch := msvcArch(target)
	if arch == "" || !targetIsWindows(target) {
		return dirs
	}

	tc := windowsToolchain(h)
	if tc.msvc != "" {
		dirs = appendDir(dirs, winJoin(tc.msvc, "lib", arch))
	}
	if lib := tc.sdkLib(); lib != "" {
		dirs = appendDir(dirs, winJoin(lib, "ucrt", arch), winJoin(lib, "um", arch))
	}
	return dirs
}

// toolchain is where this host keeps the two installations: the MSVC
// toolset directory, and the Windows Kits root plus the SDK version chosen
// inside it. Any field may be empty; every reader checks.
type toolchain struct {
	msvc       string // ...\VC\Tools\MSVC\14.44.35207
	sdk        string // ...\Windows Kits\10
	sdkVersion string // 10.0.26100.0
}

// sdkInclude and sdkLib are the versioned roots the SDK's own subdirectories
// hang off, or "" when no SDK was found.
func (t toolchain) sdkInclude() string { return t.sdkDir("Include") }
func (t toolchain) sdkLib() string     { return t.sdkDir("Lib") }

func (t toolchain) sdkDir(which string) string {
	if t.sdk == "" || t.sdkVersion == "" {
		return ""
	}
	return winJoin(t.sdk, which, t.sdkVersion)
}

// windowsToolchain locates both installations. It is called once per list
// and does not cache: the two calls a build makes are cheap — every step
// but the last reads the environment — and a cache would have to decide
// when a toolchain installed mid-process stops being stale.
func windowsToolchain(h Host) toolchain {
	var t toolchain
	t.msvc = msvcDir(h)
	t.sdk = sdkRoot(h)
	t.sdkVersion = sdkVersion(h, t.sdk)
	return t
}

// msvcDir is the MSVC toolset directory, found in the order the platform's
// own tools use:
//
//  1. %VCToolsInstallDir% — set by vcvars, and the whole answer when it is;
//  2. %VCINSTALLDIR% or %VSINSTALLDIR%, also vcvars', which name the
//     installation but not the toolset inside it;
//  3. vswhere.exe, at the fixed path Microsoft documents for exactly this.
//     It is Windows' xcrun: Microsoft owns the walk over editions,
//     channels, and side-by-side instances behind it, and changes it
//     between releases, so vcc asks rather than reimplements.
//
// Steps 2 and 3 land on an installation root, and the toolset version
// inside it is the one the installation itself names in
// Microsoft.VCToolsVersion.default.txt — the file vcvars reads for the
// same purpose — or the newest directory present when that file is not.
func msvcDir(h Host) string {
	if dir := trimSep(h.Getenv("VCToolsInstallDir")); dir != "" && h.IsDir(dir) {
		return dir
	}

	vc := trimSep(h.Getenv("VCINSTALLDIR"))
	if vc == "" {
		if vs := trimSep(h.Getenv("VSINSTALLDIR")); vs != "" {
			vc = winJoin(vs, "VC")
		}
	}
	if vc == "" {
		if vs := vswhere(h); vs != "" {
			vc = winJoin(vs, "VC")
		}
	}
	if vc == "" || !h.IsDir(vc) {
		return ""
	}

	tools := winJoin(vc, "Tools", "MSVC")
	if v, err := h.ReadFile(winJoin(vc, "Auxiliary", "Build", "Microsoft.VCToolsVersion.default.txt")); err == nil {
		if v = strings.TrimSpace(v); v != "" && h.IsDir(winJoin(tools, v)) {
			return winJoin(tools, v)
		}
	}
	if v := newestVersion(h, tools, func(string) bool { return true }); v != "" {
		return winJoin(tools, v)
	}
	return ""
}

// vswhere asks Visual Studio's own locator for the newest installation that
// carries a C++ toolset. Its path is fixed by contract — Microsoft ships it
// there so that a build system has somewhere to start — which is what makes
// asking it cheaper than reimplementing it.
//
// A missing vswhere, a machine with no Visual Studio, and a vswhere that
// fails are one answer here: "", and the note that follows from it.
func vswhere(h Host) string {
	pf := h.Getenv("ProgramFiles(x86)")
	if pf == "" {
		pf = `C:\Program Files (x86)`
	}
	exe := winJoin(pf, "Microsoft Visual Studio", "Installer", "vswhere.exe")
	out, err := h.Run(exe,
		"-latest", "-products", "*",
		"-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64",
		"-property", "installationPath")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// sdkRoot is the Windows Kits root: %WindowsSdkDir% when vcvars resolved
// one, and the installer's own location under Program Files (x86) when it
// did not. The registry holds the same path under Installed Roots, and
// reading it would mean either a syscall this package does not make or
// shelling out to reg.exe to learn a directory that is already there to
// stat — so vcc stats it.
func sdkRoot(h Host) string {
	if dir := trimSep(h.Getenv("WindowsSdkDir")); dir != "" && h.IsDir(dir) {
		return dir
	}
	pf := h.Getenv("ProgramFiles(x86)")
	if pf == "" {
		pf = `C:\Program Files (x86)`
	}
	if dir := winJoin(pf, "Windows Kits", "10"); h.IsDir(dir) {
		return dir
	}
	return ""
}

// sdkVersion is the SDK version to compile against: %WindowsSDKVersion%,
// which vcvars sets with a trailing separator, else the newest version
// installed under the root.
//
// "Installed" is the operative word. Uninstalling an SDK leaves its version
// directory behind with the headers gone, and a newest-wins pick that did
// not look inside would choose the empty one and report every #include
// missing. A version counts when its ucrt directory is there, which is the
// half a hosted compilation cannot proceed without.
func sdkVersion(h Host, root string) string {
	if root == "" {
		return ""
	}
	include := winJoin(root, "Include")
	if v := trimSep(h.Getenv("WindowsSDKVersion")); v != "" && h.IsDir(winJoin(include, v, "ucrt")) {
		return v
	}
	return newestVersion(h, include, func(v string) bool {
		return h.IsDir(winJoin(include, v, "ucrt"))
	})
}

// newestVersion is the highest-numbered subdirectory of dir that ok accepts.
//
// The comparison is numeric per component rather than lexical, because the
// names being compared are version numbers and string order gets them
// wrong: "10.0.9841.0" sorts above "10.0.26100.0" and is four years older.
func newestVersion(h Host, dir string, ok func(string) bool) string {
	names, err := h.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	for _, n := range names {
		if !isVersion(n) || !h.IsDir(winJoin(dir, n)) || !ok(n) {
			continue
		}
		if best == "" || compareVersion(n, best) > 0 {
			best = n
		}
	}
	return best
}

// isVersion reports whether a directory name is a dotted number, which is
// what separates a version from the other things an installation root
// holds ("Auxiliary", "Redist", a stray "backup").
func isVersion(s string) bool {
	if s == "" {
		return false
	}
	for _, part := range strings.Split(s, ".") {
		if part == "" {
			return false
		}
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

// compareVersion orders two dotted-numeric versions, treating an absent
// component as zero so that 14.44 and 14.44.0 compare equal.
func compareVersion(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		x, y := versionPart(as, i), versionPart(bs, i)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.Atoi(parts[i])
	return n
}

// msvcArch is the directory name the toolset and the SDK give a target
// architecture. Both spell x86_64 "x64" and i386 "x86"; a target neither
// has a name for gets "".
func msvcArch(target string) string {
	switch archOf(target) {
	case "x86_64":
		return "x64"
	case "i386":
		return "x86"
	case "aarch64":
		return "arm64"
	}
	return ""
}

// appendDir appends the directories not already in dirs, compared the way
// Windows compares paths: case-insensitively, and with either separator.
//
// It is what keeps the environment's answer authoritative. A vcvars
// %INCLUDE% and vcc's own walk name the same directories by construction,
// so without this the complete case would list every one of them twice.
func appendDir(dirs []string, add ...string) []string {
	for _, d := range add {
		if d == "" || containsDir(dirs, d) {
			continue
		}
		dirs = append(dirs, d)
	}
	return dirs
}

func containsDir(dirs []string, want string) bool {
	want = normDir(want)
	for _, d := range dirs {
		if normDir(d) == want {
			return true
		}
	}
	return false
}

func normDir(s string) string {
	return strings.ToLower(trimSep(strings.ReplaceAll(s, "/", `\`)))
}

// trimSep drops the trailing separators Windows environment variables carry:
// %VCToolsInstallDir% and %WindowsSDKVersion% are both set with one, and a
// path joined onto them without this grows an empty component.
func trimSep(s string) string { return strings.TrimRight(s, `\/`) }

// winJoin joins path elements with a backslash.
//
// It is not filepath.Join, and the reason is the same one splitList takes a
// goos parameter: this walk is exercised by tests on machines that are not
// Windows, where filepath would join with that machine's separator and
// compare unequal to every path the test wrote down.
func winJoin(elem ...string) string {
	var out string
	for _, e := range elem {
		switch e = trimSep(e); {
		case e == "":
		case out == "":
			out = e
		default:
			out += `\` + strings.TrimLeft(e, `\/`)
		}
	}
	return out
}
