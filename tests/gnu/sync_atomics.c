/* gcc's __sync_* atomics: the family that predates <stdatomic.h> and
 * predates __atomic_*, and the one the C written before either uses. vcc
 * says __GNUC__, so this is the family that C reaches for — SQLite's memory
 * barrier is __sync_synchronize() for any compiler that does.
 *
 * Which of the two values each answers with is what a test is for:
 * fetch_and_OP gives what it found, OP_and_fetch what it left behind. */

#include <stdio.h>
int main(void) {
    int v = 10;
    __sync_synchronize();

    if (__sync_fetch_and_add(&v, 5) != 10) return 1;   /* old */
    if (v != 15) return 2;
    if (__sync_add_and_fetch(&v, 5) != 20) return 3;   /* new */
    if (__sync_fetch_and_sub(&v, 4) != 20) return 4;
    if (__sync_sub_and_fetch(&v, 6) != 10) return 5;
    if (__sync_fetch_and_or(&v, 0x21) != 10) return 6;
    if (v != (10 | 0x21)) return 7;
    if (__sync_and_and_fetch(&v, 0x0F) != ((10 | 0x21) & 0x0F)) return 8;
    if (__sync_fetch_and_xor(&v, 0xFF) != ((10 | 0x21) & 0x0F)) return 9;
    if (__sync_xor_and_fetch(&v, 0xFF) != ((10 | 0x21) & 0x0F)) return 10;

    v = 7;
    if (__sync_val_compare_and_swap(&v, 7, 42) != 7) return 11;
    if (v != 42) return 12;
    if (__sync_val_compare_and_swap(&v, 7, 5) != 42) return 13;
    if (v != 42) return 14;
    if (!__sync_bool_compare_and_swap(&v, 42, 99)) return 15;
    if (v != 99) return 16;
    if (__sync_bool_compare_and_swap(&v, 42, 1)) return 17;
    if (v != 99) return 18;

    long long w = 1LL << 40;
    if (__sync_add_and_fetch(&w, 1) != (1LL << 40) + 1) return 19;

    int lock = 0;
    if (__sync_lock_test_and_set(&lock, 1) != 0) return 20;
    if (lock != 1) return 21;
    __sync_lock_release(&lock);
    if (lock != 0) return 22;

    printf("__sync OK\n");
    return 0;
}
