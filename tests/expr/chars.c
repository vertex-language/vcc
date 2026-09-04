#include <stdio.h>
#include <string.h>
#include <limits.h>
#include <wchar.h>

/* Character and string literals: every escape §6.4.4.4 lists, adjacent
   literal concatenation, embedded NULs, the wide and UTF forms, and the
   fact that a string literal is an array with a terminator. */

static const char esc[] = "\a\b\f\n\r\t\v\\\'\"\?";
static const char oct[] = "\0\1\77\101\177";
static const char hex[] = "\x00\x01\x41\x7f";
static const char adj[] = "one" " " "two";

int main(void) {
    if (esc[0] != 7 || esc[1] != 8 || esc[2] != 12) return 1;
    if (esc[3] != 10 || esc[4] != 13 || esc[5] != 9 || esc[6] != 11) return 2;
    if (esc[7] != '\\' || esc[8] != '\'' || esc[9] != '"' || esc[10] != '?') return 3;
    if (sizeof esc != 12) return 4;

    if (oct[0] != 0 || oct[1] != 1 || oct[2] != 077 || oct[3] != 'A' || oct[4] != 0177) return 5;
    if (sizeof oct != 6) return 6;
    if (hex[0] != 0 || hex[1] != 1 || hex[2] != 'A' || hex[3] != 0x7f) return 7;

    if (strcmp(adj, "one two") != 0) return 8;
    if (sizeof adj != 8) return 9;

    /* A string literal is an array; strlen stops at the first NUL but
       sizeof does not. */
    const char embedded[] = "ab\0cd";
    if (sizeof embedded != 6) return 10;
    if (strlen(embedded) != 2) return 11;
    if (embedded[3] != 'c') return 12;

    /* A char array initialized from a literal of exactly its length gets
       no terminator. */
    const char exact[3] = "abc";
    if (exact[0] != 'a' || exact[2] != 'c') return 13;
    if (sizeof exact != 3) return 14;

    /* Character constants have type int, and an escape is one character. */
    if (sizeof('a') != sizeof(int)) return 15;
    if ('\n' != 10) return 16;
    if ('\0' != 0) return 17;
    if ('\x41' != 'A') return 18;

    /* Plain char's signedness is implementation-defined; whichever it is,
       the value round-trips through the matching type. */
    char c = (char)0xFF;
    if (CHAR_MIN < 0) {
        if (c != -1) return 19;
    } else {
        if (c != 255) return 20;
    }
    if ((unsigned char)c != 255) return 21;

    /* Wide and UTF literals have the widths their types promise. */
    if (sizeof(L'a') != sizeof(wchar_t)) return 22;
    const wchar_t *w = L"wide";
    if (w[0] != L'w' || w[4] != 0) return 23;
    if (sizeof L"wide" != 5 * sizeof(wchar_t)) return 24;

    const char *u8 = u8"hé";      /* e-acute is two UTF-8 bytes */
    if (strlen(u8) != 3) return 25;
    if ((unsigned char)u8[1] != 0xC3 || (unsigned char)u8[2] != 0xA9) return 26;

    if (sizeof(u'x') != 2) return 27;
    if (sizeof(U'x') != 4) return 28;
    /* <uchar.h> is not on every hosted platform, so the literals are
       indexed directly rather than through a char16_t declaration. */
    if (u"ab"[0] != 'a' || u"ab"[2] != 0) return 29;
    if (U"ab"[1] != 'b' || U"ab"[2] != 0) return 30;
    if (sizeof u"ab" != 3 * 2) return 31;
    if (sizeof U"ab" != 3 * 4) return 32;

    /* String functions over what was built above. */
    char buf[16];
    strcpy(buf, "abc");
    strcat(buf, "def");
    if (strcmp(buf, "abcdef") != 0) return 33;
    if (strncmp(buf, "abcXXX", 3) != 0) return 34;
    if (strchr(buf, 'd') != buf + 3) return 35;
    if (strrchr(buf, 'a') != buf) return 36;
    if (memchr(buf, 'c', 6) != buf + 2) return 37;
    memset(buf, '=', 4);
    if (buf[0] != '=' || buf[3] != '=' || buf[4] != 'e') return 38;

    printf("Chars OK\n");
    return 0;
}
