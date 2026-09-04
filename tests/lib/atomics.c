#include <stdio.h>
#include <string.h>

/* Atomics, in the two layers C gives them: the _Atomic qualifier, where
   every access is atomic and a compound assignment is one read-modify-write,
   and <stdatomic.h>, which is that qualifier plus names for the operations.

   Single-threaded, so what is checked is the value rather than the ordering —
   but the ordering is what the instruction selection has to preserve. */

static _Atomic int counter = 0;
static _Atomic long long wide = 0;
static _Atomic unsigned char narrow = 0;
static _Atomic short half = 0;
static _Atomic _Bool flag = 0;

struct node { int v; };
static struct node nodes[4] = {{0}, {1}, {2}, {3}};
static struct node *_Atomic cursor = nodes;

#include <stdatomic.h>

static atomic_int named = ATOMIC_VAR_INIT(0);
static atomic_flag lock = ATOMIC_FLAG_INIT;

int main(void) {
    /* Load and store at each width. */
    counter = 7;
    if (counter != 7) return 1;
    wide = 1LL << 40;
    if (wide != 1099511627776LL) return 2;
    narrow = 250;
    if (narrow != 250) return 3;
    half = -300;
    if (half != -300) return 4;
    flag = 1;
    if (!flag) return 5;

    /* The value of an assignment is what was assigned. */
    if ((counter = 11) != 11) return 6;

    /* Compound assignment: the read-modify-write forms with an instruction
       of their own. */
    counter = 10;
    counter += 5;
    if (counter != 15) return 7;
    counter -= 3;
    if (counter != 12) return 8;
    counter &= 0xE;
    if (counter != 12) return 9;
    counter |= 1;
    if (counter != 13) return 10;
    counter ^= 0xF;
    if (counter != 2) return 11;
    if ((counter += 8) != 10) return 12;

    /* And the forms that have none, which go round a compare-and-swap. */
    counter = 6;
    counter *= 7;
    if (counter != 42) return 13;
    counter /= 6;
    if (counter != 7) return 14;
    counter %= 4;
    if (counter != 3) return 15;
    counter <<= 4;
    if (counter != 48) return 16;
    counter >>= 2;
    if (counter != 12) return 17;

    /* ++ and -- in both positions. */
    counter = 5;
    if (counter++ != 5) return 18;
    if (counter != 6) return 19;
    if (++counter != 7) return 20;
    if (counter-- != 7) return 21;
    if (--counter != 5) return 22;

    /* Narrow widths wrap in their own type, not in an int. */
    narrow = 255;
    narrow++;
    if (narrow != 0) return 23;
    narrow -= 1;
    if (narrow != 255) return 24;

    half = 32767;
    half = (short)(half + 1);
    if (half != -32768) return 25;

    /* Wide arithmetic really is 64-bit. */
    wide = 0;
    wide += 4294967296LL;
    if (wide != 4294967296LL) return 26;
    wide *= 3;
    if (wide != 12884901888LL) return 27;

    /* An atomic pointer steps in elements. §6.5.16.2 admits += on one, but
       clang does not compile it, so only ++ and -- are exercised here where
       both compilers can be compared. */
    cursor = nodes;
    if (cursor->v != 0) return 28;
    cursor++;
    cursor++;
    if (cursor->v != 2) return 29;
    cursor--;
    if (cursor->v != 1) return 30;
    if ((++cursor)->v != 2) return 31;

    /* An atomic float goes through a compare-and-swap on its bits. */
    _Atomic double d = 0.0;
    d = 1.5;
    if (d != 1.5) return 32;
    d += 2.25;
    if (d != 3.75) return 33;
    d *= 2.0;
    if (d != 7.5) return 34;

    _Atomic float sf = 1.0f;
    sf += 0.5f;
    if (sf != 1.5f) return 35;

    /* The qualifier does not change the size of a scalar. */
    if (sizeof(_Atomic int) != sizeof(int)) return 36;
    if (sizeof(_Atomic long long) != sizeof(long long)) return 37;

    /* ---- <stdatomic.h>: the same qualifier, named ---- */

    atomic_init(&named, 5);
    if (atomic_load(&named) != 5) return 38;
    atomic_store(&named, 9);
    if (named != 9) return 39;
    if (atomic_exchange(&named, 3) != 9) return 40;
    if (named != 3) return 41;
    if (atomic_fetch_add(&named, 4) != 3) return 42;
    if (named != 7) return 43;
    if (atomic_fetch_sub(&named, 2) != 7) return 44;
    if (atomic_fetch_or(&named, 8) != 5) return 45;
    if (named != 13) return 46;
    if (atomic_fetch_and(&named, 12) != 13) return 47;
    if (named != 12) return 48;
    if (atomic_fetch_xor(&named, 15) != 12) return 49;
    if (named != 3) return 50;

    int expected = 3;
    if (!atomic_compare_exchange_strong(&named, &expected, 42)) return 51;
    if (named != 42) return 52;
    expected = 3;
    if (atomic_compare_exchange_strong(&named, &expected, 7)) return 53;
    if (expected != 42) return 54;
    if (named != 42) return 55;

    if (atomic_flag_test_and_set(&lock)) return 56;
    if (!atomic_flag_test_and_set(&lock)) return 57;
    atomic_flag_clear(&lock);
    if (atomic_flag_test_and_set(&lock)) return 58;

    atomic_thread_fence(memory_order_seq_cst);
    atomic_signal_fence(memory_order_acquire);
    if (!atomic_is_lock_free(&named)) return 59;

    atomic_store_explicit(&named, 1, memory_order_release);
    if (atomic_load_explicit(&named, memory_order_acquire) != 1) return 60;
    if (atomic_fetch_add_explicit(&named, 1, memory_order_relaxed) != 1) return 61;

    printf("Atomics OK\n");
    return 0;
}
