#include <stdio.h>

/* Operators whose operand can end the block it started in. A conditional, a
   || and a statement expression each branch, so the operator has to be
   emitted where the operand left off and not where it began — otherwise it
   lands after a terminator and the module does not verify. Every one of
   these was a crash. */

struct big { long a, b, c, d; };
static struct big pick(int c) { struct big x = {1,2,3,4}, y = {5,6,7,8}; return c ? x : y; }
static int neg(int c) { return -(c ? 2 : 3); }
static int notc(int c) { return !(c ? 1.0 : 2.0); }
static int cmp(int c) { return ~(c ? 4 : 5); }
static void vret(int c) { (void)(c ? 1 : 2); return; }
static int shortcircuit(int c) { return !(c && 2); }
static int stmtexpr(int c) { return -({ int t = c; t + 1; }); }

int main(void) {
    if (pick(1).a != 1 || pick(0).d != 8) return 1;
    if (neg(1) != -2 || neg(0) != -3) return 2;
    if (notc(1) != 0) return 3;
    if (cmp(1) != ~4) return 4;
    if (shortcircuit(1) != 0 || shortcircuit(0) != 1) return 5;
    if (stmtexpr(3) != -4) return 6;
    vret(1);
    printf("Block Cursor OK\n");
    return 0;
}
