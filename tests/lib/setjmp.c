#include <stdio.h>
#include <setjmp.h>

jmp_buf buf;

void foo() {
    longjmp(buf, 42);
}

int main() {
    int val = setjmp(buf);
    if (val == 0) {
        foo();
        return 1;
    }
    
    if (val != 42) return 2;

    printf("Setjmp OK\n");
    return 0;
}
