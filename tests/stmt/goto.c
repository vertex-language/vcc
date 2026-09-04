/* goto: backward to make a loop, forward out of a nest, and into the tail of
   a function where the cleanup lives — the shape C code actually uses it
   for. A label is a statement's name, so one at the end of a block needs a
   statement to name (§6.8.1). */

#include <stdio.h>

static int cleanup(int fail) {
    int acquired = 0, released = 0, code = 0;

    acquired = 1;
    if (fail == 1) { code = 1; goto done; }

    acquired = 2;
    if (fail == 2) { code = 2; goto done; }

    acquired = 3;

done:
    released = acquired;        /* the one path every exit shares */
    return code * 100 + released;
}

int main(void) {
    /* A loop built out of a backward goto. */
    int sum = 0, i = 0;
loop:
    if (i >= 10) goto after;
    if (i % 2 == 0) { i++; goto loop; }
    sum += i;
    i++;
    goto loop;
after:
    if (sum != 25) return 1;

    /* Forward, out of two nested blocks at once. */
    int found = 0;
    for (int a = 0; a < 5; a++) {
        for (int b = 0; b < 5; b++) {
            if (a * b == 6) {
                found = a * 10 + b;
                goto escaped;
            }
        }
    }
escaped:
    if (found != 23) return 2;

    if (cleanup(0) != 3) return 3;
    if (cleanup(1) != 101) return 4;
    if (cleanup(2) != 202) return 5;

    /* Jumping forward over a declaration is allowed; the object exists but
       its initializer did not run, so it is not read here. */
    goto skip;
    int unread = 7;
skip:
    unread = 1;
    if (unread != 1) return 6;

    /* A label at the end of a block names the null statement. */
    {
        goto tail;
    tail: ;
    }

    printf("Goto OK\n");
    return 0;
}
