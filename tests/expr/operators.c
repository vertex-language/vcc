/* Operators, judged by what they evaluate rather than what they compute:
   how many times an operand is read, which arm of a conditional runs, what a
   short-circuit skips. The values themselves — conversions, promotions,
   widths — are integers.c's and floats.c's business.

   Every rule here is one a compiler can pass by accident with a simpler
   program: an operand evaluated twice still gives the right answer until it
   has a side effect. */

#include <stdio.h>

static int calls = 0;

static int *counted(int *p) {
    calls++;
    return p;
}

static int bump(int *n) {
    (*n)++;
    return 1;
}

int main(void) {
    /* The binary arithmetic, so the file stands on its own as the operator
       list. What each one does to values of other widths is integers.c's. */
    int a = 10, b = 3;
    if (a + b != 13) return 100;
    if (a - b != 7) return 101;
    if (a * b != 30) return 102;
    if (a / b != 3) return 103;
    if (a % b != 1) return 104;
    if (-a != -10 || +a != 10) return 105;
    if ((a << 1) != 20 || (a >> 1) != 5) return 106;

    /* §6.5.2.4, §6.5.3.1: the operand of ++ is evaluated once, whichever
       side of it the operator is on, and the value is before or after. */
    int x = 5;
    if ((*counted(&x))++ != 5) return 1;
    if (x != 6) return 2;
    if (calls != 1) return 3;
    if (++(*counted(&x)) != 7) return 4;
    if (x != 7) return 5;
    if (calls != 2) return 6;
    if (x-- != 7 || x != 6) return 7;
    if (--x != 5 || x != 5) return 8;

    /* Compound assignment reads its left operand once too. */
    calls = 0;
    *counted(&x) += 10;
    if (x != 15 || calls != 1) return 9;

    /* Bitwise, for completeness — these have no ordering to get wrong. */
    if ((0xF0 | 0x0F) != 0xFF) return 10;
    if ((0xFF & 0x0F) != 0x0F) return 11;
    if ((0xFF ^ 0x0F) != 0xF0) return 12;
    if (~0 != -1) return 13;

    /* §6.5.13p4, §6.5.14p4: && and || yield int 0 or 1, and the right
       operand is not evaluated at all when the left decides the answer. */
    int n = 0;
    if ((0 && bump(&n)) != 0) return 14;
    if (n != 0) return 15;
    if ((1 || bump(&n)) != 1) return 16;
    if (n != 0) return 17;
    if ((1 && bump(&n)) != 1) return 18;
    if (n != 1) return 19;
    if ((0 || bump(&n)) != 1) return 20;
    if (n != 2) return 21;

    /* A comparison is an int, and so is !. */
    if ((7 > 3) != 1) return 22;
    if ((7 < 3) != 0) return 23;
    if (!7 != 0) return 24;
    if (!0 != 1) return 25;

    /* A floating operand is compared against zero, not truncated. */
    float half = 0.5f;
    if ((1 && half) != 1) return 26;
    if ((0.0f || 0.0) != 0) return 27;

    /* §6.5.15p4: the conditional evaluates exactly one of its arms. */
    n = 0;
    if (((7 > 3) ? 100 : bump(&n)) != 100) return 28;
    if (n != 0) return 29;
    if (((7 < 3) ? bump(&n) : 200) != 200) return 30;
    if (n != 0) return 31;
    if ((1 ? (1 ? 30 : 40) : 50) != 30) return 32;

    /* §6.5.17: the comma operator evaluates both, in order, and yields the
       value of the right one. */
    n = 0;
    if ((bump(&n), bump(&n), 9) != 9) return 33;
    if (n != 2) return 34;

    /* Assignment is an expression, and it yields the value stored. */
    int y = 0;
    if ((y = 4) != 4 || y != 4) return 35;
    int z = 0;
    y = z = 7;
    if (y != 7 || z != 7) return 36;

    printf("Operators OK\n");
    return 0;
}
