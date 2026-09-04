/* A function that returns a pointer to a function.
 *
 * `void (*pick(int a))(void)` derives a function twice — pick takes an int,
 * and what it returns is called with none — and only the inner list is
 * pick's own. A compiler that reads the outer one binds the body's
 * parameters from `(void)`, which names nothing, and every use of a
 * parameter in the body becomes an unbound name.
 *
 * The prototype is what makes it show: without one the type carries the
 * definition's own parameter names and nothing has to go looking. SQLite's
 * sqlite3OsDlSym is this shape, prototype and all. */

#include <stdio.h>

static int doubled(int x) { return x * 2; }
static int tripled(int x) { return x * 3; }

/* Declared first, with the parameters unnamed. */
int (*pick(int, int))(int);

int (*pick(int which, int bias))(int) {
    if (which + bias > 0) return doubled;
    return tripled;
}

/* And returning a pointer to an array, the other doubled derivation. */
static int grid[2][3] = {{1, 2, 3}, {4, 5, 6}};

int (*row(int i, int unused))[3] {
    (void)unused;
    return &grid[i];
}

int main(void) {
    int (*f)(int) = pick(1, 0);
    if (f == 0) return 1;
    if (f(21) != 42) return 2;

    f = pick(-1, 0);
    if (f(14) != 42) return 3;

    int (*r)[3] = row(1, 0);
    if ((*r)[0] != 4 || (*r)[2] != 6) return 4;

    printf("Returning pointers OK\n");
    return 0;
}
