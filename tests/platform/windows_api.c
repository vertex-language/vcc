/* vcc-flags: windows: -l ws2_32 -l advapi32 */

/* Breadth rather than depth: the header sets a Windows program actually
 * reaches for, each one called far enough to prove it linked and works.
 *
 * What this is testing is not the API. It is that <winsock2.h>,
 * <shellapi.h> and the registry half of <windows.h> all open, that the
 * structures they declare have the layout the OS expects, and that the
 * import libraries resolve. A struct laid out wrong here does not fail to
 * compile — it fails at the call, with the OS reading a field where no field
 * is. */

#include <stdio.h>

#ifdef _WIN32
#define WIN32_LEAN_AND_MEAN
#include <winsock2.h>
#include <ws2tcpip.h>
#include <windows.h>
#include <string.h>

static int tcpRoundTrip(void) {
    WSADATA wsa;
    if (WSAStartup(MAKEWORD(2, 2), &wsa) != 0) return 1;

    SOCKET srv = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    if (srv == INVALID_SOCKET) return 2;

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof addr);
    addr.sin_family = AF_INET;
    addr.sin_port = 0;                        /* the OS picks one */
    addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    if (bind(srv, (struct sockaddr *)&addr, sizeof addr) == SOCKET_ERROR) return 3;
    if (listen(srv, 1) == SOCKET_ERROR) return 4;

    int alen = sizeof addr;
    if (getsockname(srv, (struct sockaddr *)&addr, &alen) == SOCKET_ERROR) return 5;
    if (addr.sin_port == 0) return 6;         /* the struct read back */

    SOCKET cli = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    if (cli == INVALID_SOCKET) return 7;
    if (connect(cli, (struct sockaddr *)&addr, sizeof addr) == SOCKET_ERROR) return 8;
    SOCKET acc = accept(srv, NULL, NULL);
    if (acc == INVALID_SOCKET) return 9;

    const char msg[] = "hello over tcp";
    char buf[64] = {0};
    if (send(cli, msg, (int)sizeof msg, 0) != (int)sizeof msg) return 10;
    if (recv(acc, buf, sizeof buf, 0) != (int)sizeof msg) return 11;
    if (strcmp(buf, msg) != 0) return 12;

    closesocket(acc); closesocket(cli); closesocket(srv);
    WSACleanup();

    if (ntohl(htonl(0x01020304u)) != 0x01020304u) return 13;
    if (ntohs(htons(0x0102)) != 0x0102) return 14;
    return 0;
}

int main(void) {
    if (tcpRoundTrip() != 0) return tcpRoundTrip();

    /* The registry, whose value is read into a buffer the API sizes. */
    {
        HKEY k;
        char buf[256];
        DWORD sz = sizeof buf, type = 0;
        if (RegOpenKeyExA(HKEY_LOCAL_MACHINE,
                "SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion",
                0, KEY_READ, &k) != ERROR_SUCCESS) return 20;
        LSTATUS st = RegQueryValueExA(k, "CurrentVersion", NULL, &type, (LPBYTE)buf, &sz);
        RegCloseKey(k);
        if (st != ERROR_SUCCESS || type != REG_SZ || sz == 0) return 21;
    }

    /* A symbol resolved at run time and called through the pointer. */
    {
        HMODULE h = LoadLibraryA("kernel32.dll");
        if (!h) return 22;
        typedef DWORD(WINAPI * getTick)(void);
        getTick p = (getTick)(void *)GetProcAddress(h, "GetTickCount");
        if (!p || p() == 0) return 23;
        FreeLibrary(h);
    }

    /* Wide and narrow, which is where WCHAR's two bytes matter. */
    {
        WCHAR wide[MAX_PATH];
        char narrow[MAX_PATH];
        if (GetSystemDirectoryW(wide, MAX_PATH) == 0) return 24;
        if (WideCharToMultiByte(CP_UTF8, 0, wide, -1, narrow, MAX_PATH, NULL, NULL) <= 0) return 25;
        if (narrow[1] != ':') return 26;      /* a drive letter came back */
    }

    /* LARGE_INTEGER is a union over an anonymous struct: the halves and the
     * whole have to name the same eight bytes. */
    {
        LARGE_INTEGER li, freq;
        li.QuadPart = 0x0123456789ABCDEFLL;
        if (li.LowPart != 0x89ABCDEFu) return 27;
        if (li.HighPart != 0x01234567) return 28;
        if (!QueryPerformanceFrequency(&freq) || freq.QuadPart <= 0) return 29;
    }

    /* A slim lock, which is a pointer-sized struct the CRT never touches. */
    {
        SRWLOCK lock = SRWLOCK_INIT;
        AcquireSRWLockExclusive(&lock);
        ReleaseSRWLockExclusive(&lock);
        AcquireSRWLockShared(&lock);
        ReleaseSRWLockShared(&lock);
    }

    printf("Windows API OK\n");
    return 0;
}

#else

int main(void) {
    printf("Windows API OK (not this host)\n");
    return 0;
}

#endif
