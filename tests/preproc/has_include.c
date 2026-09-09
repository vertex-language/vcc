#include <stdio.h>

/* __has_include, which is an operator rather than a macro: its operand is a
   header-name, and a header-name is not an expression. Every check here is
   one a real system header makes -- the SDK on a Mac cannot be read at all
   without this, because Availability.h asks before it declares anything. */

/* The operator answers `defined`, which is how a portable header finds out
   whether it may use it. */
#if !defined(__has_include)
#error "__has_include should be defined"
#endif

#ifndef __has_include
#error "__has_include should answer #ifdef too"
#endif

/* A header that is there, and one that is not. */
#if __has_include(<stdio.h>)
#define HAVE_STDIO 1
#else
#define HAVE_STDIO 0
#endif

#if __has_include(<no/such/header.h>)
#define HAVE_BOGUS 1
#else
#define HAVE_BOGUS 0
#endif

/* The quoted form looks beside this file first, which is the whole
   difference between the two spellings. */
#if __has_include("has_a/present.h")
#define HAVE_LOCAL 1
#else
#define HAVE_LOCAL 0
#endif

#if __has_include("has_a/absent.h")
#define HAVE_LOCAL_BOGUS 1
#else
#define HAVE_LOCAL_BOGUS 0
#endif

/* Asking does not include: the macro that header defines is not defined
   here. */
#ifdef PRESENT_H_WAS_READ
#error "__has_include read the header instead of looking for it"
#endif

/* It composes with the rest of a controlling expression, and the operand
   with a slash and a dot in it survives -- neither is a token between the
   angle brackets. */
#if defined(__has_include) && __has_include(<sys/types.h>) && !__has_include(<sys/nope.h>)
#define COMPOSED 1
#else
#define COMPOSED 0
#endif

/* Both branches of an #if that is not taken are skipped, operator and all:
   an unresolved __has_include inside a dead branch is not an error. */
#if 0
#if __has_include(<still/not/scanned.h>)
#endif
#endif

int main(void) {
    if (HAVE_STDIO != 1) return 1;
    if (HAVE_BOGUS != 0) return 2;
    if (HAVE_LOCAL != 1) return 3;
    if (HAVE_LOCAL_BOGUS != 0) return 4;
    if (COMPOSED != 1) return 5;

    /* And the header it found really is includable, which is the claim the
       operator makes. */
#if __has_include("has_a/present.h")
#include "has_a/present.h"
#endif
#ifndef PRESENT_H_WAS_READ
    return 6;
#endif

    printf("__has_include OK\n");
    return 0;
}
