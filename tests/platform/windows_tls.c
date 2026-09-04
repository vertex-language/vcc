/* Thread-local storage, actually threaded.
 *
 * decl/thread_local.c checks the storage class in one thread, which is all
 * a portable program can do; this is the half that says the copies are
 * separate. It needs threads, so it needs a platform.
 *
 * The Windows model is PE's static TLS. A thread-local's address is found by
 * reading the TEB's ThreadLocalStoragePointer, indexing it by the image's
 * _tls_index, and adding the variable's offset within the block — four
 * instructions, and the same ones clang emits. Getting any of them wrong
 * does not fail to link: it reads the template, which holds the initial
 * value and belongs to no thread, so every thread appears to share one
 * variable and the program is merely wrong. */

#include <stdio.h>

#ifdef _WIN32
#include <windows.h>

_Thread_local int counter;
_Thread_local long long tag = -1;
_Thread_local char label[32];

/* MSVC's spelling of the same storage class, which reaches the compiler as
 * a __declspec rather than a keyword. */
__declspec(thread) int msSpelling = 5;

static volatile LONG failures;
static void fail(void) { InterlockedIncrement(&failures); }

static DWORD WINAPI worker(LPVOID arg) {
    int id = (int)(INT_PTR)arg;

    /* Each thread starts from the template, not from whatever the last
     * thread left behind. */
    if (counter != 0) fail();
    if (tag != -1) fail();
    if (label[0] != 0) fail();
    if (msSpelling != 5) fail();

    tag = id;
    msSpelling = id + 100;
    for (int i = 0; i < 1000; i++) counter++;
    for (int i = 0; label[i] = "thread"[i], label[i]; i++) {
    }
    label[6] = (char)('0' + id);

    Sleep(5);  /* let the others interleave */

    /* And nobody else's writes reached this copy. */
    if (counter != 1000) fail();
    if (tag != id) fail();
    if (msSpelling != id + 100) fail();
    if (label[6] != (char)('0' + id)) fail();
    return 0;
}

int main(void) {
    counter = 12345;   /* the main thread's own copies */
    tag = 999;
    msSpelling = 777;

    HANDLE h[8];
    int i;
    for (i = 0; i < 8; i++) {
        h[i] = CreateThread(NULL, 0, worker, (LPVOID)(INT_PTR)i, 0, NULL);
        if (h[i] == NULL) return 1;
    }
    if (WaitForMultipleObjects(8, h, TRUE, 30000) == WAIT_FAILED) return 2;
    for (i = 0; i < 8; i++) CloseHandle(h[i]);

    if (failures != 0) {
        printf("%ld thread-local failures\n", (long)failures);
        return 3;
    }

    /* Untouched by any of them. */
    if (counter != 12345) return 4;
    if (tag != 999) return 5;
    if (msSpelling != 777) return 6;

    printf("Windows TLS OK (8 threads)\n");
    return 0;
}

#else

int main(void) {
    printf("Windows TLS OK (not this host)\n");
    return 0;
}

#endif
