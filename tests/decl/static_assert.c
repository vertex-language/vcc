/* _Static_assert, at every place a declaration may appear, over the kinds of
   constant expression §6.6 allows. A failing one is in tests/errors. */

#include <stdio.h>
#include <limits.h>

_Static_assert(sizeof(int) == 4, "int is four bytes on every target vcc has");
_Static_assert(CHAR_BIT == 8, "a byte is eight bits");
_Static_assert(sizeof(long) >= sizeof(int), "long is no narrower than int");
_Static_assert(1, "a constant that is not zero");

enum { WIDTH = 16, HEIGHT = 4 };
_Static_assert(WIDTH * HEIGHT == 64, "enumeration constants are constants");

struct frame {
    int a;
    _Static_assert(sizeof(int) <= sizeof(long), "inside a struct, too");
    int b;
};

_Static_assert(sizeof(struct frame) == 2 * sizeof(int), "and it costs nothing");

int main(void) {
    _Static_assert(sizeof(char) == 1, "in block scope");

    struct frame f = {1, 2};
    if (f.a + f.b != 3) return 1;

    /* sizeof of a variable is a constant expression too. */
    int arr[WIDTH];
    _Static_assert(sizeof(int[WIDTH]) == WIDTH * sizeof(int), "of a type");
    if (sizeof arr != WIDTH * sizeof(int)) return 2;

    printf("Static Assert OK\n");
    return 0;
}
