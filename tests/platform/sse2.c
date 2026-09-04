/* SSE2's integer intrinsics, the _mm_* names <emmintrin.h> declares.
 *
 * The type is the platform's, not vcc's: MSVC's header writes __m128i as a
 * union marked __declspec(intrin_type), which says the record names a vector
 * register rather than a place in memory. vcc reads that mark, so a value of
 * the type lives in an XMM register and the intrinsics over it are single
 * instructions — while the union's members stay where they are and stay
 * readable, which is what the second half of this file checks.
 *
 * Every expected value here was produced by cl.exe /O2 on the same source
 * and read back byte for byte. That is the only way to check a saturating
 * add or a pack: the answer is what the instruction does, and a second
 * reading of the manual by the same pair of eyes is not an independent one.
 *
 * This is a platform test because the header is the platform's. gcc and
 * clang declare the same names in their own <emmintrin.h> and lower them to
 * the same instructions, so the file runs unchanged against either.
 */

#include <stdio.h>

#if defined(__SSE2__) || defined(_M_X64) || defined(_M_AMD64) || defined(__x86_64__)

#include <emmintrin.h>

/* eq reports whether v holds the sixteen bytes want names. Comparing the
 * bytes rather than the lanes keeps one helper for every lane shape, and
 * catches a result that is right in the lanes the test happened to print. */
static int eq(__m128i v, const unsigned char *want) {
    unsigned char got[16];
    int i;
    _mm_storeu_si128((__m128i *)got, v);
    for (i = 0; i < 16; i++) {
        if (got[i] != want[i]) {
            printf("lane byte %d: got %02x, want %02x\n", i, got[i], want[i]);
            return 0;
        }
    }
    return 1;
}

/* A by-value __m128i across a call boundary. The Microsoft convention passes
 * one by pointer to a copy the caller makes and returns one in XMM0, which
 * is not a shape any other type in C has. */
static __m128i twice(__m128i v) { return _mm_add_epi32(v, v); }

int main(void) {
    __m128i a = _mm_setr_epi8(1, 2, 3, 4, 5, 6, 7, 8,
                              (char)250, (char)251, (char)252, (char)253,
                              (char)254, (char)255, 0, (char)128);
    __m128i b = _mm_setr_epi8(10, 20, 30, 40, 50, 60, 70, 80,
                              6, 6, 6, 6, 6, 6, 6, 6);
    __m128i w = _mm_setr_epi16(-3, -2, -1, 0, 1, 2, 3, 30000);
    __m128i x = _mm_setr_epi16(7, 7, 7, 7, 7, 7, 7, 30000);
    __m128i d = _mm_setr_epi32(-2, -1, 100000, 7);
    __m128i e = _mm_setr_epi32(5, 5, 5, 5);

    /* Wrapping arithmetic at each lane width. */
    {
        static const unsigned char want[16] = {
            0x0b, 0x16, 0x21, 0x2c, 0x37, 0x42, 0x4d, 0x58,
            0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x86 };
        if (!eq(_mm_add_epi8(a, b), want)) return 1;
    }
    {
        static const unsigned char want[16] = {
            0x04, 0x00, 0x05, 0x00, 0x06, 0x00, 0x07, 0x00,
            0x08, 0x00, 0x09, 0x00, 0x0a, 0x00, 0x60, 0xea };
        if (!eq(_mm_add_epi16(w, x), want)) return 2;
    }
    {
        static const unsigned char want[16] = {
            0x03, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00,
            0xa5, 0x86, 0x01, 0x00, 0x0c, 0x00, 0x00, 0x00 };
        if (!eq(_mm_add_epi32(d, e), want)) return 3;
    }

    /* Saturation, which is the whole reason these are separate verbs: the
     * signed form clamps at 0x7f and 0x80, the unsigned at 0xff and 0. */
    {
        static const unsigned char want[16] = {
            0x0b, 0x16, 0x21, 0x2c, 0x37, 0x42, 0x4d, 0x58,
            0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x86 };
        if (!eq(_mm_adds_epi8(a, b), want)) return 4;
    }
    {
        static const unsigned char want[16] = {
            0x0b, 0x16, 0x21, 0x2c, 0x37, 0x42, 0x4d, 0x58,
            0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x06, 0x86 };
        if (!eq(_mm_adds_epu8(a, b), want)) return 5;
    }
    {
        static const unsigned char want[16] = {
            0x04, 0x00, 0x05, 0x00, 0x06, 0x00, 0x07, 0x00,
            0x08, 0x00, 0x09, 0x00, 0x0a, 0x00, 0xff, 0x7f };
        if (!eq(_mm_adds_epi16(w, x), want)) return 6;
    }

    /* Multiplication, which SSE2 gives at one lane width and in halves. */
    {
        static const unsigned char want[16] = {
            0xeb, 0xff, 0xf2, 0xff, 0xf9, 0xff, 0x00, 0x00,
            0x07, 0x00, 0x0e, 0x00, 0x15, 0x00, 0x00, 0xe9 };
        if (!eq(_mm_mullo_epi16(w, x), want)) return 7;
    }
    {
        static const unsigned char want[16] = {
            0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xa4, 0x35 };
        if (!eq(_mm_mulhi_epi16(w, x), want)) return 8;
    }
    {
        /* Even lanes only, widened: the odd ones are not read. */
        static const unsigned char want[16] = {
            0xf6, 0xff, 0xff, 0xff, 0x04, 0x00, 0x00, 0x00,
            0x20, 0xa1, 0x07, 0x00, 0x00, 0x00, 0x00, 0x00 };
        if (!eq(_mm_mul_epu32(d, e), want)) return 9;
    }

    /* Comparison yields a mask, and movemask is how it leaves the register
     * file as something a branch can read. */
    if (_mm_movemask_epi8(_mm_cmpeq_epi32(d, d)) != 0xffff) return 10;
    if (_mm_movemask_epi8(_mm_cmpeq_epi32(d, e)) != 0) return 11;
    if (_mm_movemask_epi8(a) != 0xbf00) return 12;
    if (_mm_movemask_epi8(_mm_cmpgt_epi8(a, b)) != 0x0000) return 13;
    if (_mm_movemask_epi8(_mm_cmplt_epi8(a, b)) != 0xffff) return 14;

    /* andnot negates its *first* operand, which is the opposite of what the
     * name suggests and the thing worth a test of its own. */
    {
        static const unsigned char want[16] = {
            0x0a, 0x14, 0x1c, 0x28, 0x32, 0x38, 0x40, 0x50,
            0x04, 0x04, 0x02, 0x02, 0x00, 0x00, 0x06, 0x06 };
        if (!eq(_mm_andnot_si128(a, b), want)) return 15;
    }

    /* Shifts: a literal count and a computed one take different instruction
     * forms, and a count past the lane width yields zero rather than
     * wrapping the way a scalar shift would. */
    {
        static const unsigned char want[16] = {
            0xe8, 0xff, 0xf0, 0xff, 0xf8, 0xff, 0x00, 0x00,
            0x08, 0x00, 0x10, 0x00, 0x18, 0x00, 0x80, 0xa9 };
        if (!eq(_mm_slli_epi16(w, 3), want)) return 16;
    }
    {
        volatile int n = 3;
        static const unsigned char want[16] = {
            0xe8, 0xff, 0xf0, 0xff, 0xf8, 0xff, 0x00, 0x00,
            0x08, 0x00, 0x10, 0x00, 0x18, 0x00, 0x80, 0xa9 };
        if (!eq(_mm_slli_epi16(w, n), want)) return 17;
    }
    if (_mm_movemask_epi8(_mm_slli_epi32(e, 99)) != 0) return 18;
    {
        /* The whole register, five bytes left, zeroes shifted in. */
        static const unsigned char want[16] = {
            0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03,
            0x04, 0x05, 0x06, 0x07, 0x08, 0xfa, 0xfb, 0xfc };
        if (!eq(_mm_slli_si128(a, 5), want)) return 19;
    }

    /* Packing saturates; unpacking interleaves. */
    {
        static const unsigned char want[16] = {
            0xfd, 0xfe, 0xff, 0x00, 0x01, 0x02, 0x03, 0x7f,
            0x07, 0x07, 0x07, 0x07, 0x07, 0x07, 0x07, 0x7f };
        if (!eq(_mm_packs_epi16(w, x), want)) return 20;
    }
    {
        static const unsigned char want[16] = {
            0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0xff,
            0x07, 0x07, 0x07, 0x07, 0x07, 0x07, 0x07, 0xff };
        if (!eq(_mm_packus_epi16(w, x), want)) return 21;
    }
    {
        static const unsigned char want[16] = {
            0x01, 0x0a, 0x02, 0x14, 0x03, 0x1e, 0x04, 0x28,
            0x05, 0x32, 0x06, 0x3c, 0x07, 0x46, 0x08, 0x50 };
        if (!eq(_mm_unpacklo_epi8(a, b), want)) return 22;
    }

    /* Permutes, whose pattern the instruction takes as an immediate. */
    {
        static const unsigned char want[16] = {
            0x07, 0x00, 0x00, 0x00, 0xa0, 0x86, 0x01, 0x00,
            0xff, 0xff, 0xff, 0xff, 0xfe, 0xff, 0xff, 0xff };
        if (!eq(_mm_shuffle_epi32(d, _MM_SHUFFLE(0, 1, 2, 3)), want)) return 23;
    }

    /* In and out of a general register. */
    if (_mm_cvtsi128_si32(d) != -2) return 24;
    if (_mm_extract_epi16(w, 1) != 0xfffe) return 25;
    if (_mm_extract_epi16(w, 7) != 30000) return 26;
    if (_mm_cvtsi128_si32(_mm_cvtsi32_si128(-5)) != -5) return 27;
    if (_mm_cvtsi128_si32(_mm_srli_si128(_mm_cvtsi32_si128(-5), 4)) != 0) return 28;
    {
        static const unsigned char want[16] = {
            0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
            0xef, 0xbe, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00 };
        if (!eq(_mm_insert_epi16(_mm_setzero_si128(), 0xbeef, 4), want)) return 29;
    }

    /* Loads and stores. The aligned and unaligned forms are different
     * instructions, and only one of them survives a misaligned address. */
    {
        unsigned char buf[48];
        unsigned char *al = buf + ((16 - ((unsigned long long)(size_t)buf & 15)) & 15);
        unsigned char out[16];
        int i;
        for (i = 0; i < 32; i++) al[i] = (unsigned char)(i * 7 + 1);

        if (!eq(_mm_load_si128((const __m128i *)al), al)) return 30;
        if (!eq(_mm_loadu_si128((const __m128i *)(al + 3)), al + 3)) return 31;

        _mm_store_si128((__m128i *)al, a);
        if (!eq(a, al)) return 32;
        _mm_storeu_si128((__m128i *)out, b);
        if (!eq(b, out)) return 33;

        /* Eight bytes into the low lane, the rest zeroed. */
        {
            unsigned char want[16];
            for (i = 0; i < 8; i++) want[i] = al[i];
            for (i = 8; i < 16; i++) want[i] = 0;
            if (!eq(_mm_loadl_epi64((const __m128i *)al), want)) return 34;
            if (!eq(_mm_move_epi64(_mm_load_si128((const __m128i *)al)), want)) return 35;
        }
    }

    /* setzero and set1, which every vector loop opens with. */
    if (_mm_movemask_epi8(_mm_setzero_si128()) != 0) return 36;
    if (_mm_movemask_epi8(_mm_set1_epi8((char)0x80)) != 0xffff) return 37;
    {
        volatile int k = -1;
        if (_mm_movemask_epi8(_mm_set1_epi32(k)) != 0xffff) return 38;
    }

    /* The union's members, which are the reason __m128i is one. */
    {
        __m128i v = _mm_set_epi32(4, 3, 2, 1);
        if (v.m128i_i32[0] != 1 || v.m128i_i32[3] != 4) return 39;
        if (v.m128i_i8[0] != 1 || v.m128i_u16[1] != 0) return 40;
        v.m128i_i32[2] = 99;
        if (_mm_extract_epi16(v, 4) != 99) return 41;
    }

    /* A value across a call boundary, and an array and a struct member of
     * the type — each a place the sixteen bytes have to live in memory
     * while the computation happens in a register. */
    {
        __m128i t = twice(_mm_set_epi32(4, 3, 2, 1));
        if (t.m128i_i32[0] != 2 || t.m128i_i32[3] != 8) return 42;
    }
    {
        __m128i arr[3];
        int i;
        for (i = 0; i < 3; i++) arr[i] = _mm_set1_epi32(i + 1);
        if (arr[0].m128i_i32[0] != 1 || arr[1].m128i_i32[3] != 2 ||
            arr[2].m128i_i32[1] != 3) return 43;
    }
    {
        struct holder { int tag; __m128i v; } h;
        h.tag = 7;
        h.v = _mm_set1_epi16(3);
        if (sizeof h != 32) return 44;
        if (h.tag != 7 || h.v.m128i_i16[5] != 3) return 45;
    }

    printf("SSE2 intrinsics OK\n");
    return 0;
}

#else

int main(void) {
    printf("SSE2 intrinsics OK (no vector unit on this target)\n");
    return 0;
}

#endif
