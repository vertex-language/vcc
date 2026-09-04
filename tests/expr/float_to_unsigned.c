/* Converting a float to a 64-bit unsigned integer.
 *
 * x86-64 has no instruction for it: CVTTSD2SI writes a signed result, so
 * it can only produce values below 2^63 and saturates above. The upper half
 * of the range is the whole test — a value at or above 2^63 is biased down,
 * converted, and has the sign bit put back, and the two halves have to meet
 * exactly at the boundary. §6.3.1.4p1 leaves out-of-range undefined, so
 * nothing here asks for one.
 *
 * The one at 1e19 is what SQLite's sqlite3FpDecode converts. */

#include <stdio.h>
#include <inttypes.h>

static uint64_t d2u(double d) { return (uint64_t)d; }
static uint64_t f2u(float f)  { return (uint64_t)f; }

int main(void) {
    if (d2u(0.0) != 0) return 1;
    if (d2u(1.5) != 1) return 2;
    if (d2u(4294967295.0) != 4294967295ULL) return 3;
    if (d2u(4294967296.0) != 4294967296ULL) return 4;
    /* Below 2^63: the signed instruction's own range. */
    if (d2u(9223372036854774784.0) != 9223372036854774784ULL) return 5;
    /* At and above 2^63: the biased path. */
    if (d2u(9223372036854775808.0) != 9223372036854775808ULL) return 6;
    if (d2u(18446744073709549568.0) != 18446744073709549568ULL) return 7;
    if (d2u(1e19) != 10000000000000000000ULL) return 8;

    if (f2u(0.0f) != 0) return 9;
    if (f2u(1.9f) != 1) return 10;
    if (f2u(1e19f) != 9999999980506447872ULL) return 11;

    printf("float->u64 OK: %" PRIu64 " %" PRIu64 "\n", d2u(1e19), f2u(1e19f));
    return 0;
}
