/* <stdint.h> — C11 §7.20. Exact-width types are selected by the
 * target model's __SIZEOF_*__ predefines, so one text serves every
 * Model. The #error arms mark model shapes no current target has;
 * a target that hits one teaches this file a new case, on evidence,
 * the same way the parser's tolerated list grows.
 */
#ifndef _VCC_STDINT_H
#define _VCC_STDINT_H

#if __CHAR_BIT__ != 8
#error "<stdint.h> expects an 8-bit byte; no current target model has another"
#endif

typedef signed char        int8_t;
typedef unsigned char      uint8_t;

#if __SIZEOF_SHORT__ == 2
typedef short              int16_t;
typedef unsigned short     uint16_t;
#else
#error "<stdint.h>: no 16-bit type in this target model"
#endif

#if __SIZEOF_INT__ == 4
typedef int                int32_t;
typedef unsigned int       uint32_t;
#elif __SIZEOF_LONG__ == 4
typedef long               int32_t;
typedef unsigned long      uint32_t;
#else
#error "<stdint.h>: no 32-bit type in this target model"
#endif

#if __SIZEOF_LONG__ == 8
typedef long               int64_t;
typedef unsigned long      uint64_t;
#elif __SIZEOF_LONG_LONG__ == 8
typedef long long          int64_t;
typedef unsigned long long uint64_t;
#else
#error "<stdint.h>: no 64-bit type in this target model"
#endif

/* Least and fast alias exact. Fast is implementation-defined and the
 * exact type is a conforming, predictable choice. */
typedef int8_t   int_least8_t;   typedef uint8_t   uint_least8_t;
typedef int16_t  int_least16_t;  typedef uint16_t  uint_least16_t;
typedef int32_t  int_least32_t;  typedef uint32_t  uint_least32_t;
typedef int64_t  int_least64_t;  typedef uint64_t  uint_least64_t;
typedef int8_t   int_fast8_t;    typedef uint8_t   uint_fast8_t;
typedef int16_t  int_fast16_t;   typedef uint16_t  uint_fast16_t;
typedef int32_t  int_fast32_t;   typedef uint32_t  uint_fast32_t;
typedef int64_t  int_fast64_t;   typedef uint64_t  uint_fast64_t;

#if __SIZEOF_POINTER__ == 8
typedef int64_t  intptr_t;       typedef uint64_t  uintptr_t;
#define INTPTR_MAX  INT64_MAX
#define INTPTR_MIN  INT64_MIN
#define UINTPTR_MAX UINT64_MAX
#elif __SIZEOF_POINTER__ == 4
typedef int32_t  intptr_t;       typedef uint32_t  uintptr_t;
#define INTPTR_MAX  INT32_MAX
#define INTPTR_MIN  INT32_MIN
#define UINTPTR_MAX UINT32_MAX
#else
#error "<stdint.h>: unexpected pointer size in this target model"
#endif

typedef __INTMAX_TYPE__  intmax_t;
typedef __UINTMAX_TYPE__ uintmax_t;

#define INT8_MAX   127
#define INT8_MIN   (-128)
#define UINT8_MAX  255
#define INT16_MAX  32767
#define INT16_MIN  (-32768)
#define UINT16_MAX 65535
#define INT32_MAX  2147483647
#define INT32_MIN  (-2147483647 - 1)
#define UINT32_MAX 4294967295U
#define INT64_MAX  9223372036854775807LL
#define INT64_MIN  (-9223372036854775807LL - 1)
#define UINT64_MAX 18446744073709551615ULL

#define INT_LEAST8_MAX   INT8_MAX
#define INT_LEAST8_MIN   INT8_MIN
#define UINT_LEAST8_MAX  UINT8_MAX
#define INT_LEAST16_MAX  INT16_MAX
#define INT_LEAST16_MIN  INT16_MIN
#define UINT_LEAST16_MAX UINT16_MAX
#define INT_LEAST32_MAX  INT32_MAX
#define INT_LEAST32_MIN  INT32_MIN
#define UINT_LEAST32_MAX UINT32_MAX
#define INT_LEAST64_MAX  INT64_MAX
#define INT_LEAST64_MIN  INT64_MIN
#define UINT_LEAST64_MAX UINT64_MAX

#define INT_FAST8_MAX   INT8_MAX
#define INT_FAST8_MIN   INT8_MIN
#define UINT_FAST8_MAX  UINT8_MAX
#define INT_FAST16_MAX  INT16_MAX
#define INT_FAST16_MIN  INT16_MIN
#define UINT_FAST16_MAX UINT16_MAX
#define INT_FAST32_MAX  INT32_MAX
#define INT_FAST32_MIN  INT32_MIN
#define UINT_FAST32_MAX UINT32_MAX
#define INT_FAST64_MAX  INT64_MAX
#define INT_FAST64_MIN  INT64_MIN
#define UINT_FAST64_MAX UINT64_MAX

#define INTMAX_MAX  __INTMAX_MAX__
#define INTMAX_MIN  (-INTMAX_MAX - 1)
#define UINTMAX_MAX (INTMAX_MAX * 2ULL + 1ULL)

#define SIZE_MAX    __SIZE_MAX__
#define PTRDIFF_MAX __PTRDIFF_MAX__
#define PTRDIFF_MIN (-PTRDIFF_MAX - 1)

#define WCHAR_MAX __WCHAR_MAX__
#define WCHAR_MIN __WCHAR_MIN__
#define WINT_MAX  __WINT_MAX__
#define WINT_MIN  __WINT_MIN__

/* sig_atomic_t is int in every target model. */
#define SIG_ATOMIC_MAX __INT_MAX__
#define SIG_ATOMIC_MIN (-__INT_MAX__ - 1)

#define INT8_C(v)    v
#define INT16_C(v)   v
#define INT32_C(v)   v
#define INT64_C(v)   v ## LL
#define UINT8_C(v)   v
#define UINT16_C(v)  v
#define UINT32_C(v)  v ## U
#define UINT64_C(v)  v ## ULL
#define INTMAX_C(v)  v ## LL
#define UINTMAX_C(v) v ## ULL

#endif