/* Layout: where the padding goes, what offsetof says about it, and that a
   struct with holes survives being passed, returned and compared as a value.

   The numbers here are this ABI's, and every one of them agrees with clang
   on the same target — which is the point of checking them at all. */

#include <stdio.h>
#include <stddef.h>
#include <string.h>

struct pad {
    char c;         /* offset 0, seven bytes of padding after it */
    double d;       /* offset 8 */
    int i;          /* offset 16, four bytes of tail padding */
};

struct small { char a, b; };

struct nested {
    char c;
    struct pad inner;    /* aligned as its widest member requires */
};

static int sum(struct pad p) { return p.c + (int)p.d + p.i; }

static struct pad make(char c, double d, int i) {
    struct pad p = {c, d, i};
    return p;
}

int main(void) {
    if (sizeof(struct pad) != 24) return 1;
    if (offsetof(struct pad, c) != 0) return 2;
    if (offsetof(struct pad, d) != 8) return 3;
    if (offsetof(struct pad, i) != 16) return 4;
    if (_Alignof(struct pad) != _Alignof(double)) return 5;

    /* Two chars need no padding between them, and none at the end. */
    if (sizeof(struct small) != 2) return 6;
    if (_Alignof(struct small) != 1) return 7;

    /* A struct member is aligned as strictly as the struct itself. */
    if (offsetof(struct nested, inner) != 8) return 8;
    if (sizeof(struct nested) != 32) return 9;

    struct pad p = {'a', 3.14, 42};
    if (p.c != 'a' || p.d != 3.14 || p.i != 42) return 10;

    /* Passed and returned by value, holes included. */
    struct pad q = {1, 2.0, 3};
    if (sum(q) != 6) return 11;
    struct pad r = make('z', 1.5, 7);
    if (r.c != 'z' || r.d != 1.5 || r.i != 7) return 12;

    /* Assignment copies the padding too, which is what makes memcmp of two
       copies meaningful — the bytes came from the same object. */
    struct pad copy = r;
    if (memcmp(&copy, &r, sizeof r) != 0) return 13;

    printf("Padding OK\n");
    return 0;
}
