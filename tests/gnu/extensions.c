#include <stdio.h>
#include <string.h>
#include <stddef.h>

/* The GCC extensions that C written for gcc or clang actually contains.
   Each one is here because there is no ISO spelling that does the same
   thing, and a compiler meant to replace those two has to read what they
   read. Checked against clang -std=gnu11 like the rest of the suite.

   __auto_type and __int128 have files of their own next door, being large
   enough to say something about on their own. */

/* typeof, and the macro shape that needs it: an argument named once and
   evaluated once, with a type the macro does not know. */
#define MAX(a, b) ({ __typeof__(a) _a = (a); __typeof__(b) _b = (b); _a > _b ? _a : _b; })
#define SWAP(a, b) do { __typeof__(a) _t = (a); (a) = (b); (b) = _t; } while (0)

/* The comma swallow: no ISO spelling before C23's __VA_OPT__. */
#define LOG(fmt, ...) snprintf(buf, sizeof buf, fmt, ##__VA_ARGS__)

static char buf[64];
static int calls;
static int bump(void) { return ++calls; }

/* A zero-length array as a trailing member: what C written before C99
   uses where a flexible array member would go now. */
struct packet { int len; char body[0]; };

struct inner { int a; double b; };
struct outer { char pad; struct inner in; int arr[4]; };

static int classify(int x) {
    switch (x) {
    case 1 ... 5:   return 1;
    case 6 ... 9:   return 2;
    case 10:        return 3;
    case -3 ... -1: return 4;
    default:        return 0;
    }
}

/* A dispatch loop built from label addresses: the shape an interpreter has
   and a switch cannot be made into. */
static int run(const char *prog) {
    void *ops[] = {&&op_inc, &&op_dbl, &&op_end};
    int acc = 0;
    const char *pc = prog;
    goto *ops[(int)*pc];
op_inc:
    acc++;
    goto *ops[(int)*++pc];
op_dbl:
    acc *= 2;
    goto *ops[(int)*++pc];
op_end:
    return acc;
}

int main(void) {
    /* typeof on an expression, on a type name, and keeping qualifiers. */
    int i = 3;
    __typeof__(i) j = i * 2;
    __typeof(int) k = j + 1;   /* the type-name form */
    __typeof__(&i) p = &i;
    if (j != 6 || k != 7 || *p != 3) return 1;

    const int ci = 9;
    __typeof__(ci) cj = ci;
    if (cj != 9) return 2;

    double d = 1.5;
    __typeof__(d) e = d * 2;
    if (e != 3.0) return 3;

    /* A statement expression's value is its last statement's. */
    int r = ({ int t = 3; t * 4; });
    if (r != 12) return 4;
    if (({ 5; }) != 5) return 5;

    /* Which is what makes MAX evaluate each argument exactly once. */
    calls = 0;
    if (MAX(bump(), bump()) != 2) return 6;
    if (calls != 2) return 7;

    int x = 1, y = 2;
    SWAP(x, y);
    if (x != 2 || y != 1) return 8;

    /* A statement expression declares in its own scope. */
    int t = 100;
    int u = ({ int t = 5; t + 1; });
    if (t != 100 || u != 6) return 9;

    /* The comma swallow, with and without variadic arguments. */
    LOG("plain");
    if (strcmp(buf, "plain") != 0) return 10;
    LOG("%d-%d", 4, 5);
    if (strcmp(buf, "4-5") != 0) return 11;

    /* Binary constants. */
    if (0b1011 != 11) return 12;
    if (0b0 != 0 || 0b1 != 1) return 13;
    if (0b11111111 != 255) return 14;
    if (0b1U != 1u) return 15;

    /* A zero-length array contributes no size. */
    if (sizeof(struct packet) != sizeof(int)) return 16;
    struct packet *q = (struct packet *)buf;
    q->len = 3;
    q->body[0] = 'a';
    q->body[2] = 'c';
    if (q->body[0] != 'a' || q->body[2] != 'c') return 17;

    /* ---- case and designator ranges, label addresses ---- */

    if (classify(3) != 1) return 18;
    if (classify(7) != 2) return 19;
    if (classify(10) != 3) return 20;
    if (classify(-2) != 4) return 21;
    if (classify(0) != 0) return 22;
    if (classify(100) != 0) return 23;

    /* One designator, every element between the bounds. */
    int a[10] = {[0 ... 3] = 7, [4] = 1, [5 ... 9] = 8};
    if (a[0] != 7 || a[3] != 7 || a[4] != 1 || a[5] != 8 || a[9] != 8) return 24;

    int n = 2;
    int b[6] = {[0 ... 2] = n * 5, [3 ... 5] = n};
    if (b[1] != 10 || b[2] != 10 || b[4] != 2) return 25;

    /* __builtin_offsetof, including a path through a member and a
       subscript. */
    if (__builtin_offsetof(struct inner, b) != 8) return 26;
    if (__builtin_offsetof(struct outer, in) != offsetof(struct outer, in)) return 27;
    if (__builtin_offsetof(struct outer, in.b) != offsetof(struct outer, in) + 8) return 28;
    if (__builtin_offsetof(struct outer, arr[2]) != offsetof(struct outer, arr) + 8) return 29;

    /* "\x01\x01\x02" is inc, inc, dbl... run to the end marker. */
    if (run("\x00\x00\x01\x02") != 4) return 30;
    if (run("\x02") != 0) return 31;

    /* The compiler barrier every header writes as empty inline assembly. */
    int barrier = 1;
    __asm__ volatile ("" ::: "memory");
    if (barrier != 1) return 32;

    printf("GNU Extensions OK\n");
    return 0;
}
