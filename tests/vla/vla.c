/* Variable length arrays: a size decided at run time, as a local and as a
   parameter, in one dimension and in two. */

#include <stdio.h>

static int sum1(int n, int a[n]) {
    int s = 0;
    for (int i = 0; i < n; i++) s += a[i];
    return s;
}

/* The inner dimension is a parameter too, so the subscript arithmetic is
   computed rather than folded. */
static int sum2(int rows, int cols, int m[rows][cols]) {
    int s = 0;
    for (int i = 0; i < rows; i++)
        for (int j = 0; j < cols; j++) s += m[i][j];
    return s;
}

/* [*] in a prototype says "variably modified" without naming the size; the
   definition that follows names it. */
static int firstn(int n, int a[*]);
static int firstn(int n, int a[n]) {
    int s = 0;
    for (int i = 0; i < n; i++) s += a[i];
    return s;
}

static int nested(int n) {
    int total = 0;
    for (int k = 1; k <= n; k++) {
        int inner[k];                 /* a new object each time round */
        for (int i = 0; i < k; i++) inner[i] = i + 1;
        for (int i = 0; i < k; i++) total += inner[i];
    }
    return total;
}

int main(void) {
    int n = 5;
    int v[n];
    for (int i = 0; i < n; i++) v[i] = i + 1;
    if (sum1(n, v) != 15) return 1;

    /* A fixed-size array is an argument to a VLA parameter like any other. */
    int fixed[2][3] = {{1, 2, 3}, {4, 5, 6}};
    if (sum2(2, 3, fixed) != 21) return 2;
    if (firstn(3, fixed[0]) != 6) return 3;

    int rows = 2, cols = 3;
    int grid[rows][cols];
    for (int i = 0; i < rows; i++)
        for (int j = 0; j < cols; j++) grid[i][j] = i * cols + j;
    if (sum2(rows, cols, grid) != 15) return 4;
    if (grid[1][2] != 5) return 5;

    /* A pointer into a VLA walks it the way it walks any array. */
    int *p = grid[1];
    if (p[0] != 3 || p[2] != 5) return 6;

    if (nested(4) != 1 + 3 + 6 + 10) return 7;

    /* A VLA in a block leaves with the block, and the next one may be a
       different size. */
    for (int k = 1; k <= 3; k++) {
        int scratch[k * 2];
        scratch[k * 2 - 1] = k;
        if (scratch[k * 2 - 1] != k) return 8;
    }

    printf("VLA OK\n");
    return 0;
}
