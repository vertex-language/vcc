package vcc

import (
	"bytes"
	"fmt"

	amd64elf "github.com/vertex-language/amd64/obj/elf"
	amd64macho "github.com/vertex-language/amd64/obj/macho"
	amd64pe "github.com/vertex-language/amd64/obj/pe"
	arm64elf "github.com/vertex-language/arm64/obj/elf"
	arm64macho "github.com/vertex-language/arm64/obj/macho"
	i386elf "github.com/vertex-language/i386/obj/elf"
	machocore "github.com/vertex-language/macho"

	"github.com/vertex-language/ir"
	amd64lower "github.com/vertex-language/ir/lower/amd64"

	"github.com/vertex-language/amd64/feature"
	arm64lower "github.com/vertex-language/ir/lower/arm64"
	i386lower "github.com/vertex-language/ir/lower/i386"
)

// emitObject lowers m for t and returns the object file's bytes.
//
// This is the pipeline below vir: ir/lower selects instructions for one
// architecture, and that architecture's obj/{elf,macho} writes them into a
// container. The two halves are chosen independently — Arch picks the backend,
// Format picks the writer — and a pair that does not exist is an error naming
// both rather than a nil object.
//
// producer is the toolchain string stamped where the container has a place
// for it; empty stamps nothing.
func emitObject(m *ir.Module, t Target, producer, march string) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("nil module")
	}
	switch t.arch {
	case ArchARM64:
		return arm64Object(m, t, producer)
	case ArchAMD64:
		return amd64Object(m, t, producer, march)
	case ArchI386:
		return i386Object(m, t, producer)
	}
	return nil, fmt.Errorf("target %s names no backend", t.name)
}

func arm64Object(m *ir.Module, t Target, producer string) ([]byte, error) {
	// Apple's variadic convention is a fact of the platform, and the
	// platform is what the container says: both Darwin and Linux declare
	// abi "aapcs" in the layout block, so this cannot be read off the
	// module. Getting it wrong is a wrong call, not a slow one.
	variadic := arm64lower.VariadicAAPCS64
	if t.format == FormatMachO {
		variadic = arm64lower.VariadicDarwin
	}
	o, err := arm64lower.Lower(m, arm64lower.Options{
		LibcallPrefix: t.prefix,
		Variadic:      variadic,
	})
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	switch t.format {
	case FormatMachO:
		err = arm64macho.Write(&buf, o, arm64macho.Options{
			Platform: machocore.PlatformMacOS,
			MinOS:    t.minOS,
		})
	case FormatELF:
		err = arm64elf.Write(&buf, o, arm64elf.Options{Comment: producer})
	default:
		err = fmt.Errorf("no %s writer for arm64", t.format)
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func amd64Object(m *ir.Module, t Target, producer, march string) ([]byte, error) {
	features, err := amd64Features(march)
	if err != nil {
		return nil, err
	}
	o, err := amd64lower.Lower(m, amd64lower.Options{
		Features:      features,
		LibcallPrefix: t.prefix,
	})
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	switch t.format {
	case FormatMachO:
		err = amd64macho.Write(&buf, o, amd64macho.Options{
			Platform: machocore.PlatformMacOS,
			MinOS:    t.minOS,
		})
	case FormatELF:
		err = amd64elf.Write(&buf, o, amd64elf.Options{Comment: producer})
	case FormatPE:
		err = amd64pe.Write(&buf, o, amd64pe.Options{File: m.Name()})
	default:
		err = fmt.Errorf("no %s writer for amd64", t.format)
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func i386Object(m *ir.Module, t Target, producer string) ([]byte, error) {
	o, err := i386lower.Lower(m, i386lower.Options{})
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	switch t.format {
	case FormatELF:
		err = i386elf.Write(&buf, o, i386elf.Options{Comment: producer})
	default:
		// Intel386 is ELF and COFF; Mach-O for it died with Rosetta 1.
		err = fmt.Errorf("no %s writer for i386", t.format)
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// amd64Features reads a -march spelling into the set the backend chooses
// instructions from.
//
// The baseline is the architecture's own — v1, which is SSE2 and nothing
// above it, and is the same floor MSVC's default /arch:SSE2 sets. Raising it
// is what lets the backend pick POPCNT for a popcount rather than refusing
// the verb; it is not what lets a program name an instruction, which an
// intrinsic does on its own account. See Compiler.March.
func amd64Features(march string) (feature.Set, error) {
	set := feature.NewSet(feature.V1)
	if march == "" {
		return set, nil
	}
	out, err := feature.Parse(set, march)
	if err != nil {
		return set, fmt.Errorf("-march %q: %w", march, err)
	}
	return out, nil
}
