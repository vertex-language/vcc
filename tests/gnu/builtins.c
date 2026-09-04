#include <stdio.h>
#include <math.h>
#include <string.h>
#include <stdlib.h>

/* The GCC builtins vcc implements. Each is here because it cannot be
   written in C: __builtin_fabs is the machine's absolute value rather
   than a call to libm, __builtin_inf names a value no literal spells,
   __builtin_constant_p asks a question only the compiler can answer,
   and the byte swaps are one instruction no expression reliably
   becomes. Every platform header on this machine reaches for several. */

static int called;
static int side(void) { return ++called; }

int main(void) {
    /* Byte swaps at each width, including the ends. */
    if (__builtin_bswap16(0x1234) != 0x3412) return 1;
    if (__builtin_bswap32(0x01020304u) != 0x04030201u) return 2;
    if (__builtin_bswap64(0x0102030405060708ULL) != 0x0807060504030201ULL) return 3;
    if (__builtin_bswap32(0) != 0) return 4;
    if (__builtin_bswap64(0xFFULL) != 0xFF00000000000000ULL) return 5;

    /* Absolute value and the values no literal spells. */
    if (__builtin_fabs(-2.5) != 2.5) return 6;
    if (__builtin_fabsf(-1.5f) != 1.5f) return 7;
    if (__builtin_fabsl(-3.5L) != 3.5L) return 8;
    if (!isinf(__builtin_inf())) return 9;
    if (!isinf(__builtin_inff())) return 10;
    if (__builtin_inf() <= 0) return 11;
    if (__builtin_huge_val() != __builtin_inf()) return 12;
    if (!isnan(__builtin_nan(""))) return 13;

    /* The quiet comparisons, which compare, and are false for a NaN. */
    double n = __builtin_nan("");
    if (!__builtin_isgreater(2.0, 1.0)) return 14;
    if (__builtin_isgreater(1.0, 2.0)) return 15;
    if (!__builtin_islessequal(1.0, 1.0)) return 16;
    if (__builtin_isless(n, 1.0)) return 17;
    if (!__builtin_isunordered(n, 1.0)) return 18;
    if (__builtin_isunordered(1.0, 2.0)) return 19;
    if (__builtin_islessgreater(n, n)) return 20;
    if (!__builtin_islessgreater(1.0, 2.0)) return 21;

    /* __builtin_constant_p, and the promise that its argument is not
       evaluated. */
    if (!__builtin_constant_p(5)) return 22;
    if (!__builtin_constant_p(2 + 3)) return 23;
    called = 0;
    (void)__builtin_constant_p(side());
    if (called != 0) return 24;

    /* Bit counts over constants. */
    if (__builtin_popcount(0xFF) != 8) return 25;
    if (__builtin_popcount(0) != 0) return 26;
    if (__builtin_ctz(8) != 3) return 27;

    /* The ones that name a library function lower to a call to it. */
    char buf[8];
    __builtin_memset(buf, 'x', 4);
    buf[4] = 0;
    if (strcmp(buf, "xxxx") != 0) return 28;
    if (__builtin_strlen("abc") != 3) return 29;
    if (__builtin_abs(-7) != 7) return 30;
    if (__builtin_abs(7) != 7) return 31;
    if (__builtin_labs(-8L) != 8L) return 32;
    if (__builtin_llabs(-9LL) != 9LL) return 33;

    printf("Builtins OK\n");
    return 0;
}
