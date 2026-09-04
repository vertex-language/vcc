/* Enumerators outside int.
 *
 * §6.7.2.2p2 requires every enumerator to be representable as an int, and
 * gcc, clang and MSVC all widen the enumeration rather than refuse one that
 * is not. It is not a nicety: the Windows SDK's wingdi.h alone writes
 * thirty-four enumerators past INT_MAX, so a compiler that refuses cannot
 * open <windows.h>. */

#include <limits.h>
#include <stdio.h>

/* Everything fits: int, and signed. */
enum Small { S_LO = INT_MIN, S_HI = INT_MAX };
_Static_assert(sizeof(enum Small) == sizeof(int), "small is int");
_Static_assert(S_LO < 0, "small is signed");

/* Past INT_MAX and never negative: unsigned, and still four bytes. This is
 * DISPLAYCONFIG_OUTPUT_TECHNOLOGY_INTERNAL's shape. */
enum Big { B_ZERO = 0, B_TOP = 0x80000000, B_ALL = 0xFFFFFFFF };
_Static_assert(sizeof(enum Big) == 4, "big is four bytes");
_Static_assert(B_TOP > B_ZERO, "big is unsigned");
_Static_assert(B_ALL > B_TOP, "big keeps its value");

/* Needs both the sign and the range: 64 bits. */
enum Wide { W_NEG = -1, W_BIG = 4294967295 };
_Static_assert(sizeof(enum Wide) == 8, "wide is eight bytes");
_Static_assert(W_NEG < 0, "wide is signed");

int main(void) {
    enum Big b = B_TOP;

    /* The value survives into an object of the type, and reads back
     * unsigned. As an int it would be negative. */
    if (b != 0x80000000) return 1;
    if (b < B_ZERO) return 2;
    if ((unsigned long long)B_ALL != 4294967295ULL) return 3;

    /* An enumerator still counts up from the one before it. */
    if (S_HI != INT_MAX) return 4;

    if (W_NEG >= 0) return 5;
    if (W_BIG != 4294967295LL) return 6;

    printf("Enum range OK\n");
    return 0;
}
