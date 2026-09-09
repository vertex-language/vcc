/* vcc-flags: darwin: -l objc -framework Foundation */
#include <stdio.h>

/* -framework, which is Darwin's and has no -l spelling: a framework is a
   directory holding its library under the framework's own name, so
   Foundation.framework/Foundation.tbd is what a link reads and
   libFoundation.tbd has never existed.

   Everything below is the Objective-C runtime's C API, which is all a
   framework needs to be useful without a compiler that speaks the
   language. */

#if defined(__APPLE__)
typedef void *id;
typedef void *Class;
typedef void *SEL;

extern Class objc_getClass(const char *name);
extern SEL sel_registerName(const char *name);
extern id objc_msgSend(id self, SEL op, ...);

/* Each call casts objc_msgSend to the signature of the method being sent.
   On arm64 a variadic call and a direct one do not put their arguments in
   the same registers, so the cast is not decoration. */
typedef id (*msg_t)(id, SEL);
typedef long (*msg_long_t)(id, SEL);
typedef id (*msg_str_t)(id, SEL, const char *);
#endif

int main(void) {
#if defined(__APPLE__)
    /* NSObject is in libobjc, so -l objc alone would find it. */
    Class object = objc_getClass("NSObject");
    if (!object) return 1;

    /* NSString is in Foundation, so this one is the framework's. Before
       -framework existed there was no way to link it at all. */
    Class string = objc_getClass("NSString");
    if (!string) return 2;

    id empty = ((msg_t)objc_msgSend)((id)string, sel_registerName("string"));
    if (!empty) return 3;
    if (((msg_long_t)objc_msgSend)(empty, sel_registerName("length")) != 0) return 4;

    id five = ((msg_str_t)objc_msgSend)(
        (id)string, sel_registerName("stringWithUTF8String:"), "hello");
    if (!five) return 5;
    if (((msg_long_t)objc_msgSend)(five, sel_registerName("length")) != 5) return 6;
#endif
    printf("Framework OK\n");
    return 0;
}
