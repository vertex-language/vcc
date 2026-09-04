/* gcc's inline assembly.
 *
 * Every case here is written twice, once per architecture, because the
 * instructions are the point: what is under test is the operand model —
 * which register an operand lands in, which direction it travels, and what
 * the template's %0 turns into — and that can only be checked by running
 * the instructions and looking at the answer.
 *
 * A target with no section here compiles the file down to `return 0`, which
 * is the honest result: nothing was tested.
 */
#include <stdio.h>

#if defined(__aarch64__) || defined(__x86_64__)
#define HAVE_ASM 1
#endif

/* The word an x86-64 template moves with movq, and an aarch64 one with an x
   register. It is not `long`: that is eight bytes under LP64 and four under
   LLP64, so on Windows every movq below would name a four-byte object and
   the assembler would refuse the form — correctly. */
typedef long long asmword;

int main(void) {
#ifdef HAVE_ASM
    /* An output and an input, the plain shape. */
    int a = 7, b = 0;
#if defined(__aarch64__)
    __asm__ ("mov %0, %1" : "=r" (b) : "r" (a));
#else
    __asm__ ("movl %1, %0" : "=r" (b) : "r" (a));
#endif
    if (b != 7) return 1;

    /* Read-write: one register in both roles, which the IR spells as an
     * output and an input tied to it. */
    int rw = 5;
#if defined(__aarch64__)
    __asm__ ("add %0, %0, #3" : "+r" (rw));
#else
    __asm__ ("addl $3, %0" : "+r" (rw));
#endif
    if (rw != 8) return 2;

    /* Symbolic operand names, which are an alias for a position. */
    int src = 11, dst = 0;
#if defined(__aarch64__)
    __asm__ ("mov %[d], %[s]" : [d] "=r" (dst) : [s] "r" (src));
#else
    __asm__ ("movl %[s], %[d]" : [d] "=r" (dst) : [s] "r" (src));
#endif
    if (dst != 11) return 3;

    /* A memory operand: the template stores through the address, so there
     * is no output register and the object is written where it lives. */
    asmword m = 0;
#if defined(__aarch64__)
    __asm__ ("mov x9, #42\n\tstr x9, %0" : "=m" (m) : : "x9");
#else
    __asm__ ("movq $42, %0" : "=m" (m));
#endif
    if (m != 42) return 4;

    /* An input read from memory rather than from a register. */
    asmword in = 13, out = 0;
#if defined(__aarch64__)
    __asm__ ("ldr %0, %1" : "=r" (out) : "m" (in));
#else
    __asm__ ("movq %1, %0" : "=r" (out) : "m" (in));
#endif
    if (out != 13) return 5;

    /* Two outputs, numbered before the inputs. */
    int p = 0, q = 0, seed = 3;
#if defined(__aarch64__)
    __asm__ ("mov %0, %2\n\tadd %1, %2, #1"
             : "=&r" (p), "=&r" (q) : "r" (seed));
#else
    /* %q2 asks for the 64-bit view of the operand, which is what an
     * address wants: a 32-bit base register in 64-bit mode needs the
     * address-size prefix, and %q is how gcc's own headers avoid it. */
    __asm__ ("movl %2, %0\n\tleal 1(%q2), %1"
             : "=&r" (p), "=&r" (q) : "r" (seed));
#endif
    if (p != 3 || q != 4) return 6;

    /* asm goto, whose labels the assembled text branches to. */
    asmword zero = 0;
#if defined(__aarch64__)
    __asm__ goto ("cbz %0, %l[is_zero]" : : "r" (zero) : : is_zero);
#else
    __asm__ goto ("testq %0, %0\n\tjz %l[is_zero]" : : "r" (zero) : "cc" : is_zero);
#endif
    return 7;

is_zero:
    /* A second asm goto in the same function, which is where a block label
     * and the fragment that branches to it can be emitted in either order. */
    {
        asmword one = 1;
#if defined(__aarch64__)
        __asm__ goto ("cbz %0, %l[wrong]" : : "r" (one) : : wrong);
#else
        __asm__ goto ("testq %0, %0\n\tjz %l[wrong]" : : "r" (one) : "cc" : wrong);
#endif
    }

    /* asm goto with an output. The value is written before the branch and
     * read on the path that fell through, which is where gcc defines it. */
    asmword probe = 0;
#if defined(__aarch64__)
    __asm__ goto ("add %0, %1, #10\n\tcbz %1, %l[skipped]"
                  : "=&r" (probe) : "r" (zero + 3) : : skipped);
#else
    __asm__ goto ("leaq 10(%1), %0\n\ttestq %1, %1\n\tjz %l[skipped]"
                  : "=&r" (probe) : "r" (zero + 3) : "cc" : skipped);
#endif
    if (probe != 13) return 11;
    goto after_probe;
skipped:
    return 12;
after_probe:

    /* Basic asm: no colon, no operands, always volatile. */
    __asm__ ("nop");

    /* The compiler barrier every header writes. */
    __asm__ __volatile__ ("" ::: "memory");

    /* A float operand lands in the vector file, which the operand's own type
     * decides and no constraint letter has to say. */
    double d = 2.5, dout = 0;
#if defined(__aarch64__)
    __asm__ ("fadd %d0, %d1, %d1" : "=w" (dout) : "w" (d));
#else
    __asm__ ("addsd %1, %0" : "=x" (dout) : "x" (d), "0" (d));
#endif
    if (dout != 5.0) return 13;

    /* An output that is not a name: a member, an element, a dereference. */
    struct { asmword x, y; } rec = {0, 0};
    asmword cell[3] = {0, 0, 0};
    asmword thing = 0, *ptr = &thing;
#if defined(__aarch64__)
    __asm__ ("mov %0, #6" : "=r" (rec.y));
    __asm__ ("mov %0, #7" : "=r" (cell[1]));
    __asm__ ("mov %0, #8" : "=r" (*ptr));
#else
    __asm__ ("movq $6, %0" : "=r" (rec.y));
    __asm__ ("movq $7, %0" : "=r" (cell[1]));
    __asm__ ("movq $8, %0" : "=r" (*ptr));
#endif
    if (rec.y != 6 || cell[1] != 7 || thing != 8) return 14;

    /* %% is a literal percent, which has to survive the renumbering. */
    asmword pct = 0;
#if defined(__aarch64__)
    __asm__ ("mov %0, #1 // 100%%" : "=r" (pct));
#else
    __asm__ ("movq $1, %0 # 100%%" : "=r" (pct));
#endif
    if (pct != 1) return 15;

    /* A tied operand written by the source, which numbers the outputs
     * first — so "0" is the output above it and not the input beside it. */
    asmword tied = 4;
    asmword tied_out = 0;
#if defined(__aarch64__)
    __asm__ ("add %0, %0, #1" : "=r" (tied_out) : "0" (tied));
#else
    __asm__ ("addq $1, %0" : "=r" (tied_out) : "0" (tied));
#endif
    if (tied_out != 5) return 9;

    /* A template expanded twice in one function, each with the same numeric
     * local label in it. The two are different labels. */
    for (int i = 0; i < 2; i++) {
        asmword spin = 1;
#if defined(__aarch64__)
        __asm__ ("1:\n\tsubs %0, %0, #1\n\tb.ne 1b" : "+r" (spin) : : "cc");
#else
        __asm__ ("1:\n\tdecq %0\n\tjnz 1b" : "+r" (spin) : : "cc");
#endif
        if (spin != 0) return 10;
    }
#endif /* HAVE_ASM */

    printf("Asm OK\n");
    return 0;

#ifdef HAVE_ASM
wrong:
    return 8;
#endif
}
