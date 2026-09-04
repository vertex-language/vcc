/* The Microsoft x64 calling convention, exercised rather than inspected.
 *
 * It is not SysV with the registers renamed. There is one argument
 * sequence, not two: argument i takes register i of whichever file its type
 * belongs to, so a double in the second position takes XMM1 and spends RDX
 * without using it. Past the fourth, arguments go on the stack, over a
 * thirty-two byte home area the caller reserves whether or not anyone
 * writes it.
 *
 * A mixed signature is what tells the two conventions apart: under SysV the
 * third argument here would be the second integer register, and under this
 * one it is the third register overall. Getting it wrong does not crash —
 * it reads the wrong register and returns a plausible number — so the
 * checks are on values, and every argument is distinct. */

#include <stdio.h>

#if defined(_WIN32) || defined(__unix__) || defined(__APPLE__)

/* Mixed, within the four register positions. */
static long long mixed(int a, double b, int c, double d) {
    return (long long)a * 1000000 + (long long)b * 10000 + (long long)c * 100 + (long long)d;
}

/* Past the fourth, where the home area ends and the stack begins. */
static long long spilled(int a, int b, int c, int d, int e, int f, int g) {
    return (long long)a * 1000000 + (long long)b * 100000 + (long long)c * 10000 +
           (long long)d * 1000 + (long long)e * 100 + (long long)f * 10 + g;
}

/* Floats past the fourth position too, which is the other half of the one
 * sequence: the fifth argument is on the stack whatever file it belongs to. */
static double floats(double a, double b, double c, double d, double e, double f) {
    return a * 100000 + b * 10000 + c * 1000 + d * 100 + e * 10 + f;
}

/* An aggregate travels in a register only when it is one, two, four or
 * eight bytes wide; anything else travels as the address of a copy. Both
 * shapes, and an integer after each, so a mistake about which one consumed
 * a register shows up in the argument that follows it. */
struct Small { int x, y; };            /* eight bytes: in a register */
struct Big   { long long a, b, c; };   /* twenty-four: by address */

static long long small(struct Small s, int after) { return s.x * 10000 + s.y * 100 + after; }
static long long big(struct Big b, int after) { return b.a * 10000 + b.b * 1000 + b.c * 100 + after; }

/* Variadic, where the home area is what makes a va_list a bare pointer. */
static long long sum(int n, ...) {
    __builtin_va_list ap;
    __builtin_va_start(ap, n);
    long long total = 0;
    for (int i = 0; i < n; i++) total = total * 10 + __builtin_va_arg(ap, int);
    __builtin_va_end(ap);
    return total;
}

int main(void) {
    if (mixed(1, 2.0, 3, 4.0) != 1020304LL) return 1;
    if (spilled(1, 2, 3, 4, 5, 6, 7) != 1234567LL) return 2;
    if (floats(1, 2, 3, 4, 5, 6) != 123456.0) return 3;

    struct Small s = {1, 2};
    if (small(s, 3) != 10203LL) return 4;

    struct Big b = {1, 2, 3};
    if (big(b, 4) != 12304LL) return 5;

    if (sum(5, 1, 2, 3, 4, 5) != 12345LL) return 6;

    /* Through a function pointer, which reaches the same lowering by a
     * different route. */
    long long (*f)(int, double, int, double) = mixed;
    if (f(9, 8.0, 7, 6.0) != 9080706LL) return 7;

    printf("Calling convention OK\n");
    return 0;
}

#else

int main(void) {
    printf("Calling convention OK (not this host)\n");
    return 0;
}

#endif
