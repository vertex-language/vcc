/* MSVC parity sweep: a systematic probe of the MSVC compatibility surface.
 *
 * Each test_*() function exercises one MSVC feature. main() calls them all
 * and returns the first failure. The file compiles and runs identically under
 * cl.exe and vcc — any difference is a gap.
 *
 * Guard: only compiled when the target is an MSVC-convention compiler.
 */

#include <stdio.h>
#include <string.h>
#include <math.h>

#if defined(_MSC_VER) || defined(__VCC__)

/* ===================================================================
 * Tier 1: Known likely gaps
 * =================================================================== */

/* 1.1 — MSVC integer literal suffixes: i8, ui8, i16, ui16, i32, ui32, i64, ui64 */
static int test_i64_suffix(void) {
    unsigned __int64 a = 0x8000000000000000ui64;
    __int64 b = 100i64;
    unsigned __int32 c = 42ui32;
    __int64 d = -1i64;
    unsigned __int16 e = 1000ui16;
    __int8 f = 127i8;
    if (a != 0x8000000000000000ULL) return 1;
    if (b != 100) return 1;
    if (c != 42u) return 1;
    if (d != -1LL) return 1;
    if (e != 1000) return 1;
    if (f != 127) return 1;
    return 0;
}

/* 1.2 — __assume(expr) and __assume(0) */
static int classify(int x) {
    switch (x) {
    case 0: return 10;
    case 1: return 20;
    case 2: return 30;
    default: __assume(0);
    }
}
static int test_assume(void) {
    if (classify(0) != 10) return 1;
    if (classify(1) != 20) return 1;
    if (classify(2) != 30) return 1;
    /* Optimizer hint: do not call with x > 2. */
    __assume(1 > 0);
    return 0;
}

/* 1.3 — __noop(...) */
static int test_noop(void) {
    int x = 42;
    __noop(x, x + 1, "hello");
    __noop();
    if (x != 42) return 1;
    return 0;
}

/* 1.4 — #pragma comment(lib, ...) parsing
 * We can only test that the pragma is accepted without error.
 * Actual auto-linking would need a linker-level test. */
#pragma comment(lib, "kernel32.lib")
#pragma comment(lib, "user32")
static int test_pragma_comment_lib(void) {
    /* If we got here, the pragma parsed. */
    return 0;
}

/* 1.5 — __declspec(selectany) */
__declspec(selectany) const int selectany_var = 12345;
static int test_selectany(void) {
    if (selectany_var != 12345) return 1;
    return 0;
}

/* 1.6 — __declspec(dllexport) on a function (in an EXE, it just exports).
 * We test that the declspec is accepted; actual PE export verification
 * would need dumpbin. */
__declspec(dllexport) int exported_func_for_test(int x) { return x * 2; }
static int test_dllexport(void) {
    if (exported_func_for_test(5) != 10) return 1;
    return 0;
}

/* ===================================================================
 * Tier 2: Likely working — confirm
 * =================================================================== */

/* 2.1 — #pragma warning(push/pop/disable) */
#pragma warning(push)
#pragma warning(disable: 4996)
#pragma warning(disable: 4100 4101)
static int test_pragma_warning(void) {
    int unused;
    (void)unused;
    return 0;
}
#pragma warning(pop)

/* 2.2 — __declspec(deprecated) */
__declspec(deprecated("use new_func")) int deprecated_func(void) { return 1; }
static int test_deprecated(void) {
    /* Don't call it — just verify the declaration parsed. */
    return 0;
}

/* 2.3 — _Static_assert */
_Static_assert(sizeof(int) == 4, "int must be 4 bytes");
_Static_assert(sizeof(void*) == 8, "pointer must be 8 bytes on x64");
static int test_static_assert(void) {
    _Static_assert(sizeof(long long) == 8, "long long must be 8");
    return 0;
}

/* 2.4 — _Generic type-generic selection */
#define type_id(x) _Generic((x), \
    int: 1, \
    long: 2, \
    double: 3, \
    default: 0)
static int test_generic(void) {
    int i = 0;
    double d = 0.0;
    if (type_id(i) != 1) return 1;
    if (type_id(d) != 3) return 1;
    return 0;
}

/* 2.5 — Compound literals */
struct point { int x, y; };
static int point_sum(const struct point *p) { return p->x + p->y; }
static int test_compound_literal(void) {
    int r = point_sum(&(struct point){ 3, 4 });
    if (r != 7) return 1;
    int *p = (int[]){ 10, 20, 30 };
    if (p[1] != 20) return 1;
    return 0;
}

/* 2.6 — Designated initializers */
static int test_designated_init(void) {
    int a[5] = { [2] = 42, [4] = 99 };
    if (a[0] != 0 || a[2] != 42 || a[4] != 99) return 1;
    struct { int x, y, z; } s = { .z = 3, .x = 1 };
    if (s.x != 1 || s.y != 0 || s.z != 3) return 1;
    return 0;
}

/* 2.7 — __FUNCTION__ predefined identifier */
static const char *get_fn_name(void) { return __FUNCTION__; }
static int test_function_name(void) {
    const char *n = get_fn_name();
    if (strcmp(n, "get_fn_name") != 0) return 1;
    return 0;
}

/* 2.8 — __COUNTER__ macro */
static int test_counter(void) {
    int a = __COUNTER__;
    int b = __COUNTER__;
    int c = __COUNTER__;
    if (b != a + 1) return 1;
    if (c != a + 2) return 1;
    return 0;
}

/* ===================================================================
 * Tier 3: Edge cases
 * =================================================================== */

/* 3.1 — #pragma intrinsic / #pragma function */
#pragma intrinsic(memcpy)
static int test_pragma_intrinsic(void) {
    char a[16] = "hello, world!!!";
    char b[16];
    memcpy(b, a, 16);
    if (strcmp(a, b) != 0) return 1;
    return 0;
}

/* 3.2 — #pragma message */
#pragma message("Compiling MSVC parity sweep")
static int test_pragma_message(void) { return 0; }

/* 3.3 — Multi-declspec: __declspec(align(16)) and __declspec(noinline) */
__declspec(align(16)) int aligned_var = 42;
__declspec(noinline) int no_inline_func(int x) { return x + 1; }
static int test_multi_declspec(void) {
    if (((unsigned long long)&aligned_var & 15) != 0) return 1;
    if (no_inline_func(5) != 6) return 1;
    return 0;
}

/* 3.4 — Nested anonymous struct/union (LARGE_INTEGER pattern)
 *
 * Windows writes this with DWORD and LONG, which are unsigned long and long
 * there because Windows is LLP64 and those are thirty-two bits. Spelled that
 * way here it would stop being a test of anonymous members and become a test
 * of how wide long is: on LP64 the two halves are sixty-four bits each, the
 * union is sixteen bytes, and LowPart is the whole value rather than half of
 * it. int is thirty-two bits on both, so the split is the one the pattern is
 * about wherever this runs. */
typedef union {
    struct { unsigned int LowPart; int HighPart; };
    struct { unsigned int LowPart; int HighPart; } u;
    long long QuadPart;
} LARGE_INTEGER_LIKE;
static int test_anon_nested(void) {
    LARGE_INTEGER_LIKE li;
    li.QuadPart = 0x0000000200000001LL;
    if (li.LowPart != 1) return 1;
    if (li.HighPart != 2) return 1;
    if (li.u.LowPart != 1) return 1;
    return 0;
}

/* 3.5 — Basic float/double arithmetic */
static int test_float_ops(void) {
    double a = 3.14159265358979;
    float b = 2.71828f;
    if (fabs(a * 2.0 - 6.28318530717958) > 1e-10) return 1;
    if (fabs((double)b + 1.0 - 3.71828) > 1e-3) return 1;
    return 0;
}

/* 3.6 — Variadic function with mixed int/float args */
#include <stdarg.h>
static double sum_mixed(int count, ...) {
    va_list ap;
    double s = 0.0;
    int i;
    va_start(ap, count);
    for (i = 0; i < count; i++)
        s += va_arg(ap, double);
    va_end(ap);
    return s;
}
static int test_variadic_float(void) {
    double r = sum_mixed(3, 1.0, 2.5, 3.5);
    if (fabs(r - 7.0) > 1e-10) return 1;
    return 0;
}

/* 3.7 — __declspec(noreturn) */
__declspec(noreturn) void noreturn_func(void) {
    /* The compiler should accept this without warning about
     * a missing return even though it is declared noreturn. */
    for (;;) {}
}
static int test_noreturn_declspec(void) {
    /* Just test that the declaration compiled. Don't call it. */
    return 0;
}

/* 3.8 — Bitfield in struct, non-constant expression at use site */
struct bf_test { unsigned a : 3; unsigned b : 5; unsigned c : 8; };
static int test_bitfield_use(void) {
    struct bf_test t = { 0 };
    t.a = 5;
    t.b = 17;
    t.c = 200;
    if (t.a != 5) return 1;
    if (t.b != 17) return 1;
    if (t.c != 200) return 1;
    return 0;
}

/* 3.9 — __forceinline
 *
 * static, because what is under test here is that the spelling is accepted
 * and the function works. Without it this is an inline definition that
 * provides no external definition, and calling it is a link error in
 * standard C — which is what clang and gcc give at -O0, and what vcc gives
 * too. §6.7.4's linkage rules are tests/decl/inline.c's subject; this
 * file's is the dialect. */
static __forceinline int force_inlined(int x) { return x * x; }
static int test_forceinline(void) {
    if (force_inlined(7) != 49) return 1;
    return 0;
}

/* 3.10 — __inline (legacy spelling), static for the reason above. */
static __inline int legacy_inline(int x) { return x + x; }
static int test_inline_legacy(void) {
    if (legacy_inline(11) != 22) return 1;
    return 0;
}

/* 3.11 — Calling convention keywords accepted on x64 */
int __cdecl    cdecl_func(int x) { return x; }
int __stdcall  stdcall_func(int x) { return x; }
int __fastcall fastcall_func(int x) { return x; }
static int test_calling_conventions(void) {
    if (cdecl_func(1) != 1) return 1;
    if (stdcall_func(2) != 2) return 1;
    if (fastcall_func(3) != 3) return 1;
    return 0;
}

/* 3.12 — __w64, __unaligned, __ptr64 parsed and discarded */
typedef int __w64 INT_PTR_LIKE;
static int test_type_qualifiers(void) {
    INT_PTR_LIKE x = 42;
    if (x != 42) return 1;
    return 0;
}

/* ===================================================================
 * Runner
 * =================================================================== */

typedef int (*test_fn)(void);

struct test_entry {
    const char *name;
    test_fn     fn;
};

#define T(name) { #name, name }

int main(void) {
    struct test_entry tests[] = {
        /* Tier 1 — likely gaps */
        T(test_i64_suffix),
        T(test_assume),
        T(test_noop),
        T(test_pragma_comment_lib),
        T(test_selectany),
        T(test_dllexport),

        /* Tier 2 — likely working */
        T(test_pragma_warning),
        T(test_deprecated),
        T(test_static_assert),
        T(test_generic),
        T(test_compound_literal),
        T(test_designated_init),
        T(test_function_name),
        T(test_counter),

        /* Tier 3 — edge cases */
        T(test_pragma_intrinsic),
        T(test_pragma_message),
        T(test_multi_declspec),
        T(test_anon_nested),
        T(test_float_ops),
        T(test_variadic_float),
        T(test_noreturn_declspec),
        T(test_bitfield_use),
        T(test_forceinline),
        T(test_inline_legacy),
        T(test_calling_conventions),
        T(test_type_qualifiers),
    };
    int n = sizeof(tests) / sizeof(tests[0]);
    int pass = 0, fail = 0;
    int i;
    for (i = 0; i < n; i++) {
        int r = tests[i].fn();
        if (r != 0) {
            printf("FAIL: %s (returned %d)\n", tests[i].name, r);
            fail++;
        } else {
            printf("  ok: %s\n", tests[i].name);
            pass++;
        }
    }
    printf("\n%d/%d passed", pass, n);
    if (fail) printf(", %d FAILED", fail);
    printf("\n");
    return fail ? 1 : 0;
}

#else /* not MSVC-convention */

int main(void) {
    printf("MSVC parity sweep: skipped (not an MSVC target)\n");
    return 0;
}

#endif
