/* vcc-flags: -I preproc/inc_a -I preproc/inc_b */
#include <stdio.h>
#include <wrap.h>

/* __COUNTER__ builds a name that is unique per expansion, which is what a
   macro that declares something needs when it may be used twice in one
   scope. Every assertion macro that declares a static is written this way. */
#define CAT_(a, b) a##b
#define CAT(a, b) CAT_(a, b)
#define UNIQUE(stem) CAT(stem, __COUNTER__)

static int UNIQUE(slot);
static int UNIQUE(slot);

int main(void) {
    if (WRAP_A != 1) return 1;
    if (WRAP_B != 2) return 2;   /* the shadowed header was reached */

    slot0 = 10;
    slot1 = 20;
    if (slot0 + slot1 != 30) return 3;

    /* Each expansion is a new value, and they increase. */
    int a = __COUNTER__, b = __COUNTER__;
    if (b <= a) return 4;

    printf("Include Next OK\n");
    return 0;
}
