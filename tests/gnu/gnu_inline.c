#include <stdio.h>

/* gnu_inline: gcc 89's inline, which means the opposite of C99's.

   `extern inline` in C99 provides an external definition. With the
   gnu_inline attribute it provides none -- the definition is there to be
   inlined, and some other unit or library has the real one. Darwin's
   <sys/cdefs.h> spells __header_inline that way for any compiler that says
   __GNUC_STDC_INLINE__, so every system header full of them depends on the
   distinction being kept.

   These carry always_inline as well, which is how the system headers
   spell it: __header_always_inline is the pair. gcc and clang satisfy a
   call by inlining the body; vcc has no inliner and emits a definition
   for the ones this unit uses instead. Either way the call resolves and
   the unused ones are absent, which is the part that matters -- an unused
   one that was emitted would drag its own references along. */

extern int add_one(int x);
extern __inline__ __attribute__((__gnu_inline__, __always_inline__)) int add_one(int x) {
    return x + 1;
}

/* Never called. Its body names a function nothing defines, so if this were
   emitted the link would fail on a symbol the program never uses -- which
   is exactly what a system header's unused inlines used to do. */
extern int nowhere(int);
extern __inline__ __attribute__((__gnu_inline__, __always_inline__)) int never_called(int x) {
    return nowhere(x);
}

/* The doubled-underscore spelling and the bare one are one attribute. */
extern int add_two(int x);
extern __inline__ __attribute__((gnu_inline, always_inline)) int add_two(int x) {
    return x + 2;
}

int main(void) {
    if (add_one(1) != 2) return 1;
    if (add_two(1) != 3) return 2;
    if (add_one(add_two(0)) != 3) return 3;

    printf("gnu_inline OK\n");
    return 0;
}
