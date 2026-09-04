#include <stdio.h>
#include <stddef.h>

/* __attribute__((packed)) and ((aligned)) decide a record's shape, so a
   compiler that drops them produces a struct that reads the wrong bytes —
   a wire format is a struct whose layout the protocol chose. Every case
   here agrees with clang on size, alignment and offset. */
struct __attribute__((packed)) a { char c; int i; short s; };
struct __attribute__((packed, aligned(8))) b { char c; int i; };
struct c { char c; int i; } __attribute__((packed));
struct __attribute__((aligned(32))) d { int i; };
struct __attribute__((packed)) e { char c; unsigned x:3; unsigned y:9; char t; };
struct __attribute__((__packed__)) f { char c; long l; };
#define P(N,M) printf(#N " %zu/%zu off=%zu\n", sizeof(struct N), _Alignof(struct N), offsetof(struct N, M));
int main(void) {
    if (sizeof(struct a) != 7 || _Alignof(struct a) != 1) return 1;
    if (offsetof(struct a, s) != 5) return 2;
    if (sizeof(struct b) != 8 || _Alignof(struct b) != 8) return 3;
    if (sizeof(struct c) != 5 || offsetof(struct c, i) != 1) return 4;
    if (sizeof(struct d) != 32 || _Alignof(struct d) != 32) return 5;
    if (sizeof(struct e) != 4 || offsetof(struct e, t) != 3) return 6;
    /* Written against sizeof(long) rather than 8: a packed record's size is
       one byte plus the member's, and long is four bytes under LLP64. */
    if (sizeof(struct f) != 1 + sizeof(long) || offsetof(struct f, l) != 1) return 7;

    /* The members read and write through the unaligned offsets. */
    struct a v = {0};
    v.c = 'z'; v.i = 0x01020304; v.s = -2;
    if (v.c != 'z' || v.i != 0x01020304 || v.s != -2) return 8;

    struct e w = {0};
    w.c = 1; w.x = 5; w.y = 300; w.t = 2;
    if (w.c != 1 || w.x != 5 || w.y != 300 || w.t != 2) return 9;

    printf("Packed OK\n");
    return 0;
}
