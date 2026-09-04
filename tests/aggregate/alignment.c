/* Over-aligned types, in both spellings.
 *
 * gcc writes __attribute__((aligned(n))) and MSVC writes __declspec(align(n)),
 * and they mean the same thing: this type is placed at n bytes, and anything
 * containing it is too.
 *
 * Dropping one is not a conservative choice. <setjmp.h> declares the halves
 * of a jmp_buf with __declspec(align(16)), so a compiler that reads past it
 * gives the buffer eight-byte alignment; _setjmp then stores xmm6 into it
 * with movdqa, which faults on an unaligned address — inside the CRT, in a
 * program that looks entirely ordinary and never computed the address
 * itself. Lua's pcall is that program. */

#include <setjmp.h>
#include <stdio.h>
#include <stddef.h>

typedef struct __declspec(align(16)) MS16 { unsigned long long p[2]; } MS16;
typedef struct __attribute__((aligned(16))) GNU16 { unsigned long long p[2]; } GNU16;
struct __declspec(align(32)) Big { char c; };
static struct Big bigStatic;

/* Over-alignment propagates: a struct containing one is at least as aligned,
 * and an array of them strides by the padded size. */
struct Holder { char c; MS16 v; };

int main(void) {
    if (_Alignof(MS16) != 16) return 1;
    if (_Alignof(GNU16) != 16) return 2;
    if (sizeof(MS16) != 16 || sizeof(GNU16) != 16) return 3;

    /* An alignment larger than the type is padding, not a no-op. It is
     * checked on a static, because vcc gives a frame sixteen bytes and says
     * so rather than quietly handing back less: a local wanting thirty-two
     * is refused with a diagnostic. */
    if (_Alignof(struct Big) != 32) return 4;
    if (sizeof(struct Big) != 32) return 5;
    if ((size_t)&bigStatic % 32 != 0) return 14;

    if (_Alignof(struct Holder) != 16) return 6;
    if (offsetof(struct Holder, v) != 16) return 7;

    /* And the addresses really come out aligned. */
    {
        MS16 a[3];
        if ((size_t)&a[0] % 16 != 0) return 8;
        if ((size_t)&a[1] % 16 != 0) return 9;
        if ((char *)&a[1] - (char *)&a[0] != 16) return 10;
    }

    /* jmp_buf is the one that matters, and setjmp through a call is where
     * an unaligned one shows. */
    {
        jmp_buf b;
        if (_Alignof(jmp_buf) < 16) return 12;
        if ((size_t)(char *)b % 16 != 0) return 13;
    }

    printf("Alignment OK\n");
    return 0;
}
