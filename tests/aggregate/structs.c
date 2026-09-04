/* Structs: members, nesting, assignment as a whole, and the two things a
   struct is not — an array (it copies) and a bag of bytes (it has holes). */

#include <stdio.h>
#include <string.h>

struct point { int x, y; };

struct rect {
    struct point top_left;
    struct point bottom_right;
};

/* A struct containing a union containing an anonymous struct: three levels
   of member lookup in one declaration. */
struct node {
    int id;
    union {
        int i;
        float f;
        struct { short x, y; } coords;
    } data;
};

struct empty { };            /* a GNU extension: size zero, not one */

static int area(struct rect r) {
    return (r.bottom_right.x - r.top_left.x) * (r.bottom_right.y - r.top_left.y);
}

int main(void) {
    struct rect r;
    r.top_left.x = 0;
    r.top_left.y = 0;
    r.bottom_right.x = 10;
    r.bottom_right.y = 5;
    if (area(r) != 50) return 1;

    struct rect r2 = {{1, 1}, {4, 5}};
    if (area(r2) != 12) return 2;

    /* Assignment copies the whole object, members and padding alike. */
    struct rect r3 = r2;
    r3.top_left.x = 100;
    if (r2.top_left.x != 1) return 3;
    if (r3.bottom_right.y != 5) return 4;

    struct point a = {3, 4}, b;
    b = a;
    if (b.x != 3 || b.y != 4) return 5;
    if (memcmp(&a, &b, sizeof a) != 0) return 6;

    /* Nested member access through a union. */
    struct node n;
    n.id = 1;
    n.data.i = 42;
    if (n.data.i != 42) return 7;
    n.data.f = 3.14f;
    if (n.data.f != 3.14f) return 8;
    n.data.coords.x = 10;
    n.data.coords.y = 20;
    if (n.data.coords.x != 10 || n.data.coords.y != 20) return 9;

    /* A pointer to a struct, and -> as the spelling of (*p).member. */
    struct point *p = &a;
    if (p->x != 3 || (*p).y != 4) return 10;
    p->x = 30;
    if (a.x != 30) return 11;

    /* An array of structs, and the struct's size deciding the stride. */
    struct point pts[3] = {{1, 1}, {2, 2}, {3, 3}};
    if (sizeof pts != 3 * sizeof(struct point)) return 12;
    if (pts[2].x != 3) return 13;
    struct point *q = pts;
    q += 2;
    if (q->y != 3) return 14;

    /* The empty struct is gcc's and clang's, and is zero bytes. */
    struct empty e;
    (void)e;
    if (sizeof(struct empty) != 0) return 15;

    printf("Structs OK\n");
    return 0;
}
