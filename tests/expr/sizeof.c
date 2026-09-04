/* sizeof(expr) is an integer constant expression, and vcc now folds it.
 *
 * §6.6p6 admits sizeof among the operators an integer constant expression is
 * built from, and does not distinguish its two forms. vcc folded only
 * sizeof(TypeName): the operand of the expression form had to be typed, and
 * the analyzer typed expressions only where it was already walking them. So
 * `_Static_assert(sizeof(x) == 4)` and, more to the point,
 * `int n[sizeof(arr) / sizeof(arr[0])]` were rejected as non-constant —
 * the array-length idiom every C program in the world writes.
 *
 * §6.5.3.4p2 is the exception this respects: where the operand's type is
 * variably modified the size is computed where the sizeof is, so it is not
 * a constant and the operand really is evaluated. sizeof_vla below is a
 * runtime check for that reason.
 */
#include <stdio.h>

struct S { int a; double b; char c; };

static int obj;
static struct S s;
static int arr[10];
static char *ptr;

_Static_assert(sizeof(obj) == sizeof(int), "an object");
_Static_assert(sizeof obj == sizeof(int), "the unparenthesized form");
_Static_assert(sizeof(s) == sizeof(struct S), "a struct object");
_Static_assert(sizeof(arr) == 10 * sizeof(int), "an array is not decayed");
_Static_assert(sizeof(ptr) == sizeof(char *), "a pointer");
_Static_assert(sizeof(arr[0]) == sizeof(int), "a subscript");
_Static_assert(sizeof(s.b) == sizeof(double), "a member");
_Static_assert(sizeof(obj + 1L) == sizeof(long), "after the usual conversions");
_Static_assert(sizeof(sizeof(obj)) == sizeof(size_t), "nested");
_Static_assert(sizeof(&obj) == sizeof(int *), "an address");
_Static_assert(sizeof(*&s) == sizeof(struct S), "through a dereference");

/* the idiom: an array length taken from another array's size */
static int copy[sizeof(arr) / sizeof(arr[0])];
_Static_assert(sizeof(copy) == sizeof(arr), "same length by construction");

/* and in a bit-field width and an enum value, the other constant contexts */
struct B { unsigned f : sizeof(int) > 2 ? 8 : 4; };
enum E { N = sizeof(arr) / sizeof(arr[0]) };
_Static_assert(N == 10, "enumerator from sizeof");

static int sizeof_vla(int n) {
    int vla[n];
    vla[0] = 0;
    return (int)(sizeof(vla) / sizeof(vla[0]));
}

int main(void) {
    if (sizeof(copy) != 10 * sizeof(int)) return 1;
    if (N != 10) return 2;
    if (sizeof_vla(6) != 6) return 3;
    if (sizeof_vla(1) != 1) return 4;

    /* a run-time read of the same sizes, so the fold and the emitted
     * code have to agree rather than only the fold being right */
    if (sizeof(arr) / sizeof(arr[0]) != 10) return 5;
    if (sizeof(s) != sizeof(struct S)) return 6;

    struct B b;
    b.f = 3;
    if (b.f != 3) return 7;

    printf("Sizeof const OK\n");
    return 0;
}
