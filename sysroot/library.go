package sysroot

import (
	"runtime"
	"strings"
)

// LibraryDirs is where this host keeps the target's libraries, in the order a
// -l name is looked for. It is Resolve's other half: headers are what a
// compilation needs from the platform, and these are what a link needs.
//
// The rules are Resolve's too. Directories are probed rather than assumed, so
// an absent one is an absent entry and no distro table decides anything. A
// freestanding link gets nothing: a program that names no libc names its own
// libraries, with -L.
//
// A nil Host is the real machine.
func LibraryDirs(h Host, target string, hosted bool) []string {
	if h == nil {
		h = osHost{}
	}
	return libraryDirs(h, runtime.GOOS, target, hosted)
}

// libraryDirs is LibraryDirs with the impurities injected, as resolve is to
// Resolve. Tests call this; nothing else should.
func libraryDirs(h Host, goos, target string, hosted bool) []string {
	if !hosted {
		return nil
	}
	var dirs []string
	switch goos {
	case "darwin":
		// The SDK is the only place a Mach-O link finds a system library:
		// /usr/lib holds no dylibs on a modern macOS, since the shared cache
		// replaced them, and what a link actually reads is the .tbd stub
		// beside the headers. Same SDK as the include list, by construction.
		dirs = append(dirs, "/usr/local/lib")
		if sdk, ok := darwinSDK(h); ok {
			dirs = append(dirs, sdk+"/usr/lib")
		}
	case "linux":
		// gcc's order with the gcc-isms removed. The multiarch pair is
		// Debian-family and simply does not exist elsewhere; lib64 is
		// Fedora's and Arch's, and is where a 64-bit libc lives there.
		dirs = append(dirs, "/usr/local/lib")
		if tuple := multiarch[archOf(target)]; tuple != "" {
			dirs = append(dirs, "/usr/lib/"+tuple, "/lib/"+tuple)
		}
		switch archOf(target) {
		case "x86_64", "aarch64", "riscv64":
			dirs = append(dirs, "/usr/lib64", "/lib64")
		case "i386":
			dirs = append(dirs, "/usr/lib32", "/lib32")
		}
		dirs = append(dirs, "/usr/lib", "/lib")
	case "windows":
		// %LIB% is what a vcvars environment resolves, exactly as %INCLUDE%
		// is for headers, and what vcc finds itself fills the same gaps —
		// the toolset and the SDK, split by architecture. windows.go says
		// why finding them is vcc's job rather than the environment's.
		dirs = windowsLibraryDirs(h, target)
	}

	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if h.IsDir(dir) {
			out = append(out, dir)
		}
	}
	return out
}

// DefaultLibraries is the C runtime a hosted link gets when the caller named
// none: the -l names a program that says nothing about libraries still has to
// be linked against for main to be reached and printf to exist.
//
// It is the platform's answer and not a preference, which is why it lives
// beside LibraryDirs rather than in the driver. The driver links it after
// whatever the caller named with -l, rather than instead of it: -l user32 is
// a program that wants user32 as well as a C runtime, not one that has
// undertaken to name its own. --freestanding is how a caller says none.
//
// Only Windows has an answer here today, and the reason the other two are
// empty is not that they need nothing. An ELF hosted link needs crt1.o, crti.o
// and crtn.o before it needs -lc, and a Mach-O one needs the SDK's start
// files; vcc does not supply startup objects yet, so listing -lc alone would
// turn "no libc" into "no _start" and answer nothing.
//
// A nil Host is the real machine.
func DefaultLibraries(h Host, target string, hosted bool) []string {
	if h == nil {
		h = osHost{}
	}
	return defaultLibraries(h, runtime.GOOS, target, hosted)
}

func defaultLibraries(h Host, goos, target string, hosted bool) []string {
	if !hosted {
		return nil
	}
	switch goos {
	case "windows":
		// The static CRT, which is cl.exe's own default and the one that
		// needs nothing installed to run: ucrtbase.dll ships with Windows
		// but vcruntime140.dll does not, so a link against the DLL runtime
		// produces a program that runs on the machine that built it and
		// nowhere else. kernel32 is what every one of them calls.
		//
		// The names carry the lib prefix because MSVC's convention is the
		// reverse of Unix's — foo.lib is the import library and libfoo.lib
		// the static one — so spelling the static form outright is the only
		// way to say which is meant without leaning on the search order.
		if !targetIsWindows(target) {
			return nil
		}
		// oldnames is where open, read, write, close, strdup and the rest
		// of the names the ucrt spells with a leading underscore are also
		// spelled without one. The header declares them as real functions
		// rather than macro-ing them, so the alias has to come from
		// somewhere, and this is the library MSVC links by default for it.
		// zlib's gzlib.c calls open(); without this it compiles and does
		// not link.
		return []string{"libucrt", "libvcruntime", "libcmt", "oldnames", "kernel32"}
	}
	return nil
}

// targetIsWindows reports whether a target name asks for a Windows link. A
// cross-build from Windows to Linux gets no MSVC runtime.
func targetIsWindows(target string) bool {
	return strings.HasSuffix(target, "-windows")
}
