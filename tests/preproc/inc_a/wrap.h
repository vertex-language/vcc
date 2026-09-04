/* The first wrap.h on the search path: it defines its own thing and then
   reaches the one it shadows, which is what #include_next is for and what
   no ISO spelling does. */
#ifndef WRAP_A
#define WRAP_A 1
#include_next <wrap.h>
#endif
