#include <stdio.h>

struct Anon {
    int id;
    union {
        int i;
        float f;
    };
    struct {
        int x;
        int y;
    };
};

int main() {
    struct Anon a;
    a.id = 1;
    a.i = 42;
    if (a.i != 42) return 1;

    a.f = 3.14f;
    if (a.f != 3.14f) return 2;

    a.x = 10;
    a.y = 20;
    if (a.x != 10) return 3;
    if (a.y != 20) return 4;

    printf("Anon OK\n");
    return 0;
}
