/* offsetof, and the address constant C used before it had offsetof.
 *
 * MSVC's <stddef.h> defines offsetof as ((size_t)&(((s *)0)->m)) whenever
 * _MSC_VER is set, the Windows SDK's FIELD_OFFSET falls back to the same
 * shape, and a great deal of C written before __builtin_offsetof still says
 * it outright. It has to fold where a constant is required, or a header that
 * asserts a layout with it does not compile. */

#include <stddef.h>
#include <stdio.h>

struct Inner { char pad; int deep; };

struct S {
    char a;
    int  b;
    struct { short x; long long y; };   /* anonymous: its members are S's */
    struct Inner in;
    int  arr[4];
};

#define ADDR_OFFSETOF(s, m) ((size_t)&(((s *)0)->m))

/* The two spellings agree, member for member. */
_Static_assert(ADDR_OFFSETOF(struct S, a) == offsetof(struct S, a), "a");
_Static_assert(ADDR_OFFSETOF(struct S, b) == offsetof(struct S, b), "b");
_Static_assert(ADDR_OFFSETOF(struct S, x) == offsetof(struct S, x), "anonymous");
_Static_assert(ADDR_OFFSETOF(struct S, in) == offsetof(struct S, in), "nested record");
_Static_assert(ADDR_OFFSETOF(struct S, in.deep) == offsetof(struct S, in.deep), "path");
_Static_assert(ADDR_OFFSETOF(struct S, arr[2]) == offsetof(struct S, arr[2]), "subscript");

/* The first member is at zero, whichever way it is asked. */
_Static_assert(offsetof(struct S, a) == 0, "first member");

/* The shape a header asserts a layout with: an array whose length is the
 * offset, at file scope, where a non-constant length is not a VLA but an
 * error. This is winnt.h's C_ASSERT. */
typedef char assert_a_is_first[(ADDR_OFFSETOF(struct S, a) == 0) ? 1 : -1];
typedef char assert_arr_follows_in[(offsetof(struct S, arr) > offsetof(struct S, in)) ? 1 : -1];

int main(void) {
    struct S s;
    char *base = (char *)&s;

    /* The folded offsets are the real ones. */
    if ((char *)&s.a - base != (ptrdiff_t)offsetof(struct S, a)) return 1;
    if ((char *)&s.b - base != (ptrdiff_t)offsetof(struct S, b)) return 2;
    if ((char *)&s.x - base != (ptrdiff_t)offsetof(struct S, x)) return 3;
    if ((char *)&s.y - base != (ptrdiff_t)offsetof(struct S, y)) return 4;
    if ((char *)&s.in - base != (ptrdiff_t)offsetof(struct S, in)) return 5;
    if ((char *)&s.in.deep - base != (ptrdiff_t)offsetof(struct S, in.deep)) return 6;
    if ((char *)&s.arr[2] - base != (ptrdiff_t)offsetof(struct S, arr[2])) return 7;

    if (sizeof(assert_a_is_first) != 1) return 8;
    if (sizeof(assert_arr_follows_in) != 1) return 9;

    printf("Offsets OK\n");
    return 0;
}
