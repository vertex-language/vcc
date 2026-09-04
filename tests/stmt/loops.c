/* The three loops and the two jumps inside them, including where they land
   when the loops are nested: break and continue bind to the innermost
   enclosing loop, and continue in a do-while goes to the controlling
   expression rather than past it. */

#include <stdio.h>

int main(void) {
    /* for, and the scope of its declaration. */
    int sum = 0;
    for (int i = 0; i < 10; i++) sum += i;
    if (sum != 45) return 1;

    /* An alternating sum, so the else arm has to run too. */
    sum = 0;
    for (int i = 0; i < 10; i++) {
        if (i % 2 == 0) sum += i;
        else sum -= i;
    }
    if (sum != -5) return 2;

    /* while and do-while, which differ on an initially false condition. */
    int n = 0, ran = 0;
    while (n < 5) { ran++; n++; }
    if (n != 5 || ran != 5) return 3;

    ran = 0;
    while (0) ran++;
    if (ran != 0) return 4;

    ran = 0;
    do { ran++; } while (0);
    if (ran != 1) return 5;

    n = 0; sum = 0;
    do { sum += 2; n++; } while (n < 3);
    if (sum != 6) return 6;

    /* continue: the odd numbers only, in each of the three forms. */
    sum = 0;
    for (int i = 0; i < 10; i++) {
        if (i % 2 == 0) continue;
        sum += i;
    }
    if (sum != 25) return 7;

    sum = 0; n = 0;
    while (n < 10) {
        int cur = n++;
        if (cur % 2 == 0) continue;
        sum += cur;
    }
    if (sum != 25) return 8;

    sum = 0; n = 0;
    do {
        int cur = n++;
        if (cur % 2 == 0) continue;   /* to the condition, not past it */
        sum += cur;
    } while (n < 10);
    if (sum != 25) return 9;

    /* break leaves the loop it is in and no more than that. */
    sum = 0;
    for (int i = 0; i < 100; i++) {
        if (i == 5) break;
        sum += i;
    }
    if (sum != 10) return 10;

    int outer = 0, inner = 0;
    for (int i = 0; i < 3; i++) {
        outer++;
        for (int j = 0; j < 10; j++) {
            if (j == 2) break;
            inner++;
        }
    }
    if (outer != 3 || inner != 6) return 11;

    outer = 0; inner = 0;
    for (int i = 0; i < 3; i++) {
        for (int j = 0; j < 4; j++) {
            if (j % 2) continue;
            inner++;
        }
        outer++;
    }
    if (outer != 3 || inner != 6) return 12;

    /* An omitted condition is true, and the loop leaves by break. */
    n = 0;
    for (;;) {
        if (++n == 4) break;
    }
    if (n != 4) return 13;

    /* An empty body is a statement. */
    n = 0;
    while (n++ < 3);
    if (n != 4) return 14;

    printf("Loops OK\n");
    return 0;
}
