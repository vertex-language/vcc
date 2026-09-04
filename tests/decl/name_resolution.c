#include <stdio.h>

/* Every place an identifier appears that is *not* an ordinary-namespace
   lookup: a member after . or ->, a .field designator, a label, a _Generic
   association, an enumeration constant used as an array designator. Each
   one is a false positive waiting to happen for a checker that resolves
   every Ident it sees, so each one is here. */

struct pt { int x, y; };
union u { int i; float f; };
enum e { A, B };

static int g(int n) { return n; }
static struct pt mk(void) { struct pt p = {.x = 1, .y = 2}; return p; }

int main(void) {
    struct pt p = mk();
    union u v = {.i = 3};
    if (p.x + p.y + v.i != 6) return 1;
    struct pt *q = &p;
    if (q->x != 1) return 2;

    enum e k = B;
    if (k != 1) return 3;

    int arr[] = {[A] = 10, [B] = 20};
    if (arr[1] != 20) return 4;

    int n = 3;
    if (sizeof(int[n]) != 3 * sizeof(int)) return 5;
    int vla[n];
    vla[0] = g(n);
    if (vla[0] != 3) return 6;

    const char *s = _Generic(p.x, int: "int", default: "other");
    if (s[0] != 'i') return 7;

    goto done;
done:
    ;
    struct pt nested = { .x = g(1), .y = (int)sizeof(struct pt) };
    if (nested.x != 1) return 8;

    /* A name declared later in the same block is not in scope earlier,
       but one declared here is visible in its own initializer's sizeof. */
    struct pt *self = (struct pt *)0;
    if (sizeof *self != sizeof(struct pt)) return 9;

    printf("Name Resolution OK\n");
    return 0;
}
