/* <stddef.h> — C11 §7.19. Types from the target model's predefines. */
#ifndef _VCC_STDDEF_H
#define _VCC_STDDEF_H

typedef __PTRDIFF_TYPE__ ptrdiff_t;
typedef __SIZE_TYPE__    size_t;
typedef __WCHAR_TYPE__   wchar_t;

/* The type with the strictest alignment. long double has it on every
   model vcc currently carries; revisit if a Model gains a stricter one. */
typedef long double max_align_t;

/* §7.19 lets NULL be any implementation-defined null pointer constant, so a
   platform header that got here first has already spelled it its own way —
   __DARWIN_NULL on Apple's SDK. Redefining a macro to a different token
   sequence is a constraint violation, and diagnosing one the user cannot fix
   is noise, so this replaces rather than collides. */
#undef NULL
#define NULL ((void *)0)

/* Constant-folds in the analyzer; no compiler magic required. */
#define offsetof(type, member) ((size_t)&(((type *)0)->member))

#endif