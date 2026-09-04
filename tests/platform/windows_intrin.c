/* <intrin.h>: MSVC's compiler intrinsics.
 *
 * They are the platform's, like the Interlocked family and the _mm_* family:
 * the header declares each one, `#pragma intrinsic` hands the name to the
 * compiler, and no library defines it. ucrt exports no __cpuid. A compiler
 * that lowers one as an ordinary call compiles a program that does not link.
 *
 * Every answer here is one another compiler agrees with, so the file runs
 * unchanged under cl.exe — which is how each expected value was arrived at.
 * The ones with no portable answer (the exact value __rdtsc returns, the
 * contents of a CPUID leaf) are checked for the shape of the answer instead:
 * that the counter moves, that leaf 0 reports a vendor and a maximum.
 */

#include <stdio.h>

#if defined(_MSC_VER) || defined(__VCC__)

#include <intrin.h>

int main(void) {
    /* —— rotates —— */
    if (_rotl(0x12345678u, 8) != 0x34567812u) return 1;
    if (_rotr(0x12345678u, 8) != 0x78123456u) return 2;
    if (_rotl(0xdeadbeefu, 0) != 0xdeadbeefu) return 3;
    if (_rotl64(0x0123456789abcdefULL, 16) != 0x456789abcdef0123ULL) return 4;
    if (_rotr64(0x0123456789abcdefULL, 16) != 0xcdef0123456789abULL) return 5;
    if (_rotl8(0x81, 1) != 0x03) return 6;
    if (_rotr8(0x81, 1) != 0xc0) return 7;
    if (_rotl8(0x81, 0) != 0x81) return 8;
    if (_rotl16(0x8001, 1) != 0x0003) return 9;
    if (_rotr16(0x8001, 1) != 0xc000) return 10;

    /* —— byte swaps —— */
    if (_byteswap_ushort(0x1234) != 0x3412) return 11;
    if (_byteswap_ulong(0x12345678u) != 0x78563412u) return 12;
    if (_byteswap_uint64(0x0123456789abcdefULL) != 0xefcdab8967452301ULL) return 13;

    /* —— bit scan ——
     *
     * Both are undefined for a zero mask, and the answer is the part both
     * compilers specify: nonzero where there was a bit to find. The index
     * MSVC leaves behind in the other case is not one of them. */
    {
        unsigned long idx = 0xabcdu;
        if (!_BitScanForward(&idx, 0x00100000u) || idx != 20) return 14;
        if (!_BitScanReverse(&idx, 0x00100010u) || idx != 20) return 15;
        /* A zero mask: the intrinsic answers zero. Both compilers
         * document the index as unmodified and neither actually
         * guarantees it under optimization, so only the answer is
         * checked — which is the part that is specified. */
        if (_BitScanForward(&idx, 0u)) return 16;
        if (!_BitScanForward64(&idx, 0x0000000100000000ULL) || idx != 32) return 17;
        if (!_BitScanReverse64(&idx, 0x8000000000000000ULL) || idx != 63) return 18;
    }

    /* —— counting ——
     *
     * POPCNT and LZCNT are not in the 2003 baseline. MSVC emits them
     * anyway and leaves the CPUID check to the caller; vcc computes the
     * same answers without them, so that a program including <intrin.h>
     * builds with no architecture flag.
     *
     * The lzcnt assertions are behind the feature bit, and the reason is
     * the hazard the instruction is known for: LZCNT's encoding is BSR
     * with an F3 prefix, and a processor without the feature ignores the
     * prefix and runs BSR — which answers the index of the highest set
     * bit, not the count of the zeroes above it, and answers garbage for
     * zero. So on such a processor MSVC's __lzcnt(0x00100000) is 20 and
     * vcc's is 11, and 11 is what the intrinsic is documented to
     * return. Asserting it unconditionally would make this file fail
     * under MSVC on the machine it was written on, for a difference
     * that is the processor's rather than the compiler's. */
    if (__popcnt(0xf0f0f0f0u) != 16) return 19;
    if (__popcnt(0u) != 0) return 20;
    if (__popcnt64(0xffffffffffffffffULL) != 64) return 21;
    {
        int ext[4] = { 0, 0, 0, 0 };
        __cpuid(ext, (int)0x80000001);
        if ((ext[2] >> 5) & 1) {
            if (__lzcnt(0x00100000u) != 11) return 22;
            if (__lzcnt(0u) != 32) return 23;
            if (__lzcnt64(0x0000000100000000ULL) != 31) return 24;
        }
    }

    /* —— wide multiply —— */
    {
        unsigned __int64 hi = 0;
        unsigned __int64 lo = _umul128(0xffffffffffffffffULL, 2ULL, &hi);
        if (hi != 1 || lo != 0xfffffffffffffffeULL) return 25;
        if (__umulh(0xffffffffffffffffULL, 2ULL) != 1) return 26;
        if (__mulh(-1LL, 2LL) != -1) return 27;
    }

    /* —— the 128-bit shifts —— */
    if (__shiftleft128(0xff00000000000000ULL, 0x00000000000000ffULL, 8) != 0xffffULL) return 28;
    if (__shiftright128(0xff00000000000000ULL, 0x00000000000000ffULL, 8) != 0xffff000000000000ULL) return 29;
    if (__shiftleft128(0x1ULL, 0x2ULL, 0) != 0x2ULL) return 30;

    /* —— carry chains —— */
    {
        unsigned __int64 out = 0;
        unsigned char c;
        c = _addcarry_u64(0, 0xffffffffffffffffULL, 1ULL, &out);
        if (c != 1 || out != 0) return 31;
        c = _addcarry_u64(1, 0xffffffffffffffffULL, 0ULL, &out);
        if (c != 1 || out != 0) return 32;
        c = _addcarry_u64(0, 2ULL, 3ULL, &out);
        if (c != 0 || out != 5) return 33;
        c = _subborrow_u64(0, 0ULL, 1ULL, &out);
        if (c != 1 || out != 0xffffffffffffffffULL) return 34;
        c = _subborrow_u64(1, 5ULL, 3ULL, &out);
        if (c != 0 || out != 1) return 35;
    }
    {
        unsigned int out32 = 0;
        unsigned char c = _addcarry_u32(0, 0xffffffffu, 1u, &out32);
        if (c != 1 || out32 != 0) return 36;
    }

    /* —— the processor ——
     *
     * Leaf 0 reports the vendor in EBX:EDX:ECX and the highest leaf in EAX.
     * Every x86-64 answers at least 1, and every vendor string is printable,
     * which is as much as a portable test can say. */
    {
        int info[4] = { 0, 0, 0, 0 };
        char vendor[13];
        int i;
        __cpuid(info, 0);
        if (info[0] < 1) return 37;
        for (i = 0; i < 4; i++) vendor[i] = (char)(info[1] >> (8 * i));
        for (i = 0; i < 4; i++) vendor[4 + i] = (char)(info[3] >> (8 * i));
        for (i = 0; i < 4; i++) vendor[8 + i] = (char)(info[2] >> (8 * i));
        vendor[12] = 0;
        for (i = 0; i < 12; i++)
            if (vendor[i] < 0x20 || vendor[i] > 0x7e) return 38;

        /* Leaf 1's EDX bit 26 is SSE2, which x86-64 has by definition. */
        __cpuidex(info, 1, 0);
        if (((info[3] >> 26) & 1) == 0) return 39;
    }

    /* The counter moves forward. Nothing else about it is portable. */
    {
        unsigned __int64 a = __rdtsc(), b, i;
        for (i = 0; i < 100000; i++) _mm_pause();
        b = __rdtsc();
        if (b <= a) return 40;
    }

    /* —— the thread environment block ——
     *
     * gs:[0x30] is the TEB's own linear address on x64: the field points at
     * the structure it is in, which is what makes it a self-check. */
    {
        unsigned __int64 teb = __readgsqword(0x30);
        if (teb == 0) return 41;
        if (__readgsqword(0x30) != teb) return 42;
    }

    /* —— barriers, which have no observable answer, only an effect —— */
    _ReadWriteBarrier();
    _ReadBarrier();
    _WriteBarrier();
    _mm_mfence();
    _mm_sfence();
    _mm_lfence();
    __nop();

    /* —— block moves —— */
    {
        unsigned char src[16], dst[16];
        int i;
        for (i = 0; i < 16; i++) { src[i] = (unsigned char)(i + 1); dst[i] = 0; }
        __movsb(dst, src, 16);
        for (i = 0; i < 16; i++) if (dst[i] != (unsigned char)(i + 1)) return 43;
        __stosb(dst, 0xab, 16);
        for (i = 0; i < 16; i++) if (dst[i] != 0xab) return 44;
    }

    /* —— bit test —— */
    {
        long w = 0x00000010L;
        if (_bittest(&w, 4) != 1) return 45;
        if (_bittest(&w, 5) != 0) return 46;
        if (_bittestandset(&w, 5) != 0 || w != 0x30L) return 47;
        if (_bittestandreset(&w, 4) != 1 || w != 0x20L) return 48;
        if (_bittestandcomplement(&w, 5) != 1 || w != 0x00L) return 49;

        /* The bit index runs past the word, exactly as the instruction's
         * does: the operand is the base of an array of them. */
        {
            long two[2] = { 0, 0 };
            if (_bittestandset(two, 33) != 0) return 50;
            if (two[1] != 2 || two[0] != 0) return 51;
        }
    }
    {
        long shared = 0;
        if (_interlockedbittestandset(&shared, 3) != 0 || shared != 8) return 52;
        if (_interlockedbittestandset(&shared, 3) != 1 || shared != 8) return 53;
        if (_interlockedbittestandreset(&shared, 3) != 1 || shared != 0) return 54;
    }

    /* —— the frame —— */
    if (_ReturnAddress() == 0) return 55;

    printf("Windows intrinsics OK\n");
    return 0;
}

#else

int main(void) {
    printf("Windows intrinsics OK (not a Microsoft-convention target)\n");
    return 0;
}

#endif
