/* The names a declaration gives things, and the one the language gives back.

   A prototype may leave its parameters unnamed and the definition that
   follows name them (§6.9.1p5), so the names cannot come from the type: the
   type is whichever declaration was reached first. The identifier-list form
   is the same problem from the other side — there the declarator says nothing
   about the parameters and only the definition's own declaration list does
   (§6.9.1p7). And inside any of them __func__ is a name the translator
   declares (§6.4.2.2). */

#include <stdio.h>
#include <string.h>

static int twice(int);
static int twice(int x) { return x + x; }
static int add(int, int);
static int add(int a, int b) { return a + b; }

/* The identifier-list definition, declared without a prototype first. */
static int knr();
static int knr(a, b) int a; long b; { return a + (int)b; }

/* A definition with no prior declaration, naming some parameters. */
static int mixed(int a, int, int c) { return a + c; }

/* An identifier-list definition whose parameters are narrower than int, or
   are float. §6.9.1p10 has them arrive promoted and converted down. */
static int narrow(c, s, f) char c; short s; float f; { return c + s + (int)f; }

/* __func__ is declared as if by `static const char __func__[] = "name";`
   at the top of every function body, so its size is the name's. */
static const char *where(void) { return __func__; }

static int compute(int x) {
    if (strcmp(__func__, "compute") != 0) return -1;
    if (sizeof __func__ != sizeof "compute") return -2;
    if (__func__[0] != 'c') return -3;
    if (strcmp(__FUNCTION__, "compute") != 0) return -4;
    return x;
}

int main(void) {
    if (twice(21) != 42) return 1;
    if (add(2, 3) != 5) return 2;
    if (knr(2, 3) != 5) return 3;
    if (mixed(1, 2, 3) != 4) return 4;
    if (narrow('A', 2, 3.5f) != 'A' + 2 + 3) return 5;

    if (strcmp(__func__, "main") != 0) return 6;
    if (strcmp(where(), "where") != 0) return 7;
    if (compute(0) != 0) return 8;

    printf("Names OK\n");
    return 0;
}
