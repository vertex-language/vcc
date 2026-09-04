/* The alignment an object's *declaration* asks for, in all four spellings.
 *
 * aggregate/alignment.c covers _Alignas on a type and on a member. This is
 * the other half: a declaration that overrides its type's alignment, which
 * C spells _Alignas, gcc spells __attribute__((aligned(n))) in either of two
 * positions, and MSVC spells __declspec(align(n)).
 *
 * It is not decoration. Every SIMD library in C reserves its buffers this
 * way — stb_image's STBI_SIMD_ALIGN is `type name __attribute__((aligned(16)))`
 * on the MSVC-less path and `__declspec(align(16)) type name` on the other —
 * and then reads them back with an aligned vector load, which faults rather
 * than being slow when the buffer is not where it was asked to be.
 *
 * The padding declarations around each one are deliberate: an object at the
 * front of a frame or a section can be aligned by accident, and a test that
 * cannot tell the difference is not one.
 *
 * Block-scope alignment stops at sixteen, which is what the stack itself
 * guarantees. A stricter one needs the frame realigned, and vcc says so
 * ("ptr.alloc wants 32-byte alignment; the frame guarantees 16") rather than
 * quietly giving the object less than it asked for. File scope has no such
 * limit — the section places the object — so the globals here ask for 32.
 *
 * Each non-standard spelling is guarded by the compiler that has it, so the
 * file runs unchanged against cl.exe (/std:c11), gcc and clang. vcc accepts
 * all four and so runs all four.
 */

#include <stdio.h>
#include <stddef.h>

#define ALIGNED(p, n) ((((unsigned long long) (size_t) (void *) (p)) & ((n) - 1)) == 0)

#if defined(__GNUC__)
#define HAVE_GNU_ALIGNED 1
#endif
#if defined(_MSC_VER) || defined(__VCC__)
#define HAVE_MS_ALIGN 1
#endif

/* File scope: the same spellings, where the alignment falls to the object's
 * section placement rather than to a frame offset. */
static char g_pad1 = 1;
static _Alignas(32) int g_alignas;
static char g_pad2 = 2;
#ifdef HAVE_GNU_ALIGNED
static __attribute__((aligned(32))) int g_attr_leading;
static char g_pad3 = 3;
static int g_attr_trailing __attribute__((aligned(32)));
#endif
static char g_pad4 = 4;
#ifdef HAVE_MS_ALIGN
static __declspec(align(32)) int g_declspec;
#endif
static char g_pad5 = 5;

int main(void) {
    /* Block scope. */
    char pad1;
    _Alignas(16) int alignas_local;
    char pad2;
#ifdef HAVE_GNU_ALIGNED
    __attribute__((aligned(16))) int attr_leading;
    char pad3;
    short attr_trailing[64] __attribute__((aligned(16)));
#endif
    char pad4;
#ifdef HAVE_MS_ALIGN
    __declspec(align(16)) int declspec_local;
#endif
    char pad5;

    (void) pad1; (void) pad2; (void) pad4; (void) pad5;

    if (!ALIGNED(&alignas_local, 16)) return 1;
    if (!ALIGNED(&g_alignas, 32)) return 2;

#ifdef HAVE_GNU_ALIGNED
    (void) pad3;
    if (!ALIGNED(&attr_leading, 16)) return 3;
    if (!ALIGNED(attr_trailing, 16)) return 4;
    if (!ALIGNED(&g_attr_leading, 32)) return 5;
    if (!ALIGNED(&g_attr_trailing, 32)) return 6;

    /* An attribute names one declarator and not the whole declaration:
     * `int a __attribute__((aligned(16))), b;` aligns a and leaves b. Only
     * the positive half is checked — that b is *un*aligned is luck, and a
     * test may not rest on it. */
    {
        int one __attribute__((aligned(16))), two;
        (void) two;
        if (!ALIGNED(&one, 16)) return 7;
    }
#endif

#ifdef HAVE_MS_ALIGN
    if (!ALIGNED(&declspec_local, 16)) return 8;
    if (!ALIGNED(&g_declspec, 32)) return 9;
#endif

    /* The alignment is a floor, never a ceiling: asking for less than the
     * type wants changes nothing. */
    {
        _Alignas(1) double d;
        if (!ALIGNED(&d, sizeof(double) < 8 ? sizeof(double) : 8)) return 10;
    }

    /* The padding objects exist so the aligned ones cannot be aligned by
     * being first. Read them so nothing elides them. */
    if (g_pad1 + g_pad2 + g_pad4 + g_pad5 != 12) return 11;

    printf("Alignment attributes OK\n");
    return 0;
}
