// The backend registrations. Each container repository keys its writers and
// linkers on (architecture, format) at init time, so the arm that reaches one
// has to be linked in — importing the writer alone leaves the linker with no
// backend registered for a target whose objects it can already read.
package vcc

import (
	_ "github.com/vertex-language/macho/arm64"
	_ "github.com/vertex-language/macho/x86_64"

	_ "github.com/vertex-language/elf/arm64"
	_ "github.com/vertex-language/elf/i386"
	_ "github.com/vertex-language/elf/x86_64"

	_ "github.com/vertex-language/pe/x64"
)
