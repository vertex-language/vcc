#include <stdio.h>

#define type_name(x) _Generic((x), \
    int: 1, \
    float: 2, \
    default: 0)

int main() {
    int i = 0;
    float f = 0.0f;
    double d = 0.0;
    if (type_name(i) != 1) return 1;
    if (type_name(f) != 2) return 2;
    if (type_name(d) != 0) return 3;

    printf("_Generic OK\n");
    return 0;
}
