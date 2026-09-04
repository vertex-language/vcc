#include <stdio.h>

enum Color {
    RED,
    GREEN,
    BLUE = 5,
    YELLOW
};

int main() {
    if (RED != 0) return 1;
    if (GREEN != 1) return 2;
    if (BLUE != 5) return 3;
    if (YELLOW != 6) return 4;

    printf("Enums OK\n");
    return 0;
}
