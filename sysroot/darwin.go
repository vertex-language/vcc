package sysroot

import "strings"

// darwinEntries resolves the macOS SDK. /usr/include does not exist
// on modern macOS; headers live only inside an SDK, found in the
// order the platform's own tools use:
//
//  1. $SDKROOT — set by xcrun and by Xcode-driven builds, so
//     honoring it means vcc composes under both;
//  2. `xcrun --show-sdk-path` — the authoritative answer. Apple owns
//     the developer-directory walk behind it (DEVELOPER_DIR, the
//     xcode-select symlink, the app-path fallbacks) and changes it
//     between releases, so vcc asks rather than reimplements;
//  3. the Command Line Tools SDK at its fixed path, for a machine
//     with the tools installed but xcrun not answering.
//
// /usr/local/include precedes the SDK, matching the platform's
// compilers — it is where Intel-Mac Homebrew installs. Apple-Silicon
// Homebrew's /opt/homebrew is deliberately not probed, also matching
// the platform's compilers: a package manager's prefix is the user's
// to name with -I.
func darwinEntries(h Host) (entries []Entry, notes []string) {
	entries = dirEntries(h, []string{"/usr/local/include"})

	sdk, _ := darwinSDK(h)

	switch {
	case sdk != "":
		entries = append(entries, dirEntries(h, []string{sdk + "/usr/include"})...)
	default:
		notes = append(notes,
			"no macOS SDK found: set SDKROOT, or install the command line tools (xcode-select --install)")
	}

	// Bare /usr/include: gone since macOS 10.14, but probing is free
	// and a machine that has one meant it.
	entries = append(entries, dirEntries(h, []string{"/usr/include"})...)
	return entries, notes
}

// SDK is the macOS SDK this host will compile and link against, found the way
// darwinEntries finds it. A nil Host is the real machine.
//
// It is exported because the SDK is not only the headers: a Mach-O link loads
// libSystem's stub out of the same directory, and resolving it twice by two
// different routes is how a machine ends up compiling against one SDK and
// failing to link against another.
func SDK(h Host) (string, bool) {
	if h == nil {
		h = osHost{}
	}
	return darwinSDK(h)
}

// darwinSDK is the three-step lookup: $SDKROOT, then xcrun, then the Command
// Line Tools SDK at its fixed path.
func darwinSDK(h Host) (string, bool) {
	if sdk := h.Getenv("SDKROOT"); sdk != "" {
		return sdk, true
	}
	if out, err := h.Run("xcrun", "--show-sdk-path"); err == nil {
		if sdk := strings.TrimSpace(out); sdk != "" {
			return sdk, true
		}
	}
	const clt = "/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk"
	if h.IsDir(clt) {
		return clt, true
	}
	return "", false
}
