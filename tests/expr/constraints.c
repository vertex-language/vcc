#include <stdio.h>
#include <string.h>
#include <stdlib.h>

/* Code that is correct and must stay quiet. Every construct here is one a
   checker that reports §6.5's constraints can get wrong: an implicit
   conversion the standard admits, a null pointer constant in each of its
   spellings, a void * either way, a qualifier added rather than dropped,
   an unprototyped call, a variadic call, and a member of an anonymous
   member. */

/* §6.7.2.1p13's anonymous member is an untagged definition, not a
   reference to a named type. */
struct outer { struct { int a; }; int b; };
union tag { int i; double d; };

static int old_style();                  /* no prototype: nothing to check */
static int old_style(a, b) int a; int b; { return a + b; }

static int takes(int a, const char *s, ...) { (void)s; return a; }
static void *give(void) { return NULL; }
static const char *name(void) { return "vcc"; }

enum color { RED, GREEN };

int main(void) {
    /* Arithmetic conversions in every direction §6.3.1.8 allows. */
    char c = 'a';
    short s = c;
    int i = s;
    long l = i;
    long long ll = l;
    unsigned u = (unsigned)ll;
    float f = (float)u;
    double d = f;
    _Bool b = d != 0;
    if (!b) return 1;
    c = (char)i; s = (short)l; i = (int)d; u = (unsigned)i;
    if (c != 'a') return 2;

    /* An enumerator is an int, and an enum object takes one. */
    enum color k = GREEN;
    i = k;
    if (i != 1) return 3;

    /* Every spelling of the null pointer constant. */
    char *p = 0;
    char *q = NULL;
    char *r = (void *)0;
    int *ip = 0;
    if (p || q || r || ip) return 4;
    p = q = r = NULL;

    /* void * converts both ways without a cast. */
    void *v = give();
    int *ints = v;
    v = ints;
    if (v != ints) return 5;

    /* Adding a qualifier is fine; the other direction is what is not. */
    char buf[8] = "abc";
    const char *cp = buf;
    volatile const char *vcp = cp;
    if (vcp[0] != 'a') return 6;

    /* A string literal initializes a char array, and decays elsewhere. */
    const char *lit = "hello";
    if (strlen(lit) != 5) return 7;
    char arr[] = "hi";
    if (sizeof arr != 3) return 8;

    /* Pointer arithmetic, subscripting from either side, and a difference. */
    int nums[4] = {0, 1, 2, 3};
    int *np = nums + 2;
    if (np[-1] != 1) return 9;
    if (2[nums] != 2) return 10;
    if (np - nums != 2) return 11;

    /* Calls: prototyped, unprototyped, variadic, and through a pointer. */
    if (takes(1, "x") != 1) return 12;
    if (takes(1, "x", 2, 3.5, "y") != 1) return 13;
    if (old_style(2, 3) != 5) return 14;
    int (*fp)(int, const char *, ...) = takes;
    if (fp(9, name()) != 9) return 15;

    /* A member reached through an anonymous member. */
    struct outer o;
    o.a = 4;
    o.b = 5;
    if (o.a + o.b != 9) return 16;

    /* A union member, and a compound literal's member. */
    union tag t = {.i = 7};
    if (t.i != 7) return 17;
    if ((union tag){.d = 1.5}.d != 1.5) return 18;

    /* ?: with a null pointer constant in one arm, and with void * in one. */
    char *sel = i ? p : NULL;
    void *vsel = i ? v : (void *)0;
    if (sel != NULL || vsel != NULL) { /* either is fine */ }

    /* A conditional whose arms are compatible pointers. */
    const char *pick = i ? lit : name();
    if (pick == NULL) return 19;

    /* Compound assignment on a pointer and on arithmetic. */
    np += 1;
    np -= 1;
    d *= 2.0;
    i <<= 1;
    i |= 1;
    if (np != nums + 2) return 20;

    /* Returning a value assignable to the return type. */
    printf("Constraints OK\n");
    return 0;
}
