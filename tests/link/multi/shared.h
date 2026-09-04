#ifndef SHARED_H
#define SHARED_H

struct point { int x, y; };

extern int counter;              /* defined in b.c */
extern const char *const label;  /* defined in b.c */

int add(int a, int b);
int b_local(int x);              /* calls b.c's own static of a shared name */
struct point scale(struct point p, int k);
int bump(void);

/* An inline definition with an external definition elsewhere: §6.7.4p7 says
   the one in b.c is what every call may bind to. */
inline int twice(int x) { return x + x; }

#endif
