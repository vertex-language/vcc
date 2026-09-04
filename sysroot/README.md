# sysroot

`package sysroot` answers the one question phase 4 cannot: where does
this host keep the target's headers?

```go
import "github.com/vertex-language/vcc/sysroot"
```

    entries, notes := sysroot.Resolve("x86_64-linux", hosted)

## Order

`Resolve` returns the ordered include list:

1. **`VCC_INCLUDE_PATH`** — user directories, `:`- or `;`-separated by
   host OS. Not `System`: a warning sited here is the user's own code,
   so it should surface every time, not once.
2. **vcc's builtin headers** — embedded via `go:embed`, written once
   against target-parameterized predefines (`__SIZE_TYPE__`,
   `__INT_MAX__`, and friends; see `builtin.go`). Mounted as
   `<builtin>`, never an absolute path, so `__FILE__` and diagnostics
   read identically on every machine.
3. **The platform's well-known directories** — probed by `stat`, not
   a distro table: an absent directory is simply an absent entry, not
   a special case.

`-I` directories are the CLI's to prepend; `Resolve` doesn't see them.

With `hosted = false` (`--freestanding`), `Resolve` skips straight to
the builtins alone — exactly the headers ISO requires of a
freestanding implementation, and nothing a hosted environment would
add.

## Per platform

- **Linux** (`linux.go`): `/usr/local/include`, the Debian multiarch
  directory for the target's architecture (`x86_64-linux-gnu` etc.,
  skipped where it doesn't apply — Fedora, Arch, Alpine), then
  `/usr/include`.
- **macOS** (`darwin.go`): `/usr/local/include`, then the SDK from
  `$SDKROOT`, else `xcrun --show-sdk-path`, else the Command Line
  Tools SDK at its fixed path, then a bare `/usr/include` if one
  happens to exist.
- **Windows** (`windows.go`): `%INCLUDE%` from a vcvars environment
  (`vcvars64.bat`, a Developer Command Prompt), and nothing else —
  reproducing `vswhere`'s walk over Program Files, editions, and
  toolset versions is folklore this package exists to avoid.

`notes` carries advice for these gaps (`no SDK found`, `INCLUDE is
not set`) — never errors. A bare host still resolves, to the
builtins; the failure that matters is the `#include` that misses,
reported there, with this list to point at.

## Entry

```go
type Entry struct {
	Name   string
	FS     fs.FS
	System bool
}
```

`System` marks platform and builtin entries, not `VCC_INCLUDE_PATH`
ones — it's what lets warnings behave differently in headers the user
didn't write.

## Testing

The probing is injected (`Host`: `Getenv`, `IsDir`, `Run`), so the
ordering logic itself is a pure function. `sysroot_test.go` drives it
as a Linux box with no multiarch, a Mac with no Xcode, or a Windows
shell with no vcvars — from any machine.

## Dependencies

Imports the standard library only. The `vcc` package converts `Entry` to
`preprocessor.Mount`; `sysroot` does not import `preprocessor`,
keeping the arrow pointing one way.