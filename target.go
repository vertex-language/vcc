package vcc

import (
	"errors"
	"runtime"
	"sort"
	"strings"

	"github.com/vertex-language/ir"

	"github.com/vertex-language/vcc/types"
)

// An Arch is a backend in ir/lower.
type Arch uint8

const (
	ArchAMD64 Arch = iota
	ArchARM64
	ArchI386
)

func (a Arch) String() string {
	switch a {
	case ArchAMD64:
		return "amd64"
	case ArchARM64:
		return "arm64"
	case ArchI386:
		return "i386"
	}
	return "invalid"
}

// A Format is an object file container.
type Format uint8

const (
	FormatELF Format = iota
	FormatMachO
	FormatPE
)

func (f Format) String() string {
	switch f {
	case FormatELF:
		return "elf"
	case FormatMachO:
		return "macho"
	case FormatPE:
		return "pe"
	}
	return "invalid"
}

// ldblKind names a long double format. types.Model carries sizes, and size
// alone cannot tell x87 extended from IEEE quad (both 16 bytes), so the format
// is a fact of the target table — beside the Model, not inside it, because
// layout never needs it and the float.h predefines do.
type ldblKind uint8

const (
	ldblDouble ldblKind = iota // long double == double (arm64 macOS, Windows)
	ldblX87                    // 80-bit extended in 16 bytes (x86-64 SysV)
	ldblQuad                   // IEEE binary128 (aarch64 Linux)
)

// A Target is everything a target name decides, in one object.
//
// "aarch64-macos" says two things to two halves of the compiler. To the front
// end it is a type model: how wide a long is, whether char is signed, what
// float.h says about long double. Below vir it is an architecture, a container
// format, the ir.Target a module opens with, and the prefix a C identifier
// wears in the symbol table. Both halves are here, because the caller who
// needs one usually needs the other, and because holding them in two packages
// is what forced a name to be looked up twice.
//
// The fields are unexported behind accessors so the table can change without
// a major version.
type Target struct {
	name string

	// The front end's half.
	model types.Model
	ldbl  ldblKind
	wint  string // C type of wint_t: glibc says unsigned int, Darwin says int

	// The machine's half.
	arch   Arch
	format Format
	irt    ir.Target
	prefix string // "_" on Mach-O, empty on ELF; lower.Options applies it
	minOS  string // Mach-O deployment target; an object with a zero minos warns

	// unsupported, when non-empty, is why this target names a machine no hop
	// below vir is written for. It is data rather than a discovery, so a
	// caller can ask before compiling instead of learning four phases in.
	unsupported string
}

// Name is the target name this Target answers to.
func (t Target) Name() string { return t.name }

// Model is how wide this target's types are.
func (t Target) Model() types.Model { return t.model }

// Arch is the backend that selects instructions for this target.
func (t Target) Arch() Arch { return t.arch }

// Format is the container its objects are written into.
func (t Target) Format() Format { return t.format }

// IR is the use path and layout a module for this target opens with.
func (t Target) IR() ir.Target { return t.irt }

// SymbolPrefix is what a C identifier becomes in the symbol table.
func (t Target) SymbolPrefix() string { return t.prefix }

// Supports reports whether vcc can build for this target, and says why not
// when it cannot. A Target names a machine; it does not promise every hop
// below vir is written for it.
func (t Target) Supports() error {
	if t.unsupported != "" {
		return errors.New(t.unsupported)
	}
	return nil
}

// x86_64ELF is bare-metal x86-64: the SysV layout ir.X86_64Linux carries,
// under a use path that names no OS. ir's stock table is one entry per
// architecture per hosted OS with nothing freestanding, so this is built
// rather than borrowed. Keeping its Layout in step with X86_64Linux's is its
// whole job — the two share an ABI and differ only in the use path.
var x86_64ELF = ir.NewTarget("x86_64/elf", ir.Layout{
	ABI: "sysv", Endian: ir.LittleEndian, PtrBits: 64, StackAlign: 16,
	ExtFloat: []ir.RegType{ir.TypeF80, ir.TypeF128}, Vector: true,
})

// i386ELF is bare-metal Intel386, on the same terms as x86_64ELF.
var i386ELF = ir.NewTarget("i386/elf", ir.Layout{
	ABI: "sysv", Endian: ir.LittleEndian, PtrBits: 32, StackAlign: 16,
	ExtFloat: []ir.RegType{ir.TypeF80},
})

// targets is every target vcc models, and is the authority on the name.
//
// An entry exists when vcc can state both halves — the type model the analyzer
// sizes against, and the machine the backend lowers for. The table does not
// grow on request; it grows with a Model and a Spec to back it.
var targets = map[string]Target{
	"x86_64-elf": {
		model: types.LP64(), ldbl: ldblX87, wint: "unsigned int",
		arch: ArchAMD64, format: FormatELF, irt: x86_64ELF,
	},
	"x86_64-linux": {
		model: types.LP64(), ldbl: ldblX87, wint: "unsigned int",
		arch: ArchAMD64, format: FormatELF, irt: ir.X86_64Linux,
	},
	"x86_64-macos": {
		model: types.LP64(), ldbl: ldblX87, wint: "int",
		arch: ArchAMD64, format: FormatMachO, irt: ir.X86_64MacOS,
		prefix: "_", minOS: "10.13",
	},
	"aarch64-linux": {
		model: lp64ARM(), ldbl: ldblQuad, wint: "unsigned int",
		arch: ArchARM64, format: FormatELF, irt: ir.AArch64Linux,
	},
	"aarch64-macos": {
		model: darwinARM64(), ldbl: ldblDouble, wint: "int",
		arch: ArchARM64, format: FormatMachO, irt: ir.AArch64MacOS,
		prefix: "_", minOS: "11.0",
	},
	"i386-elf": {
		model: ilp32(), ldbl: ldblX87, wint: "unsigned int",
		arch: ArchI386, format: FormatELF, irt: i386ELF,
	},
	"i386-linux": {
		model: ilp32(), ldbl: ldblX87, wint: "unsigned int",
		arch: ArchI386, format: FormatELF, irt: ir.I386Linux,
	},
	"x86_64-windows": {
		model: llp64(), ldbl: ldblDouble, wint: "unsigned short",
		arch: ArchAMD64, format: FormatPE, irt: ir.X86_64Windows,
	},
}

// LookupTarget is how a target name becomes a Target.
func LookupTarget(name string) (Target, bool) {
	t, ok := targets[name]
	if !ok {
		return Target{}, false
	}
	t.name = name
	return t, true
}

// Targets is every target vcc models, sorted.
func Targets() []string {
	out := make([]string, 0, len(targets))
	for n := range targets {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// HostTarget is the machine vcc is running on. It is not a Target on a machine
// vcc does not model, which is where every verb that needs one says so.
//
// The Go spellings are not C's, which is what the two tables here translate.
func HostTarget() (Target, bool) {
	return LookupTarget(HostName())
}

// HostName is the target name for the machine vcc is running on, or "" where
// that machine is not one vcc models.
func HostName() string {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64", "386": "i386"}[runtime.GOARCH]
	osname := map[string]string{"darwin": "macos", "linux": "linux", "windows": "windows"}[runtime.GOOS]
	if arch == "" || osname == "" {
		return ""
	}
	return arch + "-" + osname
}

// SplitTarget breaks a target name into its two halves. The architecture is
// everything before the first dash.
func SplitTarget(name string) (arch, osname string) {
	if i := strings.IndexByte(name, '-'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return name, ""
}

// targetList is every name, for the "known: …" tail of an error.
func targetList() string { return strings.Join(Targets(), ", ") }

// ---- the type models ----

// lp64ARM is AAPCS64 Linux: LP64 sizes, but char is unsigned and wchar_t is
// unsigned int.
func lp64ARM() types.Model {
	m := types.LP64()
	m.CharSigned = false
	m.WCharKind = types.UInt
	return m
}

// darwinARM64 is Apple's arm64 ABI: char signed (a deliberate divergence from
// AAPCS64), wchar_t int, long double == double.
func darwinARM64() types.Model {
	m := types.LP64()
	m.SizeLongDouble, m.AlignLongDouble = 8, 8
	return m
}

// ilp32 is Intel386 SysV: everything but long long and double is a word, and
// long double is x87 extended padded to 12 bytes with 4-byte alignment rather
// than x86-64's 16.
func ilp32() types.Model {
	m := types.LP64()
	m.SizeLong, m.SizePtr = 4, 4
	m.SizeLongDouble, m.AlignLongDouble = 12, 4
	return m
}

// llp64 is Windows x64: long stays 4 bytes, wchar_t is unsigned short, long
// double == double.
func llp64() types.Model {
	m := types.LP64()
	m.SizeLong = 4
	m.WCharKind = types.UShort
	m.SizeLongDouble, m.AlignLongDouble = 8, 8
	m.MSBitfields = true
	return m
}
