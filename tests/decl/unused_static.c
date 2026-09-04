/* A static function this unit never uses is not emitted.
 *
 * Internal linkage means no other unit can name one, so a definition
 * nothing here mentions is a definition nothing can call. Every C compiler
 * drops it, and the reason it matters is what an emitted one drags in: the
 * Windows SDK's stralign.h defines `static __inline ua_CharUpperW` behind
 * a macro guard, and the worker it calls is in no import library an
 * ordinary program links. This file is that shape, with a name no library
 * anywhere has, so it links only if the definition is dropped. */

#include <stdio.h>

/* Declared, never defined, never called. */
extern int no_such_symbol_exists_anywhere(int x);

/* Never used: must not be emitted, so the reference above must not exist
 * in the object either. */
static int unused_wrapper(int x) { return no_such_symbol_exists_anywhere(x); }

/* Nor through another unused static. */
static int unused_caller(int x) { return unused_wrapper(x) + 1; }

/* A used one is emitted, and still works. */
static int doubled(int x) { return x * 2; }

/* Used only through a pointer, which counts as used. */
static int tripled(int x) { return x * 3; }

/* Used only from another static that is itself used. */
static int quadrupled(int x) { return doubled(doubled(x)); }

static int (*const chosen)(int) = tripled;

int main(void) {
    if (doubled(21) != 42) return 1;
    if (chosen(14) != 42) return 2;
    if (quadrupled(10) != 40) return 3;

    printf("Unused static OK\n");
    return 0;
}
