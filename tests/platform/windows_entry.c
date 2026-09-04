/* wmain, which is one of the four entry points the Windows CRT provides.
 *
 * Which one an image wants is not on the command line: the program says it
 * by which function it defines, and the linker picks the startup routine and
 * the subsystem to match — mainCRTStartup and console for main, wmain's own
 * for wmain, WinMainCRTStartup and GUI for WinMain, and wWinMain's for that.
 * A linker that always reaches for mainCRTStartup tells a Windows program it
 * has no main, which is a symbol the program never mentioned.
 *
 * wmain is the one this file can check, because a GUI program has no stdout
 * to print its result to and a test that cannot report is not one. The other
 * three are checked where the choice is made, in pe/link. */

#include <stdio.h>

#ifdef _WIN32
#include <windows.h>

int wmain(int argc, wchar_t **argv) {
    /* argv is wide here, and argv[0] is the program's own path — which is
     * what says the wide startup ran rather than the narrow one. */
    if (argc < 1) return 1;
    if (argv == NULL || argv[0] == NULL) return 2;
    if (argv[0][0] == L'\0') return 3;
    if (argv[argc] != NULL) return 4;

    /* A wide character is two bytes here, and the path holds real ones. */
    if (sizeof(argv[0][0]) != 2) return 5;
    {
        int n = 0;
        while (argv[0][n] != L'\0') n++;
        if (n < 2) return 6;
    }

    /* The console CRT is fully initialized: stdio works, and so does the
     * Win32 side. */
    if (GetCurrentProcessId() == 0) return 7;

    printf("wmain OK (%d arg%s)\n", argc, argc == 1 ? "" : "s");
    return 0;
}

#else

int main(void) {
    printf("wmain OK (not this host)\n");
    return 0;
}

#endif
