#include <stdio.h>
#include <string.h>

/* Bit-field layout and access beyond the easy case: signedness at the
   boundary, a field that would straddle its storage unit, a zero-width
   field forcing alignment, unnamed padding fields, mixed declared types,
   and constant initialization. */

struct packed {
    unsigned a : 1;
    unsigned b : 2;
    unsigned c : 5;
    unsigned d : 8;
};                          /* 16 bits used, one unsigned of storage */

struct straddle {
    unsigned lo : 20;
    unsigned hi : 20;       /* does not fit alongside lo in one unit */
};

struct zerowidth {
    unsigned a : 4;
    unsigned   : 0;         /* start the next field at a new unit */
    unsigned b : 4;
};

struct unnamed {
    unsigned a : 3;
    unsigned   : 5;         /* five bits nothing can name */
    unsigned b : 3;
};

struct mixed {
    signed char sc : 4;
    unsigned char uc : 4;
    signed short ss : 9;
    unsigned short us : 9;
};

struct sign {
    signed int s : 4;       /* -8 .. 7 */
    unsigned int u : 4;     /* 0 .. 15 */
};

/* A field wider than any integer type has bits to spare, and one that fills
   its storage unit exactly. */
struct wide {
    unsigned a : 1;
    unsigned b : 24;
    unsigned c : 7;
};

/* A bit-field initialized by a constant expression at file scope. */
static const struct packed init = {1, 2, 3, 4};
static const struct sign edge = {-8, 15};

static int wide(struct straddle s) { return (int)(s.hi - s.lo); }

int main(void) {
    if (init.a != 1 || init.b != 2 || init.c != 3 || init.d != 4) return 1;
    if (edge.s != -8 || edge.u != 15) return 2;

    struct packed p = {0};
    p.a = 1; p.b = 3; p.c = 31; p.d = 255;
    if (p.a != 1 || p.b != 3 || p.c != 31 || p.d != 255) return 3;
    /* Writing one field leaves the others alone. */
    p.c = 0;
    if (p.a != 1 || p.b != 3 || p.d != 255) return 4;
    if (sizeof(struct packed) != sizeof(unsigned)) return 5;

    struct straddle st = {0};
    st.lo = 1000000;
    st.hi = 1048575;        /* 2^20 - 1 */
    if (st.lo != 1000000 || st.hi != 1048575) return 6;
    if (wide(st) != 1048575 - 1000000) return 7;

    struct zerowidth zw = {0};
    zw.a = 15; zw.b = 15;
    if (zw.a != 15 || zw.b != 15) return 8;
    if (sizeof zw < 2 * sizeof(unsigned) / 2) return 9;

    struct unnamed un = {0};
    un.a = 7; un.b = 7;
    if (un.a != 7 || un.b != 7) return 10;

    struct mixed m = {0};
    m.sc = -8; m.uc = 15; m.ss = -256; m.us = 511;
    if (m.sc != -8) return 11;
    if (m.uc != 15) return 12;
    if (m.ss != -256) return 13;
    if (m.us != 511) return 14;

    /* A signed bit-field wraps within its width, and the value read back
       is the one the width can represent. */
    struct sign s = {0};
    s.s = 7;
    if (s.s != 7) return 15;
    s.s = -8;
    if (s.s != -8) return 16;
    s.u = 15;
    if (s.u != 15) return 17;

    /* Compound assignment and ++ go through the same read-modify-write. */
    struct packed q = {0};
    q.d = 250;
    q.d += 5;
    if (q.d != 255) return 18;
    q.d++;                  /* wraps within eight bits */
    if (q.d != 0) return 19;
    q.b = 1;
    q.b <<= 1;
    if (q.b != 2) return 20;

    /* A bit-field is promoted to int in an expression. */
    struct packed r = {0};
    r.c = 20;
    if (sizeof(r.c + 0) != sizeof(int)) return 21;
    if (r.c * 2 != 40) return 22;

    /* Assignment through a struct copy carries the packed bits. */
    struct packed copy = p;
    if (memcmp(&copy, &p, sizeof p) != 0) return 23;

    struct wide w = {0};
    w.a = 1; w.b = 0xabcdef; w.c = 127;
    if (w.a != 1 || w.b != 0xabcdef || w.c != 127) return 24;
    if (sizeof(struct wide) != sizeof(unsigned)) return 25;
    w.b = 0;
    if (w.a != 1 || w.c != 127) return 26;

    printf("Bitfields OK\n");
    return 0;
}
