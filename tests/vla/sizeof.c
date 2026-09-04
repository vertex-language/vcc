/* sizeof over a variably modified type, which is the one sizeof that is not
   a constant expression (§6.5.3.4p2).

   Two rules decide every case here. The size expression of a VLA type is
   evaluated when the declaration is reached, once, and the size is fixed
   from then on. sizeof applied to a variably modified *type* evaluates that
   type's size expression there and then — so its operand's side effects
   happen, unlike every other sizeof. */

#include <stdio.h>

static int calls = 0;

static int five(void) {
    calls++;
    return 5;
}

static int sized(int n, int m) {
    int a[n][m];
    return (int)sizeof a;
}

int main(void) {
    /* The size is the one the declaration computed. */
    if (sized(2, 3) != 24) return 1;
    if (sized(5, 5) != 100) return 2;

    /* The size expression runs once, at the declaration; sizeof of the
       object afterwards does not run it again. */
    int a[five()];
    if (calls != 1) return 3;
    if (sizeof a != 20) return 4;
    if (calls != 1) return 5;
    if (sizeof a != 20) return 6;
    if (calls != 1) return 7;

    /* sizeof of a variably modified type evaluates its size expression. */
    int n = 5;
    size_t s = sizeof(int[n++]);
    if (s != 20) return 8;               /* n++ yielded 5 */
    if (n != 6) return 9;                /* and n moved */

    calls = 0;
    s = sizeof(int[five() * 2]);
    if (calls != 1) return 10;
    if (s != 40) return 11;

    /* A pointer to a VLA captures the size where it is declared, so what
       *p measures does not follow later changes to n. */
    n = 5;
    int (*p)[n++] = 0;
    if (n != 6) return 12;
    if (sizeof *p != 20) return 13;      /* five ints, not six */

    /* An ordinary sizeof never evaluates its operand, VLAs aside. */
    calls = 0;
    if (sizeof(int[3]) != 12) return 15;
    if (sizeof five() != sizeof(int)) return 16;
    if (calls != 0) return 17;

    printf("Sizeof VLA OK\n");
    return 0;
}
