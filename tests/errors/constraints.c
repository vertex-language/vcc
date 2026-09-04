/* Constraint violations vcc must report.
 *
 * Everything under tests/errors is a negative test: `vcc check` on it is
 * expected to fail, and each line is annotated with the paragraph it
 * violates. One diagnostic per line, and no cascade after any of them —
 * an operand whose type could not be determined is not reported about
 * again, which is what keeps one mistake from becoming five.
 *
 *     vcc check tests/errors/constraints.c
 */
struct s { int a; };
struct t { double d; };

int prototyped(int a, char *b);
void nothing(void);

int main(void) {
    int i = 0;
    char *p = 0;
    struct s a = {1};
    struct t b = {1.0};

    i = undeclared_name;          /* §6.5.1p2  */
    p = 5;                        /* §6.5.16.1 */
    i = p;                        /* §6.5.16.1 */
    p = &i;                       /* §6.5.16.1 incompatible pointer */
    a = b;                        /* §6.5.16.1 incompatible struct */
    i = a + 1;                    /* §6.5.6    */
    i = a.missing;                /* §6.5.2.3  */
    i = p.a;                      /* §6.5.2.3  use -> */
    i = i.a;                      /* §6.5.2.3  not a record */
    i = prototyped(1);            /* §6.5.2.2  too few */
    i = prototyped(1, p, 3);      /* §6.5.2.2  too many */
    i = prototyped(p, p);         /* §6.5.2.2  argument type */
    i = nothing();                /* §6.5.16.1 void into int */
    i = i();                      /* §6.5.2.2  not callable */
    i = *i;                       /* §6.5.3.2  not a pointer */
    i = i % 1.0;                  /* §6.5.5    */
    i = p < a;                    /* §6.5.8    */
    i = 1 ? p : a;                /* §6.5.15   */
    const int c = 0;
    c = 1;                        /* §6.5.16p2 not modifiable */
    return p;                     /* §6.8.6.4  */
}

void v(void) { return 1; }        /* §6.8.6.4 value from void */
int r(void) { return; }           /* §6.8.6.4 nothing from int */
