/* gcc's assembler labels and file-scope assembly.
 *
 * Neither is inline assembly in the operand-carrying sense: a label renames
 * a symbol and emits nothing, and a file-scope block emits text and declares
 * nothing. They are here because a compiler that reads system headers meets
 * both — a libc points fopen at fopen64 with a label — and because the two
 * together are how a C program reaches a symbol it did not define in C.
 *
 * A label is a symbol name and not a C name, so it carries whatever prefix
 * the platform's symbols carry. That is the one thing about it that is not
 * portable, and ASMNAME is the whole of the accommodation.
 */
#include <stdio.h>

#ifdef __APPLE__
#define ASMNAME(s) "_" s
#else
#define ASMNAME(s) s
#endif

/* A label on a definition renames the symbol; the object keeps its C name. */
int counter __asm__(ASMNAME("vcc_test_counter")) = 3;

/* A label on a declaration whose definition follows. */
static int helper(void) __asm__(ASMNAME("vcc_test_helper"));
static int helper(void) { return 4; }

/* A label on an import: the reference must name the label, which is how a
 * header reaches a differently named definition. */
extern unsigned long measure(const char *) __asm__(ASMNAME("strlen"));

#if defined(__aarch64__)
__asm__(".text\n"
        ".globl " ASMNAME("vcc_test_five") "\n"
        ASMNAME("vcc_test_five") ":\n"
        "\tmov w0, #5\n"
        "\tret");
#define HAVE_FILE_ASM 1
#elif defined(__x86_64__)
__asm__(".text\n"
        ".globl " ASMNAME("vcc_test_five") "\n"
        ASMNAME("vcc_test_five") ":\n"
        "\tmovl $5, %eax\n"
        "\tret");
#define HAVE_FILE_ASM 1
#endif

#ifdef HAVE_FILE_ASM
int vcc_test_five(void) __asm__(ASMNAME("vcc_test_five"));
#endif

int main(void) {
    if (counter != 3) return 1;
    if (helper() != 4) return 2;
    if (measure("abcd") != 4) return 3;
#ifdef HAVE_FILE_ASM
    if (vcc_test_five() != 5) return 4;
#endif
    printf("Asm labels OK\n");
    return 0;
}
