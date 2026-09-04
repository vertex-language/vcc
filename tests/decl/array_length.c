/* An array whose length is its initializer's, and what that makes constant.
 *
 * §6.7.9p22 gives an incomplete array the length its initializer supplies,
 * whatever its storage duration. The consequence is the part worth testing:
 * once the declaration is complete, sizeof of it is an integer constant
 * expression, so an array sized from it is an ordinary array and not a VLA.
 * SQLite writes exactly that over a block-scope static. */

#include <stdio.h>

#define ArraySize(X) ((int)(sizeof(X) / sizeof(X[0])))

int file_scope[] = {1, 2, 3, 4};
static int file_static[] = {1, 2, 3};
char greeting[] = "hello";

_Static_assert(ArraySize(file_scope) == 4, "file scope");
_Static_assert(ArraySize(file_static) == 3, "file-scope static");
_Static_assert(sizeof greeting == 6, "the terminator counts");

/* A designator carries the count past where the list would leave it. */
int sparse[] = {[7] = 1, 2};
_Static_assert(ArraySize(sparse) == 9, "designated");

int main(void) {
    int automatic[] = {1, 2, 3, 4, 5};
    static int block_static[] = {1, 2, 3, 4, 5, 6};
    static const struct { const char *name; int n; } table[] = {
        {"a", 1}, {"b", 2}, {"c", 3},
    };

    if (ArraySize(automatic) != 5) return 1;
    if (ArraySize(block_static) != 6) return 2;
    if (ArraySize(table) != 3) return 3;

    /* Not a VLA: the bound is constant, so these are ordinary arrays whose
     * own size is constant in turn. */
    int derived[ArraySize(table)];
    _Static_assert(sizeof derived == 3 * sizeof(int), "derived is not a VLA");

    for (int i = 0; i < ArraySize(table); i++) derived[i] = table[i].n;
    if (derived[0] != 1 || derived[2] != 3) return 4;

    /* A compound literal completes the same way. */
    if (ArraySize(((int[]){9, 8, 7})) != 3) return 5;

    printf("Array length OK\n");
    return 0;
}
