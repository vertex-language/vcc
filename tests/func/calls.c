#include <stdio.h>
#include <string.h>
#include <stdarg.h>

/* Argument passing where the ABI has to be exactly right: more arguments
   than there are registers, aggregates of every size class, mixed integer
   and floating sequences, and returns that travel in a register pair, in
   two floats, or through a hidden pointer. */

struct s1  { char a; };
struct s8  { int a, b; };
struct s16 { long a, b; };
struct s24 { long a, b, c; };
struct s32 { long a, b, c, d; };
struct f2  { float x, y; };
struct f3  { float x, y, z; };   /* three floats: its own size class */
struct d2  { double x, y; };
struct d4  { double a, b, c, d; };
struct mixed { int i; double d; };

static int many(int a, int b, int c, int d, int e, int f,
                int g, int h, int i, int j, int k, int l) {
    return a + b*2 + c*3 + d*4 + e*5 + f*6 + g*7 + h*8 + i*9 + j*10 + k*11 + l*12;
}

static double manyd(double a, double b, double c, double d, double e,
                    double f, double g, double h, double i, double j) {
    return a + b*2 + c*3 + d*4 + e*5 + f*6 + g*7 + h*8 + i*9 + j*10;
}

static double interleave(int a, double b, int c, double d, int e, double f,
                         int g, double h, int i, double j) {
    return a + b + c + d + e + f + g + h + i + j;
}

static struct s1  ret1(char a)              { struct s1 s = {a}; return s; }
static struct s8  ret8(int a, int b)        { struct s8 s = {a, b}; return s; }
static struct s16 ret16(long a, long b)     { struct s16 s = {a, b}; return s; }
static struct s24 ret24(long a)             { struct s24 s = {a, a+1, a+2}; return s; }
static struct s32 ret32(long a)             { struct s32 s = {a, a+1, a+2, a+3}; return s; }
static struct f2  retf2(float x, float y)   { struct f2 s = {x, y}; return s; }
static struct f3  retf3(float x)            { struct f3 s = {x, x+1, x+2}; return s; }
static struct f3  addf3(struct f3 a, struct f3 b) {
    struct f3 s = {a.x + b.x, a.y + b.y, a.z + b.z};
    return s;
}
static struct d2  retd2(double x, double y) { struct d2 s = {x, y}; return s; }
static struct d4  retd4(double x)           { struct d4 s = {x, x*2, x*3, x*4}; return s; }
static struct mixed retmixed(int i, double d) { struct mixed s = {i, d}; return s; }

static long take24(struct s24 s) { return s.a * 100 + s.b * 10 + s.c; }
static long take32(struct s32 s) { return s.a + s.b + s.c + s.d; }
static double taked4(struct d4 s) { return s.a + s.b + s.c + s.d; }
static double takemixed(struct mixed m) { return m.i + m.d; }

/* An aggregate argument after the registers are exhausted goes on the
   stack, and a large one goes there by reference on some ABIs. */
static long spill(int a, int b, int c, int d, int e, int f, int g, int h,
                  struct s24 s, int tail) {
    return a+b+c+d+e+f+g+h + s.a + s.b + s.c + tail;
}

static int sum_varargs(int n, ...) {
    va_list ap;
    va_start(ap, n);
    int t = 0;
    for (int i = 0; i < n; i++) t += va_arg(ap, int);
    va_end(ap);
    return t;
}

int main(void) {
    if (many(1,1,1,1,1,1,1,1,1,1,1,1) != 78) return 1;
    if (many(0,0,0,0,0,0,0,0,0,0,0,1) != 12) return 2;
    if (manyd(1,1,1,1,1,1,1,1,1,1) != 55.0) return 3;
    if (interleave(1, 2.0, 3, 4.0, 5, 6.0, 7, 8.0, 9, 10.0) != 55.0) return 4;

    if (ret1('z').a != 'z') return 5;
    struct s8 a8 = ret8(3, 4);
    if (a8.a != 3 || a8.b != 4) return 6;
    struct s16 a16 = ret16(5, 6);
    if (a16.a != 5 || a16.b != 6) return 7;
    struct s24 a24 = ret24(1);
    if (a24.a != 1 || a24.b != 2 || a24.c != 3) return 8;
    struct s32 a32 = ret32(10);
    if (a32.a != 10 || a32.d != 13) return 9;

    struct f2 af = retf2(1.5f, 2.5f);
    if (af.x != 1.5f || af.y != 2.5f) return 10;
    struct f3 af3 = retf3(1.0f);
    if (af3.x != 1.0f || af3.y != 2.0f || af3.z != 3.0f) return 23;
    struct f3 sum3 = addf3(af3, (struct f3){4.0f, 5.0f, 6.0f});
    if (sum3.x != 5.0f || sum3.y != 7.0f || sum3.z != 9.0f) return 24;

    struct d2 ad = retd2(1.25, 2.25);
    if (ad.x != 1.25 || ad.y != 2.25) return 11;
    struct d4 ad4 = retd4(1.0);
    if (ad4.a != 1.0 || ad4.d != 4.0) return 12;
    struct mixed am = retmixed(7, 0.5);
    if (am.i != 7 || am.d != 0.5) return 13;

    if (take24(a24) != 123) return 14;
    if (take32(a32) != 10+11+12+13) return 15;
    if (taked4(ad4) != 10.0) return 16;
    if (takemixed(am) != 7.5) return 17;

    if (spill(1,2,3,4,5,6,7,8, a24, 100) != 36 + 6 + 100) return 18;

    /* An aggregate passed by value is a copy the callee may scribble on. */
    struct s24 keep = a24;
    (void)take24(a24);
    if (memcmp(&keep, &a24, sizeof keep) != 0) return 19;

    if (sum_varargs(0) != 0) return 20;
    if (sum_varargs(3, 1, 2, 3) != 6) return 21;
    if (sum_varargs(9, 1,1,1,1,1,1,1,1,1) != 9) return 22;

    printf("Calls OK\n");
    return 0;
}
