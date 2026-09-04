/* Variadic functions: the default argument promotions §6.5.2.2p6 applies to
   every argument past the prototype, va_copy, and a va_list handed to
   another function to finish reading. */

#include <stdio.h>
#include <stdarg.h>
#include <string.h>

static int sum_all(int count, ...) {
    va_list ap;
    va_start(ap, count);
    int sum = 0;
    for (int i = 0; i < count; i++) sum += va_arg(ap, int);
    va_end(ap);
    return sum;
}

/* float promotes to double, char and short promote to int: what arrives is
   never the declared type of the argument at the call site. */
static int promoted(int count, ...) {
    va_list ap;
    va_start(ap, count);
    double d = va_arg(ap, double);
    int c = va_arg(ap, int);
    int s = va_arg(ap, int);
    unsigned u = va_arg(ap, unsigned);
    va_end(ap);

    if (d != (double)3.14f) return 1;
    if (c != 'A') return 2;
    if (s != -42) return 3;
    if (u != 7u) return 4;
    return 0;
}

/* A va_list passed on, so the callee reads the rest of the arguments. */
static int rest(va_list ap, int n) {
    int t = 0;
    for (int i = 0; i < n; i++) t += va_arg(ap, int);
    return t;
}

static int split(int n, ...) {
    va_list ap;
    va_start(ap, n);
    int first = va_arg(ap, int);
    int tail = rest(ap, n - 1);
    va_end(ap);
    return first * 1000 + tail;
}

/* va_copy: two independent walks over the same arguments. */
static int twice_over(int n, ...) {
    va_list ap, copy;
    va_start(ap, n);
    va_copy(copy, ap);

    int a = 0, b = 0;
    for (int i = 0; i < n; i++) a += va_arg(ap, int);
    for (int i = 0; i < n; i++) b += va_arg(copy, int);

    va_end(copy);
    va_end(ap);
    return a == b ? a : -1;
}

/* Mixed integer and floating arguments, which travel in different places. */
static double mixed(int n, ...) {
    va_list ap;
    va_start(ap, n);
    double t = 0;
    for (int i = 0; i < n; i++) {
        t += va_arg(ap, int);
        t += va_arg(ap, double);
    }
    va_end(ap);
    return t;
}

int main(void) {
    if (sum_all(0) != 0) return 1;
    if (sum_all(4, 1, 2, 3, 4) != 10) return 2;
    if (sum_all(9, 1,1,1,1,1,1,1,1,1) != 9) return 3;

    float f = 3.14f;
    char c = 'A';
    short s = -42;
    unsigned char u = 7;
    if (promoted(4, f, c, s, u) != 0) return 4;

    if (split(4, 1, 2, 3, 4) != 1009) return 5;
    if (twice_over(3, 5, 6, 7) != 18) return 6;
    if (mixed(2, 1, 0.5, 2, 0.25) != 3.75) return 7;

    /* vsnprintf is the library's own use of the same mechanism. */
    char buf[32];
    snprintf(buf, sizeof buf, "%d/%s/%.1f", 42, "x", 1.5);
    if (strcmp(buf, "42/x/1.5") != 0) return 8;

    printf("Variadic OK\n");
    return 0;
}
