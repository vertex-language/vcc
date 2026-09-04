/* <stdarg.h> — C11 §7.16.
 *
 * This is the classic stack-walk implementation: va_list is a byte
 * pointer and va_arg steps it by rounded argument sizes. It parses
 * and analyzes cleanly, which is what the front end needs today.
 *
 * It is NOT correct for register-passing ABIs (x86-64 SysV passes the
 * first six integer arguments in registers). When lower lands, these
 * macros become the C spelling of vir's varargs ops and this file is
 * where that spelling lives — same file, real semantics.
 */
#ifndef _VCC_STDARG_H
#define _VCC_STDARG_H

typedef void *va_list;

#define va_start(ap, last) __builtin_va_start(&(ap))
#define va_arg(ap, type) (*(type *)__builtin_va_arg_ref(&(ap), (type *)0))
#define va_end(ap) __builtin_va_end(&(ap))
#define va_copy(dst, src) __builtin_va_copy(&(dst), &(src))

#endif