#include <stdio.h>

/* __has_builtin and the four __is_target_* operators, which are how a
   header asks what it is being compiled for. TargetConditionals.h on a Mac
   asks all four, gated on __has_builtin, and what it decides from them is
   whether this is Catalyst or a simulator -- facts no predefined macro
   carries, because they are about the triple rather than the platform.

   Every check here is one clang answers the same way, which is what makes
   the file runnable against either compiler. */

#if !defined(__has_builtin)
#error "__has_builtin should be defined"
#endif

/* The operators are builtins, and asking is how a header finds out whether
   it may use them. */
#if !__has_builtin(__is_target_arch) || !__has_builtin(__is_target_vendor)
#error "__is_target_arch and __is_target_vendor should be builtins"
#endif
#if !__has_builtin(__is_target_os) || !__has_builtin(__is_target_environment)
#error "__is_target_os and __is_target_environment should be builtins"
#endif
/* __has_include is not a builtin -- to clang either. It is asked about
   with `defined`, and answering yes to both questions would tell a header
   something no other compiler tells it. */
#if __has_builtin(__has_include) || __has_builtin(__has_builtin)
#error "__has_include and __has_builtin are features, not builtins"
#endif
#if !defined(__has_include)
#error "__has_include is asked about with defined"
#endif

/* A name that is not a builtin says so, and is not an error. */
#if __has_builtin(__totally_not_a_builtin_at_all)
#error "__has_builtin should answer no for a name it does not know"
#endif

/* Exactly one architecture is the target's, whichever it is, and the
   spellings that name one thing agree. */
#if __is_target_arch(aarch64)
#define ARCH_COUNT 1
#if !__is_target_arch(arm64)
#error "arm64 and aarch64 name one architecture"
#endif
#elif __is_target_arch(x86_64)
#define ARCH_COUNT 1
#if !__is_target_arch(amd64)
#error "amd64 and x86_64 name one architecture"
#endif
#elif __is_target_arch(i386)
#define ARCH_COUNT 1
#else
#define ARCH_COUNT 0
#endif

/* An architecture that is not the target's is no. */
#if __is_target_arch(powerpc) || __is_target_arch(sparc)
#define WRONG_ARCH 1
#else
#define WRONG_ARCH 0
#endif

/* The OS half, and the spellings Darwin is known by. */
#if defined(__APPLE__)
#if !__is_target_os(macos) || !__is_target_os(macosx) || !__is_target_os(darwin)
#error "macos, macosx and darwin name one OS"
#endif
#if !__is_target_vendor(apple)
#error "an Apple target's vendor is apple"
#endif
/* Not a phone, and not Catalyst or a simulator. */
#if __is_target_os(ios) || __is_target_environment(macabi) || __is_target_environment(simulator)
#error "a plain macOS target is none of these"
#endif
#endif

#if defined(__linux__)
#if !__is_target_os(linux)
#error "a Linux target's OS is linux"
#endif
#endif

/* An operator that macro expansion produced is still an operator, which is
   how the C library actually calls one: <secure/_string.h> defines
   __supports_builtin as __has_builtin(builtin) and every string function
   is guarded through it. */
#define supports_builtin(builtin, gcc_major, gcc_minor) __has_builtin(builtin)
#if !supports_builtin(__builtin_expect, 0, 0)
#error "an operator a macro produced is still an operator"
#endif
#if supports_builtin(__not_a_builtin_either, 0, 0)
#error "and still answers no where the answer is no"
#endif

int main(void) {
    if (ARCH_COUNT != 1) return 1;
    if (WRONG_ARCH != 0) return 2;

    /* The operators compose with ordinary #if arithmetic, which is the
       shape TargetConditionals.h actually uses them in. */
#if (__is_target_arch(aarch64) || __is_target_arch(x86_64) || __is_target_arch(i386)) \
    && !__is_target_arch(mips)
    int composed = 1;
#else
    int composed = 0;
#endif
    if (!composed) return 3;

    printf("__is_target OK\n");
    return 0;
}
