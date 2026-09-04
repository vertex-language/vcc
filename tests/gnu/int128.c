/* gcc's 128-bit integers, as far as vcc takes them.
 *
 * vcc knows their width and alignment but has no 128-bit register to compute
 * in, so an object of one may be declared, laid out in a record, pointed at
 * and copied — everything that is storage — while arithmetic on a value of
 * one is refused with a diagnostic saying so. This file is the storage half;
 * the refusal has no home in tests/errors, which runs `vcc check`, and this
 * limit is lower's to report.
 *
 * __int128 is a type specifier and not a typedef name, which is why
 * `unsigned __int128` is spelled here at all: a typedef name could not
 * combine with unsigned, and gcc and clang both make it a keyword.
 * __int128_t and __uint128_t are the typedef spellings of the same two types.
 *
 * The reason any of this exists is Darwin's arm/_mcontext.h, which puts a
 * __uint128_t in a struct reached from <stdio.h>: getting the width wrong
 * moved every member after it.
 */
#include <stdio.h>
#include <string.h>

struct S { int a; __uint128_t big; int c; };

static __uint128_t g_u;
static __int128 g_s;
static unsigned __int128 g_us;
static signed __int128 g_ss;
static __int128 unsigned g_su; /* the reversed spelling is the same type */

int main(void) {
    /* width and alignment, in both the keyword and typedef spellings */
    if (sizeof(__int128) != 16) return 1;
    if (sizeof(unsigned __int128) != 16) return 2;
    if (sizeof(signed __int128) != 16) return 3;
    if (sizeof(__int128_t) != 16) return 4;
    if (sizeof(__uint128_t) != 16) return 5;
    if (_Alignof(__int128) != 16) return 6;
    if (_Alignof(unsigned __int128) != 16) return 7;

    /* sizeof an object, not a type name */
    if (sizeof g_u != 16) return 8;
    if (sizeof g_ss != 16) return 9;
    if (sizeof g_su != 16) return 10;

    /* a record holding one: the member's offset and the whole size */
    if (sizeof(struct S) != 48) return 11;
    if (__builtin_offsetof(struct S, big) != 16) return 12;
    if (__builtin_offsetof(struct S, c) != 32) return 13;

    /* an object of that record exists, and the members around the
     * 128-bit one are still reachable at the right places */
    struct S l;
    l.a = 7; l.c = 9;
    if (l.a != 7 || l.c != 9) return 14;

    /* a standalone object gets the alignment its type asks for */
    if ((unsigned long)(void *)&g_u % 16 != 0) return 15;
    if ((unsigned long)(void *)&g_s % 16 != 0) return 16;

    /* storage may be pointed at and copied, which needs no register */
    __uint128_t *p = &g_u;
    memcpy(p, &g_us, sizeof g_u);
    if (memcmp(p, &g_us, sizeof g_u) != 0) return 17;

    /* distinct objects, not one aliased four ways */
    if (&g_u == (__uint128_t *)&g_us) return 18;

    printf("Int128 OK\n");
    return 0;
}
