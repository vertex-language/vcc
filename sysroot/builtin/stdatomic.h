/* <stdatomic.h> — C11 §7.17.
 *
 * Most of this header is ordinary C over the _Atomic operators, because
 * that is what §7.17 says the generic functions mean: atomic_load(obj) is
 * a read of *obj, atomic_store(obj, v) is a write. Only three kinds of
 * operation cannot be written that way — an exchange, a compare-and-
 * exchange, and the fetch family — because each has to name the value the
 * object held *before* it changed, and no C expression does. Those go
 * through builtins.
 *
 * Orderings are accepted and strengthened to sequentially consistent.
 * That is a conforming implementation of every one of them: a stronger
 * ordering forbids executions a weaker one allows and permits none that
 * it does not, so a program written against memory_order_relaxed still
 * behaves as it was written. It costs speed, never correctness, and the
 * per-ordering instruction selection is the optimization this leaves for
 * later rather than a promise it breaks.
 *
 * atomic_is_lock_free and the ATOMIC_*_LOCK_FREE macros answer 2 —
 * always lock-free — for every type in this header. vcc lowers _Atomic
 * scalars to the machine's own atomic instructions and refuses the
 * aggregate case outright, so there is no type here that is sometimes
 * lock-free and no lock to fall back to.
 */
#ifndef _VCC_STDATOMIC_H
#define _VCC_STDATOMIC_H

#include <stddef.h>
#include <stdint.h>

/* ---- §7.17.1 orderings ---- */

typedef enum memory_order {
    memory_order_relaxed,
    memory_order_consume,
    memory_order_acquire,
    memory_order_release,
    memory_order_acq_rel,
    memory_order_seq_cst
} memory_order;

/* ---- §7.17.3 lock-free property ---- */

#define ATOMIC_BOOL_LOCK_FREE     2
#define ATOMIC_CHAR_LOCK_FREE     2
#define ATOMIC_CHAR16_T_LOCK_FREE 2
#define ATOMIC_CHAR32_T_LOCK_FREE 2
#define ATOMIC_WCHAR_T_LOCK_FREE  2
#define ATOMIC_SHORT_LOCK_FREE    2
#define ATOMIC_INT_LOCK_FREE      2
#define ATOMIC_LONG_LOCK_FREE     2
#define ATOMIC_LLONG_LOCK_FREE    2
#define ATOMIC_POINTER_LOCK_FREE  2

/* §7.17.2.1: initialization is not an atomic operation, and the object is
   not yet shared when it happens. */
#define ATOMIC_VAR_INIT(value) (value)
#define atomic_init(obj, value) ((void)(*(obj) = (value)))

/* §7.17.3.1: the dependency ordering memory_order_consume would carry is
   subsumed by the stronger ordering used throughout, so this is the
   identity it is allowed to be. */
#define kill_dependency(y) (y)

/* ---- §7.17.4 fences ---- */

#define atomic_thread_fence(order) ((void)(order), __builtin_atomic_fence())
#define atomic_signal_fence(order) ((void)(order), __builtin_atomic_signal_fence())

/* ---- §7.17.6 atomic integer types ---- */

typedef _Atomic _Bool              atomic_bool;
typedef _Atomic char               atomic_char;
typedef _Atomic signed char        atomic_schar;
typedef _Atomic unsigned char      atomic_uchar;
typedef _Atomic short              atomic_short;
typedef _Atomic unsigned short     atomic_ushort;
typedef _Atomic int                atomic_int;
typedef _Atomic unsigned int       atomic_uint;
typedef _Atomic long               atomic_long;
typedef _Atomic unsigned long      atomic_ulong;
typedef _Atomic long long          atomic_llong;
typedef _Atomic unsigned long long atomic_ullong;

typedef _Atomic int_least16_t  atomic_int_least16_t;
typedef _Atomic uint_least16_t atomic_uint_least16_t;
typedef _Atomic int_least32_t  atomic_int_least32_t;
typedef _Atomic uint_least32_t atomic_uint_least32_t;
typedef _Atomic int_least64_t  atomic_int_least64_t;
typedef _Atomic uint_least64_t atomic_uint_least64_t;
typedef _Atomic int_fast16_t   atomic_int_fast16_t;
typedef _Atomic uint_fast16_t  atomic_uint_fast16_t;
typedef _Atomic int_fast32_t   atomic_int_fast32_t;
typedef _Atomic uint_fast32_t  atomic_uint_fast32_t;
typedef _Atomic int_fast64_t   atomic_int_fast64_t;
typedef _Atomic uint_fast64_t  atomic_uint_fast64_t;
typedef _Atomic intptr_t       atomic_intptr_t;
typedef _Atomic uintptr_t      atomic_uintptr_t;
typedef _Atomic size_t         atomic_size_t;
typedef _Atomic ptrdiff_t      atomic_ptrdiff_t;
typedef _Atomic intmax_t       atomic_intmax_t;
typedef _Atomic uintmax_t      atomic_uintmax_t;

/* ---- §7.17.5, §7.17.7 operations on atomic types ---- */

#define atomic_is_lock_free(obj) ((void)(obj), 1)

#define atomic_store(obj, desired)  ((void)(*(obj) = (desired)))
#define atomic_load(obj)            (*(obj))

#define atomic_exchange(obj, desired) __builtin_atomic_exchange((obj), (desired))

/* §7.17.7.4: on failure the object's actual value is written through
   expected, so a loop can retry without reading it again. The strong and
   weak forms are the same here — this one never fails spuriously, which
   the weak form permits but does not require. */
#define atomic_compare_exchange_strong(obj, expected, desired) \
    __builtin_atomic_compare_exchange((obj), (expected), (desired))
#define atomic_compare_exchange_weak(obj, expected, desired) \
    __builtin_atomic_compare_exchange((obj), (expected), (desired))

#define atomic_fetch_add(obj, arg) __builtin_atomic_fetch_add((obj), (arg))
#define atomic_fetch_sub(obj, arg) __builtin_atomic_fetch_sub((obj), (arg))
#define atomic_fetch_or(obj, arg)  __builtin_atomic_fetch_or((obj), (arg))
#define atomic_fetch_xor(obj, arg) __builtin_atomic_fetch_xor((obj), (arg))
#define atomic_fetch_and(obj, arg) __builtin_atomic_fetch_and((obj), (arg))

/* The _explicit forms name an ordering this implementation strengthens.
   The order argument is still evaluated: it is an expression the program
   wrote, and dropping it would drop its side effects. */
#define atomic_store_explicit(obj, desired, order) \
    ((void)(order), atomic_store((obj), (desired)))
#define atomic_load_explicit(obj, order) \
    ((void)(order), atomic_load(obj))
#define atomic_exchange_explicit(obj, desired, order) \
    ((void)(order), atomic_exchange((obj), (desired)))
#define atomic_compare_exchange_strong_explicit(obj, expected, desired, succ, fail) \
    ((void)(succ), (void)(fail), atomic_compare_exchange_strong((obj), (expected), (desired)))
#define atomic_compare_exchange_weak_explicit(obj, expected, desired, succ, fail) \
    ((void)(succ), (void)(fail), atomic_compare_exchange_weak((obj), (expected), (desired)))
#define atomic_fetch_add_explicit(obj, arg, order) \
    ((void)(order), atomic_fetch_add((obj), (arg)))
#define atomic_fetch_sub_explicit(obj, arg, order) \
    ((void)(order), atomic_fetch_sub((obj), (arg)))
#define atomic_fetch_or_explicit(obj, arg, order) \
    ((void)(order), atomic_fetch_or((obj), (arg)))
#define atomic_fetch_xor_explicit(obj, arg, order) \
    ((void)(order), atomic_fetch_xor((obj), (arg)))
#define atomic_fetch_and_explicit(obj, arg, order) \
    ((void)(order), atomic_fetch_and((obj), (arg)))

/* ---- §7.17.8 atomic flag ---- */

typedef struct atomic_flag {
    atomic_bool _Value;
} atomic_flag;

#define ATOMIC_FLAG_INIT {0}

#define atomic_flag_test_and_set(obj) \
    ((_Bool)__builtin_atomic_exchange(&(obj)->_Value, 1))
#define atomic_flag_clear(obj) \
    ((void)(*&(obj)->_Value = 0))
#define atomic_flag_test_and_set_explicit(obj, order) \
    ((void)(order), atomic_flag_test_and_set(obj))
#define atomic_flag_clear_explicit(obj, order) \
    ((void)(order), atomic_flag_clear(obj))

#endif
