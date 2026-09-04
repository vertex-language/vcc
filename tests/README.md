# tests

The C suite. Every program here is compiled by vcc, linked, and run, and
passes by exiting zero.

```
go test ./tests/...                              # all of it
go test ./tests/runner -run 'TestPrograms/expr' -v
go test ./tests/runner -run 'TestPrograms/aggregate/bitfields.c' -v
```

## The convention

A test checks itself. It returns a distinct small number for each thing it
tests, so a failure names the assertion and not just the file, and it prints
one line on success:

```c
    if (sum != 13) return 1;
    if (diff != 7) return 2;
    ...
    printf("Operators OK\n");
    return 0;
```

Nothing else is compared — no expected-output files, no golden dumps. The
reason is that it makes every program runnable against another compiler
unchanged, which is how they are written and how they are checked:

```
clang -std=gnu11 -w -o /tmp/a tests/expr/operators.c && /tmp/a; echo $?
```

must give the same status and the same output vcc does. A disagreement is a
bug in one of the two, and the comment in the file says which behaviour the
standard requires and where. Several of the bugs this suite found were found
exactly that way, and a few were bugs in the test.

## The layout

One directory per area, and a file per topic within it. The area is the first
thing a reader chooses and the first thing `-run` matches, so a failure reads
as `TestPrograms/aggregate/bitfields.c` — where it is and what it is.

| | |
|---|---|
| `expr/` | operators and their evaluation order, integers, floats, pointers, chars, strings, `sizeof`, `_Generic` |
| `stmt/` | loops, `switch`, `goto` |
| `decl/` | scope, linkage, storage, names, qualifiers, enums, `inline`, `_Static_assert` |
| `aggregate/` | structs, unions, arrays, bit-fields, initializers, layout and padding |
| `func/` | calls and the ABI, function pointers, variadics, recursion |
| `vla/` | variable length arrays, and the one `sizeof` that is evaluated |
| `gnu/` | the GNU extensions vcc accepts: `typeof`, statement expressions, `__int128`, inline asm, `alloca`, builtins |
| `preproc/` | phase 4: directives, macro expansion, `#include_next` |
| `lib/` | the hosted library, where the ABI meets libc: `qsort` callbacks, `setjmp`, atomics, the heap |
| `link/` | several translation units linked into one program |
| `platform/` | what a host's own headers and libraries provide, behind the platform macro |
| `errors/` | programs that must **not** compile |

## The three shapes

| | what it is |
|---|---|
| `tests/<area>/*.c` | one file, one program |
| `tests/link/<name>/*.c` | linked together as one program |
| `tests/errors/*.c` | must **not** compile |

`tests/link/multi/` is the second shape: a header and two translation units,
for what only exists between them — a definition in one file and a
declaration in another, a static of the same name in both, a header that
defines a function every unit emits, a tentative definition resolved at the
end of a unit.

`tests/platform/` is where a host's own headers are exercised, and the
program is guarded by the platform macro so that the suite's rule still holds:
off that host it prints its line and exits zero, which is what running it
anywhere else has to do. `platform/windows.c` is `<windows.h>`, which is
where five separate gaps in vcc turned up at once.

`tests/errors/` is the negative suite. Each line is a constraint violation
annotated with the paragraph it breaks, and the test is that `vcc check`
fails. What it *says* is not asserted, because the file itself is the record:

```
vcc check tests/errors/constraints.c
```

prints one diagnostic per line, in order, and reading that output against the
file is how the diagnostics are reviewed. Its positive twin is
`expr/constraints.c`: code that is correct and must stay quiet.

## What every program runs through

`TestPrograms` builds and runs it. Two more compile it a second time, from a
different source text, and run it again:

- `TestPreprocessedInput` — `vcc build --emit i` then compile that. Phase 4's
  output has to re-enter as `.i` input and produce the same program.
- `TestClangPreprocessedInput` — `clang -E` then compile that, which is the
  interoperation claim: clang's headers, clang's `<stdarg.h>`, clang's
  nullability spellings.

So a program added here is compiled three ways, and the failure names which.

## Adding one

Write the program, check it against clang, and put it in the area that fits.
A test that needs a system header should use it — the suite is a hosted one,
and headers are half of what breaks. Name the file after what it covers
rather than after the bug that prompted it: the file outlives the bug, and a
name ending in a digit means the topic wanted a second file it should have
had in the first place.

A program that needs a flag says so on its first line, in paths relative to
`tests/`:

```c
/* vcc-flags: -I preproc/inc_a -I preproc/inc_b */
```

A flag that applies on only one host names it first, which is what
`platform/` needs: the program compiles everywhere, and the library it links
against exists in one place.

```c
/* vcc-flags: windows: -l ws2_32 -l advapi32 */
```

If a case is deliberately outside what vcc implements, say so in the comment
with the paragraph that permits the choice. `gnu/extensions.c` notes where
clang and gcc disagree and which one vcc follows.
