module github.com/vertex-language/vcc

go 1.23

require (
	github.com/vertex-language/amd64 v0.0.0
	github.com/vertex-language/arm64 v0.0.0
	github.com/vertex-language/elf v0.0.0
	github.com/vertex-language/i386 v0.0.0
	github.com/vertex-language/ir v0.0.0
	github.com/vertex-language/ir/lower v0.0.0
	github.com/vertex-language/macho v0.0.0
	github.com/vertex-language/pe v0.0.0
)

require github.com/vertex-language/asm v0.0.0 // indirect

replace (
	github.com/vertex-language/amd64 => ../amd64
	github.com/vertex-language/arm64 => ../arm64
	github.com/vertex-language/asm => ../asm
	github.com/vertex-language/elf => ../elf
	github.com/vertex-language/i386 => ../i386
	github.com/vertex-language/ir => ../ir
	github.com/vertex-language/ir/lower => ../ir/lower
	github.com/vertex-language/macho => ../macho
)

replace github.com/vertex-language/pe => ../pe
