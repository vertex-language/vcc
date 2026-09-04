#include <stdio.h>
#include <string.h>

/* const, volatile and restrict: what they change about the object, what
   they change about a pointer, and the fact that a volatile object is
   read every time the abstract machine says so. */

static volatile int ticks = 0;
static const int frozen = 42;
static const char *const message = "steady";

static void tick(void) { ticks++; }

/* restrict promises the two pointers do not alias, which is a promise the
   caller keeps rather than something the callee checks. */
static void copy_words(int *restrict dst, const int *restrict src, int n) {
    for (int i = 0; i < n; i++) dst[i] = src[i];
}

static int sum_volatile(volatile const int *p, int n) {
    int t = 0;
    for (int i = 0; i < n; i++) t += p[i];
    return t;
}

int main(void) {
    if (frozen != 42) return 1;
    if (strcmp(message, "steady") != 0) return 2;

    /* Every read of a volatile object is a real read, so a loop that
       reads it three times sees three increments. */
    ticks = 0;
    tick(); tick(); tick();
    if (ticks != 3) return 3;

    volatile int v = 0;
    v = v + 1;
    v = v + 1;
    if (v != 2) return 4;

    /* Qualifiers on a pointed-to type are part of the pointer's type but
       not of the value it holds. */
    int x = 7;
    const int *cp = &x;
    if (*cp != 7) return 5;
    x = 8;
    if (*cp != 8) return 6;

    /* A const pointer to non-const data: the pointer cannot move, the
       data can change. */
    int y = 1;
    int *const py = &y;
    *py = 2;
    if (y != 2) return 7;

    /* Casting away a qualifier is allowed; modifying an object actually
       defined const is not, so only the non-const object is written. */
    int z = 3;
    const int *cz = &z;
    *(int *)cz = 4;
    if (z != 4) return 8;

    int src[4] = {1, 2, 3, 4};
    int dst[4] = {0};
    copy_words(dst, src, 4);
    if (memcmp(dst, src, sizeof src) != 0) return 9;

    volatile const int table[3] = {10, 20, 30};
    if (sum_volatile(table, 3) != 60) return 10;

    /* Qualifiers do not change size or alignment. */
    if (sizeof(const volatile int) != sizeof(int)) return 11;
    if (_Alignof(const int) != _Alignof(int)) return 12;

    /* An array of const elements: the array itself is not const-qualified
       for the purposes of the type, but the elements are. */
    const int arr[3] = {1, 2, 3};
    const int *ap = arr;
    if (ap[2] != 3) return 13;

    /* A struct with a const member cannot be assigned as a whole; a copy
       through initialization is how one is built. */
    struct cs { const int k; int v; };
    struct cs a = {1, 2};
    struct cs b = a;
    if (b.k != 1 || b.v != 2) return 14;

    printf("Qualifiers OK\n");
    return 0;
}
