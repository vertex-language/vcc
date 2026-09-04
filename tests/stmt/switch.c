/* switch: fallthrough, a default that is not last, cases that are not in
   order, and a switch whose cases are woven through a loop — Duff's device,
   which is the case a compiler that treats a switch as a chain of ifs gets
   wrong. */

#include <stdio.h>

static int categorize(int v) {
    switch (v) {
    case 1:
        return 10;
    case 2:                 /* falls through to 3 */
    case 3:
        return 20;
    case 4:
        break;              /* leaves the switch, not the function */
    default:
        return 30;
    }
    return 40;
}

/* default before the cases, and case values out of order: neither is a
   constraint, and both are laid out by value rather than by position. */
static int unordered(int v) {
    int r = 0;
    switch (v) {
    default: r = -1; break;
    case 9:  r = 9;  break;
    case 2:  r = 2;  break;
    case 100: r = 100; break;
    case -3: r = -3; break;
    }
    return r;
}

/* Fallthrough accumulating through several labels. */
static int cascade(int v) {
    int r = 0;
    switch (v) {
    case 3: r += 3;
    case 2: r += 2;
    case 1: r += 1;
    }
    return r;
}

static void duff(short *to, const short *from, int count) {
    int n = (count + 7) / 8;
    switch (count % 8) {
    case 0: do { *to++ = *from++;
    case 7:      *to++ = *from++;
    case 6:      *to++ = *from++;
    case 5:      *to++ = *from++;
    case 4:      *to++ = *from++;
    case 3:      *to++ = *from++;
    case 2:      *to++ = *from++;
    case 1:      *to++ = *from++;
            } while (--n > 0);
    }
}

int main(void) {
    if (categorize(1) != 10) return 1;
    if (categorize(2) != 20) return 2;
    if (categorize(3) != 20) return 3;
    if (categorize(4) != 40) return 4;
    if (categorize(5) != 30) return 5;

    if (unordered(9) != 9) return 6;
    if (unordered(2) != 2) return 7;
    if (unordered(100) != 100) return 8;
    if (unordered(-3) != -3) return 9;
    if (unordered(0) != -1) return 10;

    if (cascade(3) != 6) return 11;
    if (cascade(2) != 3) return 12;
    if (cascade(1) != 1) return 13;
    if (cascade(4) != 0) return 14;

    /* The controlling expression is promoted, so a char selects an int
       case label. */
    char c = 'b';
    int hit = 0;
    switch (c) {
    case 'a': hit = 1; break;
    case 'b': hit = 2; break;
    default:  hit = 3; break;
    }
    if (hit != 2) return 15;

    /* An enum constant is a constant expression and so is a case label. */
    enum { LOW = 1, HIGH = 7 };
    hit = 0;
    switch (HIGH) {
    case LOW:  hit = 1; break;
    case HIGH: hit = 2; break;
    }
    if (hit != 2) return 16;

    short src[20], dst[20] = {0};
    for (int i = 0; i < 20; i++) src[i] = i + 1;
    duff(dst, src, 14);
    if (dst[0] != 1 || dst[13] != 14) return 17;
    if (dst[14] != 0) return 18;

    duff(dst, src, 16);         /* count % 8 == 0: the do-while entry */
    if (dst[15] != 16) return 19;

    printf("Switch OK\n");
    return 0;
}
