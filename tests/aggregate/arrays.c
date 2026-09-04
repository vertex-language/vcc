/* Arrays: several dimensions, the decay to a pointer that §6.3.2.1p3 makes
   almost everywhere, and the three places it does not happen — sizeof,
   _Alignof, and the operand of &. */

#include <stdio.h>

static int total(const int *p, int n) {
    int s = 0;
    for (int i = 0; i < n; i++) s += p[i];
    return s;
}

/* A two-dimensional parameter keeps its inner dimension: without it the
   subscript arithmetic has nothing to multiply by. */
static int total2(int m[][3], int rows) {
    int s = 0;
    for (int i = 0; i < rows; i++)
        for (int j = 0; j < 3; j++) s += m[i][j];
    return s;
}

int main(void) {
    int mat[2][3] = {{1, 2, 3}, {4, 5, 6}};

    if (mat[0][0] != 1) return 1;
    if (mat[1][2] != 6) return 2;

    int sum = 0;
    for (int i = 0; i < 2; i++)
        for (int j = 0; j < 3; j++) sum += mat[i][j];
    if (sum != 21) return 3;
    if (total2(mat, 2) != 21) return 4;

    /* Sizes are of the whole object, not of the pointer it decays to. */
    if (sizeof mat != 6 * sizeof(int)) return 5;
    if (sizeof mat[0] != 3 * sizeof(int)) return 6;
    if (sizeof mat / sizeof mat[0] != 2) return 7;
    if (_Alignof(int[7]) != _Alignof(int)) return 8;

    /* Decay: the array becomes a pointer to its first element. */
    int flat[4] = {10, 20, 30, 40};
    if (total(flat, 4) != 100) return 9;
    int *p = flat;
    if (*p != 10 || p[3] != 40) return 10;
    if (flat + 1 != &flat[1]) return 11;

    /* &array is a pointer to the array, which is a different type from a
       pointer to its first element even though it is the same address. */
    int (*whole)[4] = &flat;
    if ((void *)whole != (void *)flat) return 12;
    if (sizeof *whole != 4 * sizeof(int)) return 13;
    if ((*whole)[2] != 30) return 14;

    /* An array is not assignable, but a struct holding one is copied whole. */
    struct box { int v[3]; };
    struct box a = {{1, 2, 3}}, b;
    b = a;
    b.v[0] = 99;
    if (a.v[0] != 1 || b.v[0] != 99 || b.v[2] != 3) return 15;

    /* Zero and negative subscripts are just addition. */
    int *mid = flat + 2;
    if (mid[-2] != 10 || mid[0] != 30) return 16;

    printf("Arrays OK\n");
    return 0;
}
