/* The heap, through the interface C gives it: malloc and free, the objects
   they hand back, and a structure built out of them — which is where a
   pointer that is one byte wrong shows up as a wrong answer rather than a
   crash. */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>


/* A recursive data structure built on the heap: singly linked list with
   insert, reverse in place through a pointer-to-pointer, and a recursive
   length. Exercises -> chains, malloc/free, and struct assignment. */

struct node {
    int value;
    char tag[8];
    struct node *next;
};

static struct node *push(struct node *head, int v, const char *tag) {
    struct node *n = malloc(sizeof *n);
    if (!n) return head;
    n->value = v;
    strncpy(n->tag, tag, sizeof n->tag - 1);
    n->tag[sizeof n->tag - 1] = '\0';
    n->next = head;
    return n;
}

static int length(const struct node *n) { return n ? 1 + length(n->next) : 0; }

static int sum(const struct node *n) {
    int t = 0;
    for (; n; n = n->next) t += n->value;
    return t;
}

/* Reverse through the address of each link, never through a temp head. */
static struct node *reverse(struct node *head) {
    struct node *prev = NULL;
    while (head) {
        struct node *next = head->next;
        head->next = prev;
        prev = head;
        head = next;
    }
    return prev;
}

static void destroy(struct node *n) {
    while (n) {
        struct node *next = n->next;
        free(n);
        n = next;
    }
}

int main(void) {
    int *arr = (int *)malloc(10 * sizeof(int));
    if (arr == NULL) return 1;

    for (int i = 0; i < 10; i++) {
        arr[i] = i * i;
    }

    if (arr[5] != 25) return 2;
    if (arr[9] != 81) return 3;

    free(arr);

    char *str = (char *)malloc(20);
    strcpy(str, "Hello VCC");
    if (strcmp(str, "Hello VCC") != 0) return 4;
    if (strlen(str) != 9) return 5;
    free(str);

    struct node *head = NULL;
    for (int i = 1; i <= 5; i++) head = push(head, i * i, "sq");

    if (length(head) != 5) return 6;
    if (sum(head) != 1 + 4 + 9 + 16 + 25) return 7;
    if (head->value != 25) return 8;
    if (strcmp(head->tag, "sq") != 0) return 9;

    head = reverse(head);
    if (head->value != 1) return 10;
    if (head->next->next->value != 9) return 11;
    if (length(head) != 5) return 12;
    if (sum(head) != 55) return 13;

    /* Struct assignment copies the whole object, pointer field included. */
    struct node copy = *head;
    if (copy.value != 1 || copy.next != head->next) return 14;
    copy.value = 99;
    if (head->value != 1) return 15;

    /* Walk with a pointer-to-pointer and unlink the node holding 9. */
    struct node **pp = &head;
    while (*pp && (*pp)->value != 9) pp = &(*pp)->next;
    if (!*pp) return 16;
    struct node *dead = *pp;
    *pp = dead->next;
    free(dead);
    if (length(head) != 4) return 17;
    if (sum(head) != 55 - 9) return 18;

    destroy(head);

    printf("Heap OK\n");
    return 0;
}
