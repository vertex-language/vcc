#include <stdio.h>
#include "shared.h"

/* Both units include <stdio.h>, which is the point: a system header defines
   functions as well as declaring them — Darwin's spells __sputc as an inline
   definition every unit that includes it emits — and two of those must link.

   local_only is deliberately the same name as a.c's, with a different body:
   a static function is that unit's own, and neither the linker nor the other
   unit may see it. */

int counter = 0;

static int local_only(int x) { return x * 7; }

int b_local(int x) { return local_only(x); }

static const char text[] = "vcc";
const char *const label = text;

extern int twice(int x);   /* the external definition of the inline above */

int add(int a, int b) { return a + b; }

struct point scale(struct point p, int k) {
    p.x *= k;
    p.y *= k;
    return p;
}

int bump(void) { return ++counter; }
