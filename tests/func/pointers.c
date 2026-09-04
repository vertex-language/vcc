/* Function pointers: the spellings that all mean the same call, a pointer
   chosen at run time, and a function that returns one.

   `(*fp)(x)` is the older spelling of `fp(x)` and means the same thing:
   §6.3.2.1p4 turns a function designator straight back into a pointer, so
   the dereference cancels. Nothing loads there — the address is a place in
   .text, not an object — and a compiler that treats it as an ordinary
   lvalue has nothing to load. */
#include <stdio.h>

static int add(int a, int b) { return a + b; }
static int sub(int a, int b) { return a - b; }
static int mul(int a, int b) { return a * b; }
typedef int (*binop)(int, int);
static binop table[] = {add, mul};

/* A pointer taken without &, which is the same value (§6.3.2.1p4), and one
   at file scope so the address is a link-time constant. */
static binop const chosen = mul;

static int apply(binop f, int a, int b) { return f(a, b); }
static int (*pick(int i))(int, int) { return table[i]; }

int main(void) {
    binop g = &add;

    /* The plain form, and reassignment through the same pointer. */
    binop op = add;
    if (op(5, 3) != 8) return 100;
    op = sub;
    if (op(5, 3) != 2) return 101;
    if (chosen(6, 7) != 42) return 102;

    /* A pointer compares equal however it was spelled, and against null. */
    if (op == 0) return 103;
    if (&sub != op) return 104;
    if (add != &add) return 105;

    if (apply(add, 2, 3) != 5) return 1;
    if (apply(table[1], 2, 3) != 6) return 2;
    if (pick(0)(4, 5) != 9) return 3;

    /* the dereferencing spellings, however many stars deep */
    if ((*g)(1, 1) != 2) return 4;
    if ((**g)(1, 2) != 3) return 5;
    if ((***&g)(2, 2) != 4) return 6;
    if ((*table[1])(3, 3) != 9) return 7;

    /* and through a call that returns one */
    if ((*pick(1))(3, 4) != 12) return 8;

    printf("Function Pointers OK\n");
    return 0;
}
