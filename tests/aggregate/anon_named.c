/* Anonymous members whose type has a name.
 *
 * §6.7.2.1p13 admits one shape: a struct or union defined right there, with
 * no tag and no member name. MSVC admits two more, and the Windows SDK
 * writes both — a tagged definition, and a typedef name — which is what
 * winuser.h's `MOUSEHOOKSTRUCT DUMMYSTRUCTNAME;` and objidl.h's
 * `struct _STGMEDIUM_UNION {…};` become once the macro expands to nothing.
 * A compiler that reads them as declaring nothing cannot open <windows.h>. */

#include <stddef.h>
#include <stdio.h>

typedef struct { int px; int py; } Point;

struct Named {
    int tag;
    struct Inner { int a; int b; };   /* a tagged definition */
    Point;                            /* a typedef name */
    union { int lo; char bytes[4]; }; /* and the standard shape */
};

/* Two zero-length arrays as the arms of one union, which is winioctl.h's
 * STORAGE_QUERY_DEPENDENT_VOLUME_RESPONSE: a union has no order, so a
 * flexible array member is admissible anywhere in one. */
struct Trailing {
    int count;
    union {
        int  small[];
        long wide[];
    };
};

int main(void) {
    struct Named n;
    n.tag = 1;
    n.a = 2;      /* promoted from the tagged struct */
    n.b = 3;
    n.px = 4;     /* promoted from the typedef */
    n.py = 5;
    n.lo = 0;
    n.bytes[0] = 6;

    if (n.tag != 1 || n.a != 2 || n.b != 3 || n.px != 4 || n.py != 5) return 1;
    if (n.bytes[0] != 6) return 2;
    if (n.lo != 6) return 3;   /* the same storage, read the other way */

    /* The promoted members have real offsets, and they are the inner
     * record's plus the anonymous member's. */
    if (offsetof(struct Named, a) != offsetof(struct Named, tag) + sizeof(int)) return 4;
    if (offsetof(struct Named, py) <= offsetof(struct Named, px)) return 5;
    if ((char *)&n.a - (char *)&n != (ptrdiff_t)offsetof(struct Named, a)) return 6;

    /* A flexible array member in a union adds nothing to the size. */
    if (sizeof(struct Trailing) != sizeof(int)) return 7;

    printf("Anonymous named members OK\n");
    return 0;
}
