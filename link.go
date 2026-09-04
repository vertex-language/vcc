package vcc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vertex-language/elf"
	elflink "github.com/vertex-language/elf/link"
	"github.com/vertex-language/macho"
	macholink "github.com/vertex-language/macho/link"
	"github.com/vertex-language/pe"
	pelink "github.com/vertex-language/pe/link"

	"github.com/vertex-language/vcc/sysroot"
)

// linkParams is one link, below vir.
//
// The linker is vcc's own: there is no cc on the path, nothing to detect and
// no host check. The three formats are three vertex-language linkers, each of
// which takes bytes and returns bytes, which is why an object never has to
// reach the filesystem to be linked.
type linkParams struct {
	// Objects are the images to link, in the order the caller gave them.
	// Order is significant and is preserved exactly.
	Objects []Input

	Output       string
	Entry        string
	Static       bool
	Freestanding bool

	// LibDirs and Libs are -L and -l. A name is resolved to a file against
	// LibDirs and then the platform's own directories — see library.go —
	// because only the PE linker has a search path of its own.
	//
	// Resolved libraries are added after every object, which is where a
	// linker expects them: an archive contributes only what something
	// already in the link needs.
	LibDirs []string
	Libs    []string

	// Host is how the Mach-O link finds the platform SDK. Nil is the real
	// machine.
	Host sysroot.Host
}

// link produces an executable from objects.
func link(t Target, p linkParams) error {
	if len(p.Objects) == 0 {
		return fmt.Errorf("nothing to link")
	}
	if p.Output == "" {
		return fmt.Errorf("link needs an output path")
	}

	switch t.format {
	case FormatMachO:
		return linkMachO(t, p)
	case FormatELF:
		return linkELF(t, p)
	case FormatPE:
		return linkPE(t, p)
	default:
		return fmt.Errorf("unsupported format %v", t.format)
	}
}

func linkMachO(t Target, p linkParams) error {
	// The subtype is named rather than left zero. Mach-O's backend registry
	// keys on both halves and matches the subtype exactly, and only arm64's
	// "any implementation" subtype happens to be 0 — x86_64's and i386's is
	// 3, which macho/cpu.go says in as many words. Leaving it out meant
	// every x86_64 and i386 link failed to find a backend that was
	// registered all along.
	var cpu macho.CPU
	var sub macho.SubCPU
	switch t.arch {
	case ArchAMD64:
		cpu, sub = macho.CPU_TYPE_X86_64, macho.CPU_SUBTYPE_X86_64_ALL
	case ArchARM64:
		cpu, sub = macho.CPU_TYPE_ARM64, macho.CPU_SUBTYPE_ARM64_ALL
	case ArchI386:
		cpu, sub = macho.CPU_TYPE_I386, macho.CPU_SUBTYPE_I386_ALL
	default:
		return fmt.Errorf("macho: unsupported arch %v", t.arch)
	}

	target := macho.Target{
		CPU:      cpu,
		SubCPU:   sub,
		Platform: macho.PlatformMacOS,
		Endian:   macho.LittleEndian,
	}

	if t.minOS != "" {
		v, err := macho.ParseVersion(t.minOS)
		if err == nil {
			target.MinOS = v
		}
	}

	l, err := macholink.New(target)
	if err != nil {
		return linkErr(err)
	}

	if p.Entry != "" {
		l.SetEntry(t.prefix + p.Entry)
	}

	// The SDK is sysroot's answer, not a second one: the include list and the
	// libSystem stub have to come from the same SDK, and a machine with the
	// command line tools installed but xcrun not answering has one that only
	// sysroot's third fallback finds.
	if !p.Freestanding {
		if sdk, ok := sysroot.SDK(p.Host); ok {
			l.SetSDK(sdk)
			if data, err := os.ReadFile(filepath.Join(sdk, "usr/lib/libSystem.tbd")); err == nil {
				l.AddStub("libSystem", data)
			}
		}
	}

	libs, err := p.libraries(t)
	if err != nil {
		return err
	}
	if err := addObjects(l.AddFile, p.Objects); err != nil {
		return err
	}
	if err := addObjects(l.AddFile, libs); err != nil {
		return err
	}

	img, err := l.Link()
	if err != nil {
		return linkErr(err)
	}

	b, err := img.Bytes()
	if err != nil {
		return linkErr(err)
	}

	return os.WriteFile(p.Output, b, 0o755)
}

func linkELF(t Target, p linkParams) error {
	var arch elf.Arch
	switch t.arch {
	case ArchAMD64:
		arch = elf.ArchAMD64
	case ArchARM64:
		arch = elf.ArchARM64
	case ArchI386:
		arch = elf.ArchI386
	default:
		return fmt.Errorf("elf: unsupported arch %v", t.arch)
	}

	target := elf.Target{
		Arch:   arch,
		Class:  elf.ELFCLASS64,
		Endian: elf.EndianLittle,
	}
	if arch == elf.ArchI386 {
		target.Class = elf.ELFCLASS32
	}

	l := elflink.New(target)

	if p.Entry != "" {
		l.SetEntry(p.Entry)
	}

	// Static is the linker's own switch here: no .dynamic, no PLT, no
	// interpreter, and a shared input refused rather than quietly ignored.
	// It is set before any input is added, because it decides what an input
	// is allowed to be.
	l.Options().Static = p.Static

	libs, err := p.libraries(t)
	if err != nil {
		return err
	}
	if err := addObjects(l.AddFile, p.Objects); err != nil {
		return err
	}
	if err := addObjects(l.AddFile, libs); err != nil {
		return err
	}

	img, err := l.Link()
	if err != nil {
		return linkErr(err)
	}

	return os.WriteFile(p.Output, img.Bytes(), 0o755)
}

func linkPE(t Target, p linkParams) error {
	var m pe.Machine
	switch t.arch {
	case ArchAMD64:
		m = pe.MachineAMD64
	case ArchARM64:
		m = pe.MachineARM64
	case ArchI386:
		m = pe.MachineI386
	default:
		return fmt.Errorf("pe: unsupported arch %v", t.arch)
	}

	target := pe.Target{
		Machine: m,
		SubArch: m.SubArch(),
		ABI:     pe.ABIMSVC,
		OS:      pe.OSWindows,
	}

	l, err := pelink.New(target)
	if err != nil {
		return linkErr(err)
	}
	if p.Entry != "" {
		l.SetEntry(p.Entry)
	}

	// The search path is handed over as well as used here, because the PE
	// linker resolves names of its own: a /DEFAULTLIB inside a CRT object
	// names a library nobody wrote on the command line, and it is the
	// linker that has to find it.
	l.SetLibPath(p.libraryDirs(t)...)

	libs, err := p.libraries(t)
	if err != nil {
		return err
	}
	if err := addObjects(l.AddObject, p.Objects); err != nil {
		return err
	}
	if err := addObjects(l.AddArchive, libs); err != nil {
		return err
	}

	img, err := l.Link()
	if err != nil {
		return linkErr(err)
	}

	b, err := img.Bytes()
	if err != nil {
		return linkErr(err)
	}

	return os.WriteFile(p.Output, b, 0o755)
}

// linkErr prefixes a linker's error with "link:" unless it already says so.
// All three linkers name themselves in most of what they return, and "link:
// link: duplicate symbol" is a wrapper repeating itself.
func linkErr(err error) error {
	if strings.HasPrefix(err.Error(), "link:") {
		return err
	}
	return fmt.Errorf("link: %w", err)
}

// addObjects hands each input to a linker's add function, in order.
//
// An input carrying bytes never touches the filesystem: the linkers take
// (name, data), so an object this process just produced goes straight in.
// One that names a path — a .o from an earlier build, a .a nobody wants in
// memory — is read here, at the last possible moment.
func addObjects(add func(string, []byte) error, objs []Input) error {
	for _, o := range objs {
		data, err := o.bytes()
		if err != nil {
			return err
		}
		if err := add(o.Name, data); err != nil {
			return err
		}
	}
	return nil
}
