#include <stdio.h>
#include <stddef.h>
#include <string.h>

int main() {
    const char *s = "Hello, " "World!";
    if (strcmp(s, "Hello, World!") != 0) return 1;

    // Wide strings. The element type is wchar_t, which is four bytes on
    // Linux and two on Windows — L"Wide" has the same five elements either
    // way, and the width is the platform's answer rather than the test's.
    const wchar_t *ws = L"Wide";
    if (ws[0] != 'W' || ws[1] != 'i' || ws[2] != 'd' || ws[3] != 'e' || ws[4] != 0) return 2;

    printf("Strings OK\n");
    return 0;
}
