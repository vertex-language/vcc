#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* Calling into libc with a callback, and taking the address of a static
   function. qsort reaches back into this unit through a pointer it was
   handed, which is the one direction the linker cannot see. */

struct rec {
    int key;
    const char *name;
};

static int cmp_int(const void *a, const void *b) {
    int x = *(const int *)a, y = *(const int *)b;
    return (x > y) - (x < y);
}

static int cmp_rec(const void *a, const void *b) {
    const struct rec *p = a, *q = b;
    if (p->key != q->key) return p->key < q->key ? -1 : 1;
    return strcmp(p->name, q->name);
}

/* A comparator chosen at run time, so the call really is indirect. */
typedef int (*cmp_fn)(const void *, const void *);

static cmp_fn pick(int records) { return records ? cmp_rec : cmp_int; }

int main(void) {
    int v[] = {5, 3, 9, 1, 9, -4, 0, 7};
    size_t n = sizeof v / sizeof v[0];
    qsort(v, n, sizeof v[0], pick(0));
    for (size_t i = 1; i < n; i++)
        if (v[i - 1] > v[i]) return 1;
    if (v[0] != -4 || v[n - 1] != 9) return 2;

    /* bsearch over the same comparator. */
    int want = 7;
    int *hit = bsearch(&want, v, n, sizeof v[0], cmp_int);
    if (!hit || *hit != 7) return 3;
    want = 6;
    if (bsearch(&want, v, n, sizeof v[0], cmp_int) != NULL) return 4;

    struct rec r[] = {
        {3, "c"}, {1, "b"}, {1, "a"}, {2, "z"},
    };
    qsort(r, 4, sizeof r[0], pick(1));
    if (r[0].key != 1 || strcmp(r[0].name, "a") != 0) return 5;
    if (r[1].key != 1 || strcmp(r[1].name, "b") != 0) return 6;
    if (r[2].key != 2) return 7;
    if (r[3].key != 3) return 8;

    /* An array of function pointers, indexed at run time. */
    cmp_fn table[2] = {cmp_int, cmp_rec};
    int one = 1, two = 2;
    if (table[0](&one, &two) >= 0) return 9;
    if (table[0](&two, &one) <= 0) return 10;
    if (table[0](&one, &one) != 0) return 11;

    printf("Qsort OK\n");
    return 0;
}
