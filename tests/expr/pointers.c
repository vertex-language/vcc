/* Pointers: arithmetic on them, what a pointer to an array is as distinct
   from a pointer to its first element, and the conversions C defines both
   ways between pointers and integers. */

#include <stdio.h>
#include <stdint.h>

static void swap(int *a, int *b) {
    int t = *a;
    *a = *b;
    *b = t;
}

int main(void) {
    /* Indirection through a parameter reaches the caller's object. */
    int x = 10, y = 20;
    swap(&x, &y);
    if (x != 20 || y != 10) return 1;

    /* §6.5.6: arithmetic on a pointer is in units of the pointed-to type. */
    int arr[5] = {1, 2, 3, 4, 5};
    int *p = arr;
    if (*p != 1) return 2;
    p++;
    if (*p != 2) return 3;
    if (*(p + 2) != 4) return 4;
    if (p[-1] != 1) return 5;
    if (&arr[4] - &arr[0] != 4) return 6;
    if (arr + 5 != &arr[5]) return 7;        /* one past the end is valid */

    /* Indexing is commutative because it is defined as addition. */
    if (2[arr] != arr[2]) return 8;

    /* A pointer to an array is not a pointer to its first element: the two
       have different types, so + 1 moves different distances. */
    int grid[2][3] = {{1, 2, 3}, {4, 5, 6}};
    int (*row)[3] = grid;
    if ((*row)[0] != 1) return 9;
    row++;
    if ((*row)[0] != 4) return 10;
    if (row - (int (*)[3])grid != 1) return 11;
    if (row[0][2] != 6) return 12;

    /* The inner pointer walks elements, the outer walks rows. */
    int *q = row[0];
    q += 2;
    if (*q != 6) return 13;
    if (sizeof *row != 3 * sizeof(int)) return 14;
    if (sizeof *q != sizeof(int)) return 15;

    /* §6.3.2.3: void * converts to and from any object pointer with no
       cast, and a round trip through uintptr_t comes back usable. */
    void *v = &x;
    int *back = v;
    if (*back != 20) return 16;
    uintptr_t bits = (uintptr_t)&x;
    if (*(int *)bits != 20) return 17;
    if ((void *)bits != v) return 18;

    /* A null pointer constant converts to any pointer type and compares
       equal to a null pointer of that type. */
    int *null = 0;
    if (null != NULL) return 19;
    if (null) return 20;
    if ((void *)0 != null) return 21;

    printf("Pointers OK\n");
    return 0;
}
