/* _Thread_local: one variable, one copy per thread.
 *
 * The storage class is C11's (§6.7.1), and it is not a placement hint. A
 * thread-local has no address of its own — it has an offset into a block
 * each thread owns a copy of — so reaching one is a sequence and not a
 * symbol reference, and a compiler that emits the symbol answers with the
 * template every copy is made from: a real address, holding the initial
 * value, belonging to no thread.
 *
 * The single-threaded half is here because it is what every host can check.
 * That the copies are actually separate is platform/windows_tls.c, which
 * needs threads to say it. */

#include <stdio.h>

_Thread_local int counter;
_Thread_local long long big = 7;
_Thread_local const char *name = "main";
_Thread_local int table[4] = {1, 2, 3, 4};
static _Thread_local int hidden = 11;

struct pair { int a; long long b; };
_Thread_local struct pair rec = {5, 6};

int readHidden(void) { return hidden; }

int main(void) {
    /* The initializer is the template's, and it is what the first read
     * sees. A zero-initialized one still reads zero rather than whatever
     * the section held. */
    if (counter != 0) return 1;
    if (big != 7) return 2;
    if (name == NULL || name[0] != 'm') return 3;
    if (table[0] != 1 || table[3] != 4) return 4;
    if (hidden != 11) return 5;
    if (rec.a != 5 || rec.b != 6) return 6;

    /* And it is an ordinary object otherwise: assignable, addressable,
     * indexable. */
    counter = 41;
    counter++;
    if (counter != 42) return 7;

    big += 35;
    if (big != 42) return 8;

    {
        int *p = &counter;
        *p += 1;
        if (counter != 43) return 9;
        if (p != &counter) return 10;
    }

    for (int i = 0; i < 4; i++) table[i] *= 10;
    if (table[0] != 10 || table[3] != 40) return 11;

    rec.a = 1; rec.b = 2;
    if (rec.a != 1 || rec.b != 2) return 12;

    /* A static one is reachable from a function in the same unit and
     * nowhere else. */
    hidden = 99;
    if (readHidden() != 99) return 13;

    printf("Thread-local OK\n");
    return 0;
}
