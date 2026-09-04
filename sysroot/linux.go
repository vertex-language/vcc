package sysroot

// multiarch maps an architecture to Debian's multiarch tuple: the
// directory name under /usr/include where Debian-family distros keep
// the architecture-specific headers (<asm/...> lives there, and glibc
// headers reach it). The tuples are Debian's normalized GNU triplets;
// note armhf, where the hard-float ABI rides the suffix, not the
// vendor field.
//
// Fedora, Arch, and Alpine do not use multiarch. No table entry or
// distro check handles that: the directory does not exist there, and
// an absent directory is an absent entry.
var multiarch = map[string]string{
	"x86_64":  "x86_64-linux-gnu",
	"i386":    "i386-linux-gnu",
	"aarch64": "aarch64-linux-gnu",
	"arm":     "arm-linux-gnueabihf",
	"riscv64": "riscv64-linux-gnu",
}

// linuxEntries is gcc's well-known order with the gcc-isms removed:
// /usr/local/include, the multiarch directory, /usr/include. gcc's
// own include directory is played by the builtins (already in the
// list), and include-fixed is gcc's header-patching mechanism — a
// gcc-ism, not a place headers live.
func linuxEntries(h Host, target string) []Entry {
	dirs := []string{"/usr/local/include"}
	if tuple := multiarch[archOf(target)]; tuple != "" {
		dirs = append(dirs, "/usr/include/"+tuple)
	}
	dirs = append(dirs, "/usr/include")
	return dirEntries(h, dirs)
}