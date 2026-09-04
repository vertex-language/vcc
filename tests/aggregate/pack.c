/* #pragma pack, and the allocation rule bit-fields follow.
 *
 * Both are implementation-defined, and both are things a platform decides
 * rather than a compiler: they say what a struct means to the kernel, to a
 * library, and to the next object file. So the numbers below are the
 * platform's, and the two platforms disagree.
 *
 * #pragma pack caps how strictly each member is aligned. The Windows SDK is
 * written in it — wingdi.h puts BITMAPFILEHEADER between pshpack2.h and
 * poppack.h, which is what makes it fourteen bytes rather than sixteen — and
 * a compiler that reads past the pragma lays every such struct out too long
 * and says nothing about it.
 *
 * Bit-fields differ more widely. Under the rule gcc and clang use, one is
 * placed at the current bit unless it would cross a boundary of its declared
 * type; under MSVC's, one opens a new allocation unit of its declared type
 * unless the member before it was a bit-field of the same size with room
 * left. `struct { char a; int b : 3; }` is four bytes under the first and
 * eight under the second, which is most structs rather than a corner. */

#include <stdio.h>

#pragma pack(push, 1)
struct P1 { char a; int b; short c; double d; };
#pragma pack(2)
struct P2 { char a; int b; short c; double d; };
#pragma pack(4)
struct P4 { char a; int b; short c; double d; };
#pragma pack(pop)
struct PN { char a; int b; short c; double d; };

/* A named push and pop, which is MSVC's spelling and which the SDK uses. */
#pragma pack(push, mine, 1)
struct Named { char a; long long b; };
#pragma pack(pop, mine)
struct AfterNamed { char a; long long b; };

/* Nested: an inner push must not leak out of the pop that matches it. */
#pragma pack(push, 2)
struct Outer { char a; int b; };
#pragma pack(push, 1)
struct Inner { char a; int b; };
#pragma pack(pop)
struct Back { char a; int b; };
#pragma pack(pop)

/* An array of a packed struct: the stride is the packed size, which is what
 * makes a file format readable. */
#pragma pack(1)
struct Rec { char tag; int value; };
#pragma pack()

/* The bit-field cases the two rules disagree about. */
struct BA { char a; int b : 3; };
struct BB { int a : 3; int b : 30; };
struct BC { char a : 3; int b : 3; };
struct BD { short a : 5; int b : 5; short c : 5; };
struct BE { int a : 3; char b : 3; };
struct BF { unsigned a : 1; unsigned b : 31; unsigned c : 1; };
struct BG { char a; short b : 5; char c; };

int main(void) {
    /* #pragma pack is the same on both, being a ceiling on alignment. */
    if (sizeof(struct P1) != 15) return 1;
    if (sizeof(struct P2) != 16 || _Alignof(struct P2) != 2) return 2;
    if (sizeof(struct P4) != 20 || _Alignof(struct P4) != 4) return 3;
    if (sizeof(struct PN) != 24 || _Alignof(struct PN) != 8) return 4;

    if (sizeof(struct Named) != 9) return 5;
    if (sizeof(struct AfterNamed) != 16) return 6;

    if (sizeof(struct Outer) != 6) return 7;
    if (sizeof(struct Inner) != 5) return 8;
    if (sizeof(struct Back) != 6) return 9;

    {
        struct Rec r[3] = {{1, 100}, {2, 200}, {3, 300}};
        if (sizeof(struct Rec) != 5) return 10;
        if ((char *)&r[1] - (char *)&r[0] != 5) return 11;
        if (r[2].value != 300) return 12;
        /* And the members are readable where the packing put them. */
        if (r[1].tag != 2 || r[1].value != 200) return 13;
    }

#ifdef _WIN32
    if (sizeof(struct BA) != 8) return 20;
    if (sizeof(struct BB) != 8) return 21;
    if (sizeof(struct BC) != 8) return 22;
    if (sizeof(struct BD) != 12) return 23;
    if (sizeof(struct BE) != 8) return 24;
    if (sizeof(struct BF) != 8) return 25;
    if (sizeof(struct BG) != 6) return 26;
#else
    if (sizeof(struct BA) != 4) return 30;
    if (sizeof(struct BB) != 8) return 31;
    if (sizeof(struct BC) != 4) return 32;
    if (sizeof(struct BD) != 4) return 33;
    if (sizeof(struct BE) != 4) return 34;
    if (sizeof(struct BF) != 8) return 35;
    if (sizeof(struct BG) != 4) return 36;
#endif

    /* Whatever the rule, the values read back. */
    {
        struct BD d = {0};
        d.a = 15; d.b = 9; d.c = -7;
        if (d.a != 15 || d.b != 9 || d.c != -7) return 40;
        struct BF f = {0};
        f.a = 1; f.b = 0x7FFFFFFFu; f.c = 1;
        if (f.a != 1 || f.b != 0x7FFFFFFFu || f.c != 1) return 41;
    }

    printf("Pack OK\n");
    return 0;
}
