/* The host's own headers and libraries, on Windows.
 *
 * <windows.h> is the header that finds every gap at once: it reaches
 * DriverSpecs.h, which writes a stray backslash into a macro nothing
 * expands; wingdi.h, which writes thirty-four enumerators past INT_MAX;
 * winnt.h, which asserts a layout with an address constant; winioctl.h,
 * which uses __alignof; propidlbase.h, which declares an array parameter
 * over a type completed later; and stralign.h, which defines a static
 * inline wrapper around a symbol no import library carries.
 *
 * Guarded, so the file runs everywhere the rest of the suite does: off
 * Windows it is a program that prints its line and exits zero, which is
 * what the suite asks of it. */

#include <stdio.h>

#ifdef _WIN32
#include <windows.h>

int main(void) {
    DWORD pid = GetCurrentProcessId();
    if (pid == 0) return 1;

    /* An enumerator past INT_MAX, from wingdi.h, compared as the unsigned
     * value it is rather than the negative int it would be. */
    if (DISPLAYCONFIG_OUTPUT_TECHNOLOGY_INTERNAL <= DISPLAYCONFIG_OUTPUT_TECHNOLOGY_OTHER) return 2;

    /* FIELD_OFFSET is an integer constant expression, which is what
     * winnt.h's own C_ASSERT needs of it. */
    {
        typedef char assert_offset_folds[(FIELD_OFFSET(SYSTEM_INFO, dwPageSize) > 0) ? 1 : -1];
        if (sizeof(assert_offset_folds) != 1) return 3;
    }

    SYSTEM_INFO si;
    GetSystemInfo(&si);
    if (si.dwPageSize == 0) return 4;
    if ((char *)&si.dwPageSize - (char *)&si != (ptrdiff_t)FIELD_OFFSET(SYSTEM_INFO, dwPageSize)) return 5;

    /* A page of real memory, through the Win32 allocator: kernel32 is
     * linked, called, and returns something usable. */
    {
        void *p = VirtualAlloc(NULL, si.dwPageSize, MEM_COMMIT | MEM_RESERVE, PAGE_READWRITE);
        if (p == NULL) return 6;
        *(volatile int *)p = 0x5A5A;
        if (*(volatile int *)p != 0x5A5A) return 7;
        if (!VirtualFree(p, 0, MEM_RELEASE)) return 8;
    }

    /* Wide characters are the platform's, and UTF-16, converted by the
     * platform's own call rather than by the C library's. */
    {
        WCHAR buf[16];
        if (sizeof(WCHAR) != 2) return 9;
        if (MultiByteToWideChar(CP_ACP, 0, "42", -1, buf, 16) != 3) return 10;
        if (buf[0] != L'4' || buf[1] != L'2' || buf[2] != L'\0') return 11;
    }

    printf("Windows OK\n");
    return 0;
}

#else

int main(void) {
    printf("Windows OK (not this host)\n");
    return 0;
}

#endif
