/* An array whose size the initializer decides (§6.7.9p22): from the largest
   index a designator reaches, and from the length of a string literal, which
   includes the terminator it is not written with. */

#include <stdio.h>
#include <string.h>

char greeting[] = "Hello";               /* file scope, six bytes */
static const char *const names[] = {"a", "bb", "ccc"};

int main(void) {
    int arr[] = {1, 2, 3};
    if (sizeof arr / sizeof arr[0] != 3) return 1;
    if (arr[0] != 1 || arr[1] != 2 || arr[2] != 3) return 2;

    /* A designator past the last positional element extends the array, and
       every element it skipped is zero. */
    int sparse[] = {[4] = 9, [1] = 2};
    if (sizeof sparse / sizeof sparse[0] != 5) return 3;
    if (sparse[1] != 2 || sparse[4] != 9) return 4;
    if (sparse[0] != 0 || sparse[2] != 0 || sparse[3] != 0) return 5;

    /* A string literal initializer sizes the array, terminator included. */
    char here[] = "World";
    if (sizeof here != 6) return 6;
    if (sizeof greeting != 6) return 7;
    if (strcmp(greeting, "Hello") != 0) return 8;
    if (strcmp(here, "World") != 0) return 9;

    /* §6.7.9p14 lets the terminator be dropped when the size is written and
       leaves exactly no room for it. */
    char exact[5] = "World";
    if (exact[4] != 'd') return 10;
    if (sizeof exact != 5) return 11;

    /* An array of pointers is sized by how many initializers there are, not
       by what they point at. */
    if (sizeof names / sizeof names[0] != 3) return 12;
    if (strcmp(names[2], "ccc") != 0) return 13;

    /* Two dimensions: the outer one is inferred, the inner is written. */
    int grid[][2] = {{1, 2}, {3, 4}, {5, 6}};
    if (sizeof grid / sizeof grid[0] != 3) return 14;
    if (grid[2][1] != 6) return 15;

    printf("Inference OK\n");
    return 0;
}
