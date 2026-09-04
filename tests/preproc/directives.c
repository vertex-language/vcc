#include <stdio.h>
#include <string.h>

/* Phase 4 on its own terms: object- and function-like macros, # and ##,
   variadic macros, rescanning and the hide set, conditionals, and the
   predefined names §6.10.8 requires. */

#define ONE 1
#define TWO (ONE + ONE)
#define ADD(a, b) ((a) + (b))
#define MUL(a, b) ((a) * (b))
#define APPLY(f, a, b) f(a, b)

#define STR(x) #x
#define XSTR(x) STR(x)
#define CAT(a, b) a##b
#define XCAT(a, b) CAT(a, b)

#define EMPTY
#define PAREN() ()

/* Recursion stops at the hide set rather than looping. */
#define SELF SELF
#define A B
#define B A

#define COUNT(...) COUNT_(__VA_ARGS__, 5, 4, 3, 2, 1, 0)
#define COUNT_(a, b, c, d, e, n, ...) n
#define FIRST(x, ...) (x)

int main(void) {
    if (TWO != 2) return 1;
    if (ADD(2, 3) != 5) return 2;
    if (APPLY(MUL, 3, 4) != 12) return 3;

    /* An argument is expanded before substitution unless # or ## sees it. */
    if (strcmp(STR(ONE), "ONE") != 0) return 4;
    if (strcmp(XSTR(ONE), "1") != 0) return 5;
    if (strcmp(XSTR(ADD(1, 2)), "((1) + (2))") != 0) return 6;

    /* # keeps the spelling and escapes what has to be escaped. */
    if (strcmp(STR("q\n"), "\"q\\n\"") != 0) return 7;

    /* ## pastes before rescanning; XCAT expands its arguments first. */
    int ab = 7;
    if (CAT(a, b) != 7) return 8;
    int x1 = 9;
    if (XCAT(x, ONE) != 9) return 9;

    /* An empty macro vanishes; a macro naming itself stops. */
    if (ONE EMPTY + ONE != 2) return 10;
    (void)0;

    /* Variadic macros, including the count trick that needs the comma
       handling to be right. */
    if (COUNT(9) != 1) return 11;
    if (COUNT(9, 8) != 2) return 12;
    if (COUNT(9, 8, 7, 6) != 4) return 13;
    if (FIRST(5, 6, 7) != 5) return 14;

    /* Conditionals, defined(), and arithmetic in #if. */
#if ONE + ONE == 2 && defined(TWO) && !defined(NOPE)
    int cond = 1;
#else
    int cond = 0;
#endif
    if (!cond) return 15;

#if 0
    this line is not even scanned for anything but nesting
#  if 1
#  endif
    still not scanned;
#elif 1
    cond = 2;
#else
    cond = 3;
#endif
    if (cond != 2) return 16;

    /* An undefined name is 0 in #if, and division by that would be an
       error, so it is only compared. */
#if UNDEFINED_NAME
    return 17;
#endif

    /* #undef and redefinition. */
#undef ONE
#define ONE 100
    if (ONE != 100) return 18;

    /* The predefined names. __LINE__ tracks; __FILE__ is this file. */
    int line = __LINE__;
    if (line + 1 != __LINE__) return 19;
    if (strstr(__FILE__, "preproc") == NULL) return 20;
    if (__STDC__ != 1) return 21;
    if (__STDC_HOSTED__ != 1) return 22;
    if (__STDC_VERSION__ < 201112L) return 23;

    /* _Pragma and #pragma are accepted and ignored. */
#pragma pack(push)
#pragma pack(pop)

    printf("Preproc OK\n");
    return 0;
}
