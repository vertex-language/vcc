/* §6.7.4's inline, in one translation unit.

   The rule that decides everything: if every file-scope declaration of a
   name has inline and none has extern, the definition is an inline
   definition and provides no external definition. Add extern to any one of
   them and the unit emits it. What happens when two units emit the same one
   is link/inline's business; here it is the calls that matter.

   __inline and __inline__ are gcc's spellings of the specifier, not
   decoration to be discarded — a system header that writes `extern __inline`
   means it. */

#include <stdio.h>

static inline int twice(int x) { return x + x; }
static __inline int thrice(int x) { return 3 * x; }
static __inline__ int fourth(int x) { return 4 * x; }

/* An inline definition with extern on a declaration: this unit emits it. */
inline int emitted(int x) { return x + 1; }
extern int emitted(int x);

/* extern on the definition itself, which is the same decision. */
extern __inline int also_emitted(int x) { return x + 2; }

/* An inline function may have static storage of its own; there is one
   object, not one per call. */
static inline int counted(void) {
    static int n = 0;
    return ++n;
}

/* Recursion through an inline function is a call like any other. */
static inline int fact(int n) { return n <= 1 ? 1 : n * fact(n - 1); }

int main(void) {
    if (twice(21) != 42) return 1;
    if (thrice(5) != 15) return 2;
    if (fourth(5) != 20) return 3;
    if (emitted(41) != 42) return 4;
    if (also_emitted(40) != 42) return 5;

    if (counted() != 1) return 6;
    if (counted() != 2) return 7;
    if (counted() != 3) return 8;

    if (fact(5) != 120) return 9;

    /* Its address is a function pointer like any other, which forces a real
       definition to exist. */
    int (*p)(int) = twice;
    if (p(4) != 8) return 10;
    if (p == 0) return 11;

    printf("Inline OK\n");
    return 0;
}
