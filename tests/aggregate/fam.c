#include <stdio.h>
#include <stdlib.h>

struct Data {
    int len;
    int arr[];
};

int main() {
    struct Data *d = malloc(sizeof(struct Data) + 5 * sizeof(int));
    if (!d) return 1;
    d->len = 5;
    for (int i = 0; i < 5; i++) {
        d->arr[i] = i * 2;
    }

    if (d->arr[3] != 6) return 2;
    if (d->arr[4] != 8) return 3;

    free(d);
    printf("FAM OK\n");
    return 0;
}
