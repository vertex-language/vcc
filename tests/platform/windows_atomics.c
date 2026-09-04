/* The Interlocked family: the Windows platform's atomics.
 *
 * <windows.h> writes each of these as a macro onto an underscore-prefixed
 * name, declares a prototype, and hands it to the compiler with `#pragma
 * intrinsic`. Nothing defines them, so a compiler that lowers one as a call
 * compiles a program that does not link — and a threaded Windows program is
 * written in them.
 *
 * Which of the two values each answers with is the part worth a test:
 * Increment and Decrement give the value they left behind, everything else
 * gives the one they found, and CompareExchange gives what it read whether
 * or not it swapped. */

#include <stdio.h>

#ifdef _WIN32
#include <windows.h>

static volatile LONG shared;

static DWORD WINAPI bump(LPVOID arg) {
    LONG n = *(LONG *)arg;
    for (LONG i = 0; i < n; i++) InterlockedIncrement(&shared);
    return 0;
}

int main(void) {
    LONG v = 10;

    if (InterlockedIncrement(&v) != 11) return 1;   /* the new value */
    if (v != 11) return 2;
    if (InterlockedDecrement(&v) != 10) return 3;

    if (InterlockedExchange(&v, 42) != 10) return 4; /* the old value */
    if (v != 42) return 5;
    if (InterlockedExchangeAdd(&v, 8) != 42) return 6;
    if (v != 50) return 7;
    if (InterlockedAnd(&v, 0x0F) != 50) return 8;
    if (v != (50 & 0x0F)) return 9;
    if (InterlockedOr(&v, 0x30) != 2) return 10;
    if (v != 0x32) return 11;
    if (InterlockedXor(&v, 0xFF) != 0x32) return 12;
    if (v != (0x32 ^ 0xFF)) return 13;

    /* Compare-and-exchange answers with what it read, swapped or not. */
    v = 7;
    if (InterlockedCompareExchange(&v, 99, 7) != 7) return 14;
    if (v != 99) return 15;
    if (InterlockedCompareExchange(&v, 5, 7) != 99) return 16;
    if (v != 99) return 17;

    /* Sixty-four bits, and sixteen. */
    {
        LONG64 w = 1LL << 40;
        if (InterlockedIncrement64(&w) != (1LL << 40) + 1) return 18;
        if (InterlockedExchangeAdd64(&w, 1LL << 40) != (1LL << 40) + 1) return 19;
        if (w != (1LL << 41) + 1) return 20;
        if (InterlockedCompareExchange64(&w, 0, (1LL << 41) + 1) != (1LL << 41) + 1) return 21;
        if (w != 0) return 22;

        SHORT s = 5;
        if (InterlockedIncrement16(&s) != 6) return 23;
        if (InterlockedDecrement16(&s) != 5) return 24;
    }

    /* And pointers, which are their own width. */
    {
        void *slot = NULL;
        int target = 0;
        if (InterlockedExchangePointer(&slot, &target) != NULL) return 25;
        if (slot != &target) return 26;
        if (InterlockedCompareExchangePointer(&slot, NULL, &target) != &target) return 27;
        if (slot != NULL) return 28;
    }

    /* Indivisible under contention, which is the point of all of it. */
    {
        LONG n = 2000;
        HANDLE h[4];
        int i;
        for (i = 0; i < 4; i++) {
            h[i] = CreateThread(NULL, 0, bump, &n, 0, NULL);
            if (h[i] == NULL) return 29;
        }
        if (WaitForMultipleObjects(4, h, TRUE, 30000) == WAIT_FAILED) return 30;
        for (i = 0; i < 4; i++) CloseHandle(h[i]);
        if (shared != 4 * n) return 31;
    }

    printf("Windows atomics OK\n");
    return 0;
}

#else

int main(void) {
    printf("Windows atomics OK (not this host)\n");
    return 0;
}

#endif
