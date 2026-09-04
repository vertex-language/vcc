#include <stdio.h>
#include <limits.h>
#include <stdbool.h>

/* Integer conversion, promotion, and the exact semantics C fixes:
   truncation toward zero, the sign of %, unsigned wraparound, and the
   usual arithmetic conversions on mixed-signedness operands. */

static unsigned uf(unsigned x) { return x; }

int main(void) {
    /* §6.5.5p6: / truncates toward zero and a%b has the sign of a. */
    if (-7 / 2 != -3) return 1;
    if (-7 % 2 != -1) return 2;
    if (7 / -2 != -3) return 3;
    if (7 % -2 != 1) return 4;
    if (-7 / -2 != 3) return 5;
    if (-7 % -2 != -1) return 6;

    /* Unsigned wraps; it does not trap and it does not sign-extend. */
    unsigned u = 0;
    u--;
    if (u != UINT_MAX) return 7;
    if (u + 1 != 0) return 8;
    if ((unsigned)(-1) != UINT_MAX) return 9;

    /* §6.3.1.8: int meets unsigned int, and int converts. -1 becomes huge. */
    if ((unsigned)1 > -1) return 10;   /* -1 becomes UINT_MAX, so 1 is not greater */
    if ((-1 < 1u) != 0) return 11;

    /* Narrowing is modular for unsigned targets and implementation-defined
       (two's complement here) for signed ones. */
    unsigned char c = (unsigned char)300;
    if (c != 44) return 12;
    signed char s = (signed char)200;
    if (s != -56) return 13;
    short h = (short)70000;
    if (h != 4464) return 14;

    /* Integer promotion: char and short promote to int before arithmetic,
       so this multiplication is not done in 8 bits. */
    unsigned char a = 200, b = 200;
    if (a * b != 40000) return 15;
    if ((unsigned char)(a * b) != 64) return 16;

    /* Shifts. A left shift of a signed value is defined while the result
       fits; the right shift of a negative int is arithmetic here. */
    if ((1 << 10) != 1024) return 17;
    if ((-8 >> 1) != -4) return 18;
    if ((1u << 31) != 2147483648u) return 19;
    if ((int)(1u << 31) >= 0) return 20;
    long long big = 1LL << 40;
    if (big != 1099511627776LL) return 21;
    if ((big >> 20) != 1048576LL) return 22;

    /* 64-bit arithmetic that a 32-bit lowering would get wrong. */
    long long p = 3037000499LL * 3037000499LL;
    if (p != 9223372030926249001LL) return 23;
    unsigned long long uu = 18446744073709551615ULL;
    if (uu + 1 != 0) return 24;
    if (uu / 3 != 6148914691236517205ULL) return 25;

    /* Mixed width: the narrower operand converts to the wider type. */
    int i = -1;
    unsigned long long w = 1;
    if ((i + w) != 0) return 26;

    /* Limits are what limits.h says they are. */
    if (INT_MAX != 2147483647) return 27;
    if (INT_MIN + 1 != -2147483647) return 28;
    if (LLONG_MAX != 9223372036854775807LL) return 29;
    if (CHAR_BIT != 8) return 30;

    /* Overflow of INT_MIN / -1 is undefined, so it is not tested; the
       unsigned equivalent is defined and is. */
    if (uf(0u) - 1u != UINT_MAX) return 31;

    /* §6.3.1.2: a conversion to _Bool is a comparison against zero, so any
       value that is not zero becomes 1 — including a float too small to
       survive a truncation to int. */
    bool t = true, f = false;
    if (t != 1) return 32;
    if (f != 0) return 33;
    t = 42;
    if (t != 1) return 34;
    f = 0.5f;
    if (f != 1) return 35;
    f = 0.0;
    if (f != 0) return 36;
    t = (bool)256;              /* not a narrowing to char: still 1 */
    if (t != 1) return 37;

    printf("Integers OK\n");
    return 0;
}
