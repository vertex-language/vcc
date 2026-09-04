#include <stdio.h>

/* Scope, linkage, and storage duration: shadowing at every level, the
   two namespaces a tag and an ordinary identifier live in, static locals
   with their own lifetime, and a tentative definition resolved at the
   end of the unit. */

int tentative;               /* §6.9.2p2: defined here, zero-initialized */
int tentative;               /* the same object, not a second one */

static int file_scope = 5;
static int shadow = 1;

struct thing { int x; };     /* tag namespace */
typedef struct thing thing;  /* ordinary namespace, same type */
int thing_count = 0;         /* ordinary namespace, unrelated to the tag */

static int counter(void) {
    static int n = 0;        /* one object for the life of the program */
    return ++n;
}

static int reentrant(int depth) {
    int local = depth;       /* a new object per call */
    if (depth > 0) {
        int inner = reentrant(depth - 1);
        return local + inner;
    }
    return local;
}

int main(void) {
    if (tentative != 0) return 1;
    if (file_scope != 5) return 2;

    /* A block-scope declaration hides the file-scope one, and the outer
       object is untouched. */
    int shadow = 2;
    if (shadow != 2) return 3;
    {
        int shadow = 3;
        if (shadow != 3) return 4;
        {
            char shadow = 4;
            if (shadow != 4) return 5;
            if (sizeof shadow != 1) return 6;
        }
        if (shadow != 3) return 7;
    }
    if (shadow != 2) return 8;

    /* A tag and an ordinary identifier of the same spelling coexist. */
    struct thing t = {7};
    thing t2 = {8};
    thing_count = t.x + t2.x;
    if (thing_count != 15) return 9;
    if (sizeof(struct thing) != sizeof(thing)) return 10;

    /* A tag declared inside a block is a different type from the outer
       one with the same name. */
    {
        struct thing { double d; };
        struct thing inner = {1.5};
        if (sizeof inner == sizeof(struct thing *)) { /* nothing */ }
        if (inner.d != 1.5) return 11;
        if (sizeof inner != sizeof(double)) return 12;
    }
    if (sizeof(struct thing) != sizeof(int)) return 13;

    /* A static local keeps its value across calls; an automatic one does
       not exist between them. */
    if (counter() != 1) return 14;
    if (counter() != 2) return 15;
    if (counter() != 3) return 16;

    if (reentrant(4) != 4 + 3 + 2 + 1 + 0) return 17;

    /* A for-loop declaration is scoped to the loop. */
    int i = 100;
    for (int i = 0; i < 3; i++) {
        int i2 = i;
        (void)i2;
    }
    if (i != 100) return 18;

    /* An enumerator is an ordinary identifier and can be shadowed. */
    enum { RED = 1, GREEN, BLUE } col = BLUE;
    if (col != 3) return 19;
    {
        int RED = 99;
        if (RED != 99) return 20;
    }
    if (RED != 1) return 21;

    /* A label lives in its own namespace and is function-scoped. */
    int hops = 0;
    goto RED_label;
RED_label:
    hops++;
    if (hops == 1) goto RED_label;
    if (hops != 2) return 22;

    printf("Scoping OK\n");
    return 0;
}
