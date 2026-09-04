#include <stdio.h>
#include <string.h>

/* Aggregate initialization in the shapes §6.7.9 admits: nested braces and
   brace elision, designators mixed with positional entries, partial
   initialization zeroing the rest, compound literals with automatic and
   static duration, arrays of structs, and unions. */

struct inner { int a, b; };
struct outer {
    int tag;
    struct inner in[2];
    char name[6];
    union { int i; float f; char c[4]; } u;
};

static const struct outer table[] = {
    { .tag = 1, .in = {{1, 2}, {3, 4}}, .name = "one", .u = {.i = 0x01020304} },
    { 2, {{5, 6}, {7, 8}}, "two", {9} },
    { .tag = 3 },                        /* everything else is zero */
    [5] = { .tag = 6, .name = {'s','i','x'} },
};

/* Brace elision: the inner braces may be left out entirely. */
static const int grid[3][4] = {
    1, 2, 3, 4,
    5, 6, 7, 8,
    9, 10, 11, 12,
};

static const int sparse[8] = {[7] = 70, [3] = 30, 31, 32};

/* A compound literal at file scope has static duration, so its address is
   an address constant and may initialize another static object. */
static const struct inner *const file_lit = &(const struct inner){7, 8};

int main(void) {
    if (sizeof table / sizeof table[0] != 6) return 1;
    if (table[0].tag != 1 || table[0].in[1].b != 4) return 2;
    if (strcmp(table[0].name, "one") != 0) return 3;
    if (table[0].u.i != 0x01020304) return 4;
    if (table[1].tag != 2 || table[1].in[0].a != 5) return 5;
    if (strcmp(table[1].name, "two") != 0) return 6;
    if (table[1].u.i != 9) return 7;
    if (table[2].tag != 3 || table[2].in[0].a != 0 || table[2].name[0] != 0) return 8;
    if (table[3].tag != 0 || table[4].tag != 0) return 9;
    if (table[5].tag != 6) return 10;
    if (table[5].name[0] != 's' || table[5].name[3] != 0) return 11;

    if (grid[0][0] != 1 || grid[1][0] != 5 || grid[2][3] != 12) return 12;
    if (sizeof grid != 12 * sizeof(int)) return 13;

    /* A designator resets the cursor; what follows continues from there. */
    if (sparse[3] != 30 || sparse[4] != 31 || sparse[5] != 32) return 14;
    if (sparse[7] != 70) return 15;
    if (sparse[0] != 0 || sparse[6] != 0) return 16;

    /* Automatic aggregates, initialized from run-time values. */
    int n = 4;
    struct inner local[2] = {{n, n + 1}, {.b = n + 2}};
    if (local[0].a != 4 || local[0].b != 5) return 17;
    if (local[1].a != 0 || local[1].b != 6) return 18;

    /* Partial initialization zeroes the tail, including padding-adjacent
       members. */
    struct outer part = {.tag = 9};
    if (part.tag != 9) return 19;
    for (size_t i = 0; i < sizeof part.name; i++)
        if (part.name[i] != 0) return 20;
    if (part.in[0].a || part.in[1].b || part.u.i) return 21;

    /* Compound literals: an unnamed object with the enclosing block's
       duration, and an lvalue you may take the address of. */
    struct inner *p = &(struct inner){10, 20};
    if (p->a != 10 || p->b != 20) return 22;
    p->a = 11;
    if (p->a != 11) return 23;
    if (((struct inner){1, 2}).b != 2) return 24;

    int *arr = (int[]){1, 2, 3, 4};
    if (arr[3] != 4) return 25;

    /* file_lit is a compound literal at file scope: static duration, and
       its address is a constant expression. */
    if (file_lit->a != 7 || file_lit->b != 8) return 26;

    /* Array-to-pointer decay in a conditional: both arms are pointers. */
    int u[3] = {1, 2, 3}, v[3] = {4, 5, 6};
    int *sel = n ? u : v;
    if (sel[0] != 1) return 27;
    sel = (n == 0) ? u : v;
    if (sel[0] != 4) return 28;

    /* Brace elision is not a different initializer: the flat form fills the
       same members in the same order. (Member by member, not memcmp: the
       padding between them is unspecified in an object nobody copied.) */
    struct outer braced = {7, {{1, 2}, {3, 4}}, "ok", {5}};
    struct outer flat   = {7, 1, 2, 3, 4, 'o', 'k', 0, 0, 0, 0, 5};
    if (braced.tag != flat.tag) return 32;
    if (braced.in[0].a != flat.in[0].a || braced.in[0].b != flat.in[0].b) return 33;
    if (braced.in[1].a != flat.in[1].a || braced.in[1].b != flat.in[1].b) return 34;
    if (memcmp(braced.name, flat.name, sizeof braced.name) != 0) return 35;
    if (braced.u.i != flat.u.i) return 36;

    /* {0} zeroes the whole object whatever its shape. */
    struct outer zero = {0};
    if (zero.tag || zero.in[1].b || zero.name[5] || zero.u.i) return 37;

    /* Assigning a whole struct copies every member. */
    struct outer copy = table[1];
    if (copy.in[1].a != 7 || strcmp(copy.name, "two") != 0) return 29;
    copy.tag = 99;
    if (table[1].tag != 2) return 30;

    /* A union writes one member and reads it back; the others alias. */
    union { int i; char c[4]; } un;
    un.i = 0;
    un.c[0] = 1;
    if (un.i != 1) return 31;      /* little-endian target */

    printf("Init OK\n");
    return 0;
}
