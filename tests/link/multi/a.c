#include <stdio.h>
#include "shared.h"

static int local_only(int x) { return x * 3; }

int main(void) {
    if (add(2, 3) != 5) return 1;
    if (local_only(4) != 12) return 2;
    if (b_local(4) != 28) return 12;   /* b.c's static of the same name */

    struct point p = {3, 4};
    struct point q = scale(p, 5);
    if (q.x != 15 || q.y != 20) return 3;
    if (p.x != 3 || p.y != 4) return 4;   /* passed by value */

    if (counter != 0) return 5;
    if (bump() != 1) return 6;
    if (bump() != 2) return 7;
    if (counter != 2) return 8;

    counter = 40;
    if (bump() != 41) return 9;

    if (label[0] != 'v') return 10;
    if (twice(21) != 42) return 11;

    printf("Multi OK\n");
    return 0;
}
