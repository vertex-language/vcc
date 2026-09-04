/* A thread-local defined here and read from the other unit. An import
 * carries no domain — it has no storage in this object to place — so what
 * marks it thread-local is the model attribute, and a compiler that looks
 * only at the domain reaches the definition's template instead of the
 * calling thread's copy. */
#include <stdio.h>

_Thread_local int shared = 3;
_Thread_local long long wide = 100;

void *addrFromOther(void);
int readShared(void);
void writeShared(int);
long long readWide(void);

int main(void) {
    /* Both units must agree on the address, and it must not be the
     * template: a template address is an image address, and this one is
     * whatever the loader handed this thread. */
    if (&shared != addrFromOther()) return 1;

    if (readShared() != 3) return 2;
    if (readWide() != 100) return 3;

    writeShared(40);
    if (shared != 40) return 4;
    if (readShared() != 40) return 5;

    shared = 7;
    if (readShared() != 7) return 6;

    printf("Cross-unit thread-local OK\n");
    return 0;
}
