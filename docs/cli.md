# CLI

`cmd/vcc` is thirteen lines over `cli`, and `cli` is a wrapper over the
[`vcc`](../) package. That is the shape to keep in mind reading this: every
rule below is the library's rule, and the command line adds only what a command
adds — flags, where an artifact lands, standard input, the caret under a
diagnostic, and an exit code. Anything `vcc` can do from a terminal, a Go
program can do from the package, and the two cannot drift because there is only
one of them.

vcc compiles C11 (and C17, which is the same language) to native code, and
nothing else. All eight translation phases are in the box — vcc's own
conforming preprocessor included — and so is everything below them: instruction
selection, object writing, and linking. There is no `cc` on the path, no `as`,
no `ld`, and nothing to install beside it.

```
C → vir → machine code → object → image
```

---

## Shape

```
vcc <command> [flags] [files...]
```

Verb first, like `go` and `git` — not `cc f.c`.

`gcc` and `clang` are single-namespace drivers, and the cost is visible in the
manual: `-M -MM -MD -MMD -MF -MG -MP -MT -MQ` is one feature wearing nine
flags, and `-fsyntax-only` is a verb wearing a flag costume. A verb gets a
namespace of its own; `vcc check` needs no `--syntax-only=yes`.

The cost is that `vcc main.c` is not a command. `vcc build main.c` is.

| | |
| --- | --- |
| `vcc build` | compile and link; with `--emit`, stop earlier |
| `vcc run` | compile, link to a temporary path, execute |
| `vcc check` | preprocess, parse, analyze; no artifact |
| `vcc ast` | parse and dump the syntax tree |
| `vcc tokens` | dump the token stream |
| `vcc env` | print the resolved configuration |

The inspect verbs read the same phases the compiler runs. `vcc ast` cannot show
a tree `vcc build` will not compile, and `vcc tokens` shows the stream the
parser is actually handed — the re-scan of phase 4's output, not a separate
lexing of the file.

---

## Input

Two inputs, two entry points into one pipeline:

| | |
| --- | --- |
| `.c` | the full pipeline — phases 1 through 7 |
| `.i` | already preprocessed — enters above the parser, phase 4 skipped |

Standard input is `-`, or no file at all, and is read as `.i`: a pipe has no
extension, and guessing is worse than a flag. `-pp` says preprocess it anyway;
`-no-pp` says a named `.c` file is already preprocessed.

```
vcc build -o main main.c                     # the normal case
cc -E -P main.c | vcc build -o main -        # accepted; input is .i
vcc build -pp -o main - < main.c             # C source on a pipe
```

Anything vcc does not compile — a `.o`, an `.a` — is passed to the linker in
place, in command-line order:

```
vcc build -o app main.c vendor/util.o libfoo.a
```

`.vir` is not yet an input. `--emit vir` prints a module and nothing reads one
back.

---

## Flags every verb that reads C shares

```
-target T          target to compile for (default: this host)
-I dir             add an include search directory (repeatable, in order)
-D name[=val]      define a macro (repeatable); -D NAME defines it as 1
-U name            undefine a macro (repeatable)
-include file      process a file before the main input (repeatable)
-freestanding      builtin headers only; no platform directories
-pp / -no-pp       force preprocessing on or off
```

`-D` and `-U` share one list, and their order is meaning: `-D FOO -U FOO -D
FOO=2` says what it looks like. The target's own predefines precede all of
them, so `-U __APPLE__` strips an identity macro with no new mechanism.

Single and double dashes are both accepted (`-target` and `--target`), because
Go's flag package accepts both and inventing a rule about it would be
inventing.

---

## Preprocessing

Phase 4 is the `preprocessor` package: ISO C11 §6.10, conforming,
deterministic. The directive grammar lives there and nowhere else in the tree.

**Search order is one list.** `-I` directories in command-line order, then
`VCC_INCLUDE_PATH`, then vcc's builtin headers, then the platform's — or, under
`-freestanding`, the builtins alone. There is no `-iquote`/`-isystem`/
`-idirafter` tower: `#include "..."` looks in the including file's directory
first and then the list, `#include <...>` skips that first step, and that is
the rule ISO describes with nothing added. `vcc env` prints the list before the
build runs.

**The dialect rule applies to directives too.** `#include_next`, `__COUNTER__`,
`##__VA_ARGS__` comma-swallowing and the rest of the GNU directive dialect are
rejected. What ISO defines — `#include`, `#define` with `__VA_ARGS__`,
conditionals, `#line`, `#error`, `#pragma`, `_Pragma`, the predefined macros —
is what runs. `#pragma once` warns and points at the include guard; vcc detects
guards on its own, so a file with one is opened once.

**System headers get one carve-out.** Platform headers are not ISO C after
expansion, so a closed, documented set of extension spellings
(`__attribute__((…))`, `__restrict`, `__extension__`, `__declspec(…)` and kin —
the list is in the parser README) is parsed and discarded in declarations only.
Never given semantics, never valid elsewhere, visibly absent from `--emit vir`.

**Determinism holds through phase 4.** `__DATE__` and `__TIME__` come from
`SOURCE_DATE_EPOCH`, or from the Unix epoch when nothing names one — so a build
is reproducible with no flag saying so. `__FILE__` expands to the path as
given, never an absolute path the build machine leaked.

---

## Target selection

```
-target <arch>-<platform>        default: this host
```

Eight names, and the list is the authority — `vcc env` prints the resolved one,
and an unknown name is an error naming every valid one:

| target | container |
| --- | --- |
| `aarch64-macos`, `x86_64-macos` | Mach-O |
| `aarch64-linux`, `x86_64-linux`, `i386-linux` | ELF |
| `x86_64-elf`, `i386-elf` | ELF, freestanding |
| `x86_64-windows` | PE — modelled, not buildable |

A target name means two things at once: a type model to the front end (how wide
a `long` is, whether `char` is signed, what `float.h` says about `long double`)
and a machine below vir (an architecture, a container, the `ir.Target` a module
opens with, the symbol prefix). Both halves live in one table in the `vcc`
package, which is what keeps a name from meaning two different things in two
places.

**Cross-compiling and cross-linking are the same command.** The linker is
vcc's own, so `--emit obj` is not a lesser rung that other targets have to
settle for:

```
vcc build -target x86_64-linux -o app main.c
vcc build -target x86_64-macos -o app main.c     # on an arm64 Mac
```

What a foreign target still needs is that platform's headers and libraries,
which is what `-I`, `-L`, `-freestanding` and `SDKROOT` are for: the platform
directories vcc probes are this machine's, so a cross link names its own. What
it does not need is a toolchain.

**A modelled target is not always a buildable one.** `x86_64-windows` has a
complete entry — a type model, predefines, a container — and the amd64 backend
implements SysV while Windows declares the Microsoft ABI, so an object for it
is refused in the backend's own words. That state is real and is reported as
itself rather than hidden by leaving the target out.

**`vcc run` is the one host-only verb**, because executing a foreign binary is
this machine's business rather than the compiler's.

---

## The standard

C17, and there is no flag. C17 is C11 with defect reports folded in and the
grammar is identical, so a `-std` switch would be the first step toward a
dialect continuum with two entries and no principle.

`gnu11` is not a value, aliased or otherwise. Statement expressions, `typeof`,
nested functions and the GNU directive dialect are a different language, and
languages are out. (The tolerated-spelling list under Preprocessing is the one
exception, and it is discard-only.)

Conformance is the default. There is no `-pedantic`, because there is no
non-pedantic mode to escape from: a constraint violation is an error because
the standard says *shall*.

---

## Conventions

**stdout is data, stderr is narration.** `vcc build --emit i -o -`,
`vcc build --emit vir -o -`, `vcc ast` and `vcc tokens` write their artifact to
stdout and every diagnostic to stderr, so a pipe never picks up a warning.

**`-` means stdin or stdout** anywhere a path is accepted. `-o -` is for `i`
and `vir` only: an object file and an executable need a path, and saying so is
better than writing a binary into a terminal.

**Diagnostics are `file:line:col: severity: message [name]`,** with the source
line under them and a caret at the span, in raw bytes of what was typed —
through trigraphs, line splices and macro expansion. A diagnostic in a header
prints the chain of `#include`s that reached it; one in a macro expansion
points at the use site with a note at the definition.

```
$ vcc check use.c
hdr.h:2:32: error: 'nope' is undeclared; C11 has no implicit declaration (§6.5.1p2)
    static int oops(void) { return nope; }
                                   ^^^^
    in file included from use.c:1
```

Positions are the ones you wrote, not the ones preprocessing produced. Phase
4's output is printed and re-scanned for the parser to read, which puts every
later position in that printed text; a map records where each token came from
and every diagnostic is mapped back through it before it is printed. `vcc ast`
and `vcc tokens` are the exception, and deliberately: they are showing you the
preprocessed unit, so they number its lines.

**One diagnostic per mistake.** The preprocessor reports a bad directive once,
the parser goes quiet until it consumes a token, the scanner reports a
malformed literal once. The analyzer runs even after a parse error, because a
partial parse is a usable one and `Bad*` nodes analyze silently — so the set
stays "each mistake exactly once" rather than "everything after the first is
silence". A tree the front end rejected is never lowered: what lowering would
report is the poison left behind by the mistake, sorted in among the real
diagnostics where it buries the line you have to fix.

**Diagnostics do not depend on optimization,** because analysis runs before the
IR exists. `vcc check` and `vcc build` report the same set, always.

**Output is deterministic.** Identical input and flags produce byte-identical
output — objects, images, vir, preprocessed source, dumps. `SOURCE_DATE_EPOCH`
clamps anything time-shaped. There is no `--deterministic` flag because there
is no other mode.

**vcc never prompts,** and there is no config file. A config file makes the
same command mean different things in different directories; you have a
Makefile.

| Exit | |
| --- | --- |
| 0 | success — no error diagnostics |
| 1 | the input was wrong — diagnostics with errors |
| 2 | the invocation was wrong, or the machine failed |

`vcc run` is the exception, and forwards the program's own exit code: a program
that exits 1 did not fail to build.

---

## build

```
vcc build [flags] [files...]

  --emit <kind>     exe (default) | obj | vir | i
  -o file           write output here ("-" is standard output, for i and vir)
  -L dir            add a library search directory (repeatable, in order)
  -l name           link against a library (repeatable, in order)
  -entry sym        the program's entry symbol (default: the platform's)
  -static           link a static image
```

plus every shared flag above. One switch, `--emit`, replaces gcc's `-E`/`-c`
mode flags and names the stops the pipeline actually has:

```
vcc build -o app main.c util.c        # compile and link, like `go build`
vcc build --emit obj main.c           # → main.o              (gcc -c)
vcc build --emit vir -o - main.c      # → the IR, to stdout
vcc build --emit i -o - main.c        # → preprocessed C      (gcc -E -P)
```

With `--emit exe`, several inputs compile and link into one image — that is
what an executable is. With `obj`, `vir` or `i`, each input becomes its own
artifact, named for the input in the working directory, and `-o` with more than
one input is a usage error: merging translation units is linking, and linking
produces executables.

**Link order is the command line's.** A static link is order-sensitive, and
reordering it would be vcc deciding something you said — so a compiled source
is linked exactly where its `.c` appeared among the objects around it.
Libraries named with `-l` come after all of them, in the order given, which is
where a linker expects them: an archive contributes only what something
already in the link needs.

**`-l` resolves the way every C toolchain resolves one.** `-L` directories in
order, then the platform's own — which `vcc env` prints under `libraries:`, so
the search is data here too. Within a directory the shared form is tried
first: `libfoo.tbd` then `libfoo.dylib` then `libfoo.a` on Mach-O,
`libfoo.so` then `libfoo.a` on ELF, and `foo.lib` on PE, where the linker
does its own search from the same list. `-freestanding` leaves only the
directories you named.

A name that resolves nowhere says what it looked for and where:

```
$ vcc build -o app -l zzz main.c
vcc: cannot find -lzzz: no libzzz.tbd, libzzz.dylib or libzzz.a in /usr/local/lib, /…/MacOSX.sdk/usr/lib
```

A library that already has a path needs none of this — it is an input like any
other, linked exactly where you put it: `vcc build -o app main.c libfoo.a`.

**`-static` means archives.** On ELF it is the linker's own switch as well: no
`.dynamic`, no PLT, no interpreter, and a shared input refused rather than
quietly ignored. On Mach-O it restricts resolution to archives and nothing
more, because a Darwin program links against the system's stubs whatever else
it does — Apple does not ship a static libc either.

**`--emit i` output is valid `.i` input.** The round trip
`vcc build --emit i -o - main.c | vcc build -o main -` produces the same
executable as the direct build. `#pragma` lines survive it, and an `.i` file
passed to `--emit i` comes back byte for byte rather than being re-printed from
a scan of itself.

**`--emit vir` prints a module even for input that was rejected.** A partial
module is what broken input should produce here, the same contract `vcc ast`
keeps for a broken parse; the diagnostics say whether it is a whole one.

```
$ vcc build --emit vir -o - min.c
module min

use "aarch64/macos"

layout {
  abi        aapcs,
  endian     little,
  ptrbits    64,
  stackalign 16,
  extfloat   none,
}
```

**`verify.Module` runs between lowering and the backend, always.** It is cheap
next to instruction selection, and what it catches is a bug in vcc — which is
worth a sentence naming the module rather than whatever the backend would have
said three phases later.

**Hosted versus freestanding is one flag.** `-freestanding` ships only the
headers ISO requires of a freestanding implementation, defines
`__STDC_HOSTED__` to 0, searches no platform directory, and leaves `-entry` to
you. It replaces gcc's `-ffreestanding -nostdlib -nostartfiles -nodefaultlibs`,
which are four flags because they accreted, not because there are four ideas.

---

## run

```
vcc run [flags] <file> [-- args...]
```

Compile, link into a temporary directory, execute, forward the exit code.
Everything after `--` belongs to the program, which is why the flag set stops
there rather than at the first non-flag: `vcc run p.c -- -o x` passes `-o` to
the program, and vcc's own `-o` would be meaningless here anyway — the image is
temporary by definition.

Every `build` flag applies, `-l` and `-static` included. The one thing `run`
refuses is a target that is not this machine, with the target named.

```
vcc run hello.c
vcc run hello.c -- --verbose input.txt
```

---

## check

```
vcc check [flags] [files...]
```

The whole front end — preprocess, scan, parse, analyze — with no artifact, and
exit 1 on any error. This is gcc's `-fsyntax-only` promoted from a flag to a
verb, because it is what editors, CI and pre-commit hooks actually run.
Preprocessor flags apply, because a file cannot be checked without knowing what
its macros mean.

Several files are several translation units, each with its own macro table, and
the exit code is the worst of them.

---

## ast

```
vcc ast [flags] <file>

  -skip-bodies      skip function bodies (fast structural pass)
  -comments         retain comments on the tree
```

`ast.Fdump` of the parse, after preprocessing: positions as `line:column` in
the unit the parser read, `Bad*` nodes printed as themselves — a partial parse
is a usable one, and the dump shows exactly how partial.

```
$ vcc ast main.i
File 1:1
  FuncDecl 1:1
    KeywordSpec 1:1 int
    FuncDeclarator 1:5
      NameDeclarator 1:5
        Ident 1:5 main
      ParamDecl 1:10
        KeywordSpec 1:10 void
    CompoundStmt 1:16
```

`-skip-bodies` is the parser's structural mode: declarations, prototypes and
typedefs land, and function bodies are skipped balanced. `-comments` reaches
back into phase 4 as well as the parser, because the preprocessor drops comment
tokens unless told to keep them.

---

## tokens

```
vcc tokens [flags] <file>
```

One token per line: position, kind, the spelling for tokens that carry one, and
flags where set (`adj` for a token with no space before it, `nl` for one that
opens a line, `digraph`).

```
$ vcc tokens main.i
  1:1   int
  1:5   IDENT        main
  1:9   (            [adj]
  1:10  void         [adj]
  1:14  )            [adj]
  1:16  {
  2:5   int          [nl]
  2:9   IDENT        x
  2:11  =
  2:13  INT_LIT      0x1Fu
```

On `.c` input this is the re-scan of phase 4's output — what the parser will
actually see. `-no-pp` shows the scan of the file as written. Literals print
undecoded, because the scanner does not decode: what you see is what the parser
gets.

---

## env

```
vcc env [flags]

  -defines          also print the target's predefined macros
```

The resolved configuration, before anything is compiled. The point is the
invariant the READMEs promise: header search is data, and it is inspectable.

```
$ vcc env
target: aarch64-macos
std: c17
hosted: true

search:
  <builtin>  [system]
  /Applications/Xcode.app/…/MacOSX.sdk/usr/include  [system]

libraries:
  /usr/local/lib
  /Applications/Xcode.app/…/MacOSX.sdk/usr/lib
```

`search:` is where `#include` looks and `libraries:` is where `-l` looks, both
in the order they are walked and both before anything is compiled.

`-defines` adds the target's predefined macros in the order phase 4 receives
them — the identity set first, then the model facts the builtin headers are
written against:

```
predefines:
  -D __aarch64__=1
  -D __APPLE__=1
  -D __MACH__=1
  -D __arm64__=1
  …
```

Where something expected is missing — no SDK, no vcvars environment — `vcc env`
prints a note rather than failing. A host with no system headers still
resolves, to the builtins, and the failure that matters is the `#include` that
does not find its file, reported there with this list to point at.

---

## The environment

Four variables, all read by the command line and none by the library — a
compiler that behaves one way in a terminal and another in a test is a compiler
nobody can trust. A Go program using the package supplies these as fields, or
supplies its own machine to be asked.

| | |
| --- | --- |
| `SOURCE_DATE_EPOCH` | fixes `__DATE__` and `__TIME__`; the Unix epoch when unset |
| `VCC_INCLUDE_PATH` | extra include directories, after `-I` and before the builtins |
| `SDKROOT` | the macOS SDK to compile and link against; `xcrun` is asked when unset |
| `INCLUDE`, `LIB` | the Windows include and library lists, as a `vcvars` shell sets them |

---

## Coming from gcc and clang

| gcc / clang | vcc |
| --- | --- |
| `-c` | `--emit obj` |
| `-E`, `-E -P` | `--emit i -o -` (there are no linemarkers to suppress) |
| `-o out` | `-o out` |
| `-I`, `-D`, `-U` | unchanged |
| `-include file` | `-include file` |
| `-isystem dir`, `-iquote dir` | `-I dir` — one list, ISO's search rule |
| `-L`, `-l` | unchanged |
| `-static` | `-static` |
| `-e sym`, `--entry` | `-entry sym` |
| `-std=c11`, `-std=c17` | the default, and the only one |
| `-std=gnu11`, `-ansi` | not accepted; GNU C is a different language |
| `-fsyntax-only` | `vcc check` |
| `-ffreestanding -nostdlib -nostartfiles` | `-freestanding` |
| `--target x86_64-linux-gnu` | `-target x86_64-linux` |
| `-pedantic`, `-Wall`, `-Wextra` | the default, with no other mode |

**No translation, on purpose:**

- **The `-f` semantics zoo** — `-fwrapv`, `-fno-strict-aliasing`,
  `-fomit-frame-pointer` and the rest are requests to change what the language
  means or what the encoder decides. The language means what ISO says; encoding
  decisions belong to lowering.
- **`-mtune`, `-mcpu`** — tuning assumes an optimizer with visible opinions.
- **`-x c`** — vcc compiles C; `-pp` exists only because a pipe has no
  extension.
- **`#pragma GCC diagnostic`** — warning state is a build decision, made where
  the build can see it, not scattered through source.
- **`__COUNTER__`, `#include_next`, `##__VA_ARGS__`** — the GNU directive
  dialect.

---

## Not yet

Stated plainly, because a flag that is accepted and ignored is worse than one
that is missing.

| | |
| --- | --- |
| `--emit asm` | the architecture packages are encoders; none of them prints. A missing package, not a missing flag, and the error says so |
| `.vir` as an input | `--emit vir` prints a module; nothing reads one back |
| `vcc fmt` | the tree is built for it — specifiers in written order, trailing commas, K&R definitions whole — and the printer is not written |
| optimization, `-g` | there are no levels and no DWARF. Both are decisions below vir that have not been made |
| warning control | the baseline is the whole set; there is no `-W` switch and no `explain` verb behind the names in brackets |
| `--json` | dumps are for reading, so far |

---

## `vcc help`

```
vcc — the Vertex C Compiler

Usage:

    vcc build  [flags] [files...]  compile and link; with --emit, stop earlier
    vcc run    [flags] [file] [-- args...]   build to a temporary path and run it
    vcc check  [flags] [files...]  preprocess, parse, analyze; print diagnostics
    vcc ast    [flags] [file]      parse and dump the syntax tree
    vcc tokens [flags] [file]      dump the token stream
    vcc env    [flags]             print the resolved include list and predefines

A file of "-" (or no file) reads standard input.

A .c file runs through vcc's own preprocessor; a .i file (or stdin)
enters the pipeline above it. -pp and -no-pp override the extension.
```

`vcc help`, `vcc -h` and `vcc --help` print it in full.
