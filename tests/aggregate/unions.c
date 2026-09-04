/* Unions: one storage for several types, read back as whichever member was
   written, passed and returned whole. */

#include <stdio.h>
#include <string.h>

union data {
    int i;
    float f;
};

union wide {
    int i;
    double d;
    char bytes[8];
};

static int take(union wide u) { return u.i; }

static union data make(float f) {
    union data d;
    d.f = f;
    return d;
}

int main(void) {
    /* Reading a member other than the last written one is how C spells a
       reinterpretation, and the bits are the ones IEEE-754 defines. */
    union data d;
    d.i = 1082130432;
    if (d.f != 4.0f) return 1;
    d.f = 1.0f;
    if (d.i != 1065353216) return 2;

    /* The size is the widest member's, and every member starts at offset 0. */
    union wide w;
    if (sizeof w != sizeof(double)) return 3;
    if ((void *)&w.i != (void *)&w.d) return 4;
    if ((void *)&w.bytes[0] != (void *)&w.i) return 5;

    /* Writing one member changes the bytes the others see. */
    memset(&w, 0, sizeof w);
    w.i = 0x41424344;
    if (w.bytes[0] != 0x44) return 6;    /* little-endian, as every target is */
    if (w.bytes[3] != 0x41) return 7;

    /* A union is passed and returned by value like any other object. */
    w.i = 42;
    if (take(w) != 42) return 8;
    union data made = make(2.5f);
    if (made.f != 2.5f) return 9;

    /* An initializer with no designator initializes the first member. */
    union data first = {7};
    if (first.i != 7) return 10;
    union data named = {.f = 0.5f};
    if (named.f != 0.5f) return 11;

    /* Assignment copies the whole union. */
    union data copy = named;
    if (copy.f != 0.5f) return 12;

    printf("Unions OK\n");
    return 0;
}
