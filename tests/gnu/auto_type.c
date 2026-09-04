/* __auto_type and __builtin_types_compatible_p, the two GNU extensions a
 * header reaches for when it has to name a type it cannot write.
 *
 * __auto_type is the initializer's type after the conversions of §6.3.2.1
 * and with the top-level qualifiers dropped. That is not typeof: typeof(arr)
 * is an array and typeof(ci) is const, and neither is what a macro assigning
 * into a temporary wants. The reason it exists is the max/min shape below,
 * where the argument must be evaluated once and its type is not known.
 *
 * vcc deduces for a plain identifier only. `__auto_type *p = &v` would have
 * to solve the declarator's derivation against the initializer's type — p is
 * int*, not int** — and vcc reports that instead of guessing.
 *
 * __builtin_types_compatible_p compares two type names for §6.2.7p1
 * compatibility with top-level qualifiers dropped, and folds to a constant,
 * so a static assertion or an array length may be written with it.
 *
 * One answer here is implementation-defined and vcc's differs from clang's:
 * an enum's compatible integer type is int in vcc (§6.7.2.2p4) and unsigned
 * int in clang for these enumerators, so `(enum E, int)` is not asserted.
 * enum-against-enum is the same in both and is.
 */
#include <stdio.h>

typedef int myint;
enum E { A };
enum F { B };

int arr[4];
static int fn(int x) { return x + 1; }

#define max(a, b) ({ __auto_type _a = (a); __auto_type _b = (b); _a > _b ? _a : _b; })

static int calls = 0;
static int bump(int v) { calls++; return v; }

/* folds to a constant: usable where the language demands one */
_Static_assert(__builtin_types_compatible_p(int, myint), "typedef is transparent");
_Static_assert(!__builtin_types_compatible_p(int, long), "int and long differ");
_Static_assert(__builtin_types_compatible_p(const int, int), "top-level quals drop");
_Static_assert(!__builtin_types_compatible_p(const int *, int *), "inner quals do not");
_Static_assert(__builtin_types_compatible_p(int[], int[5]), "§6.2.7p1: one incomplete");
_Static_assert(!__builtin_types_compatible_p(int[3], int[5]), "two known, different");
_Static_assert(!__builtin_types_compatible_p(char, signed char), "three distinct chars");
_Static_assert(__builtin_types_compatible_p(enum E, enum E), "a tag is itself");
_Static_assert(!__builtin_types_compatible_p(enum E, enum F), "two tags are not");

static int width[__builtin_types_compatible_p(char *, char *) ? 4 : 1];

int main(void) {
    /* the deduced type is the initializer's, after decay */
    __auto_type a = 42;      /* int    */
    __auto_type b = 3.5;     /* double */
    __auto_type c = "hi";    /* char * */
    __auto_type d = arr;     /* int *, not int[4] */
    __auto_type e = fn;      /* int (*)(int), not the function */
    if (sizeof a != sizeof(int)) return 1;
    if (sizeof b != sizeof(double)) return 2;
    if (sizeof c != sizeof(char *)) return 3;
    if (sizeof d != sizeof(int *)) return 4;
    if (sizeof e != sizeof(int (*)(int))) return 5;

    /* and the values survive the deduction */
    if (a != 42) return 6;
    if (b != 3.5) return 7;
    if (c[0] != 'h' || c[1] != 'i' || c[2] != 0) return 8;
    arr[0] = 11;
    if (d[0] != 11) return 9;
    if (e(4) != 5) return 10;

    /* top-level qualifiers are dropped, so this one is assignable */
    const int ci = 7;
    __auto_type g = ci;
    g = 8;
    if (g != 8) return 11;

    /* several declarators deduce the same type and are allowed to */
    __auto_type p = 1, q = 2;
    if (p + q != 3) return 12;

    /* the shape the extension exists for: one evaluation per argument */
    if (max(3, 7) != 7) return 13;
    if (max(bump(5), bump(2)) != 5) return 14;
    if (calls != 2) return 15;
    if (max(1.5, 2.5) != 2.5) return 16;

    if (sizeof(width) != 4 * sizeof(int)) return 17;

    printf("Auto type OK\n");
    return 0;
}
