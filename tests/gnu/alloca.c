/* alloca, which is the compiler's and not the library's.
 *
 * The storage has to come from the caller's own frame — a call could only
 * return storage from its own, and that frame is gone by the time the caller
 * looks at it — so gcc and clang both answer alloca in the compiler and vcc
 * lowers it to the same ptr.alloca a VLA uses. Released at function exit,
 * which is what makes alloca in a loop a leak rather than a reuse, and is
 * gcc's behaviour too.
 */
#include <stdio.h>

static int fill(int n) {
    char *p = __builtin_alloca((size_t)n);
    for (int i = 0; i < n; i++) p[i] = (char)('a' + i % 26);
    int t = 0;
    for (int i = 0; i < n; i++) t += p[i];
    return t;
}

static long two(void) {
    long *a = __builtin_alloca(sizeof(long) * 4);
    long *b = __builtin_alloca(sizeof(long) * 4);
    for (int i = 0; i < 4; i++) { a[i] = i; b[i] = i * 10; }
    long t = 0;
    for (int i = 0; i < 4; i++) t += a[i] + b[i];
    return t + (a == b ? 1000 : 0);
}

int main(void) {
    if (fill(4) != 'a' + 'b' + 'c' + 'd') return 1;
    if (fill(30) <= 0) return 2;
    if (two() != 66) return 3;

    /* the alignment is the strictest the target has, so a long fits */
    char *raw = __builtin_alloca(16);
    for (int i = 0; i < 16; i++) raw[i] = 0;
    *(long *)raw = 42;
    if (*(long *)raw != 42) return 4;

    printf("Alloca OK\n");
    return 0;
}
