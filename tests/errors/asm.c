/* Inline assembly that must not compile.
 *
 * The template is not read here — what a constraint letter means is the
 * target's — so every violation below is in the C around it: an output with
 * nowhere to be written, a name that does not resolve, a label an `asm goto`
 * branches to that does not exist.
 *
 *     vcc check tests/errors/asm.c
 */
int main(void) {
    int a = 0;
    const int c = 1;
    int arr[4] = {0};

    __asm__ ("nop" : "=r" (a + 1));          /* not an lvalue          */
    __asm__ ("nop" : "=r" (undeclared_out)); /* §6.5.1p2               */
    __asm__ ("nop" : : "r" (undeclared_in)); /* §6.5.1p2               */
    __asm__ ("nop" : "=r" (c));              /* const-qualified        */
    __asm__ ("nop" : "=r" (arr));            /* not modifiable         */
    __asm__ ("nop" : [x] "=r" (a), [x] "=r" (a)); /* duplicate name    */
    __asm__ goto ("nop" : : : : nowhere);    /* no such label          */

    return a;
}
