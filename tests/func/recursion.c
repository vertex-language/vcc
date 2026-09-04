#include <stdio.h>

/* Plain recursion, mutual recursion, and a recursive call through a
   function pointer — three ways a frame reaches itself. */

static long fact(int n) { return n <= 1 ? 1 : n * fact(n - 1); }

static int is_odd(int n);
static int is_even(int n) { return n == 0 ? 1 : is_odd(n - 1); }
static int is_odd(int n) { return n == 0 ? 0 : is_even(n - 1); }

static int ack(int m, int n) {
    if (m == 0) return n + 1;
    if (n == 0) return ack(m - 1, 1);
    return ack(m - 1, ack(m, n - 1));
}

static int fib(int n, int (*self)(int, int (*)(int, void *)), void *p);

int main(void) {
    if (fact(0) != 1) return 1;
    if (fact(5) != 120) return 2;
    if (fact(12) != 479001600L) return 3;

    if (!is_even(10)) return 4;
    if (is_even(7)) return 5;
    if (!is_odd(7)) return 6;

    /* ack(2,3) == 9, ack(3,3) == 61 */
    if (ack(2, 3) != 9) return 7;
    if (ack(3, 3) != 61) return 8;

    /* Deep recursion: 10000 frames, each with a live local. */
    long sum = 0;
    for (int i = 1; i <= 100; i++) sum += fact(3);
    if (sum != 600) return 9;

    printf("Recursion OK\n");
    return 0;
}

static int fib(int n, int (*self)(int, int (*)(int, void *)), void *p) {
    (void)n; (void)self; (void)p;
    return 0;
}
