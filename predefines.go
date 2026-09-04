package vcc

import (
	"fmt"
	"strings"

	"github.com/vertex-language/vcc/preprocessor"
	"github.com/vertex-language/vcc/types"
)

// ---- identity predefines ----
//
// Identity macros say *what the target is*, the way __LONG_MAX__ says
// what its long is: facts of the target name, keyed on the name's two
// halves, never on the host — `vcc -target x86_64-windows` on a Mac
// defines _WIN32, or cross-compilation is silently wrong.
//
// Only reserved-namespace spellings appear. gcc in ISO mode defines
// __linux__ and not `linux`, and vcc is nothing but strict mode, so
// the bare spellings never exist here.
//
// __GNUC__ is deliberately absent. Claiming to be gcc makes system
// headers respond with the GNU dialect — __attribute__, __asm,
// statement expressions — which is exactly the language vcc rejects.
// Unknown-compiler fallback paths are the paths vcc wants.
//
// These apply under -freestanding too: gcc defines __linux__ under
// -ffreestanding, because the target's identity is orthogonal to
// whether a libc exists. Hosted keeps meaning what it means now —
// __STDC_HOSTED__ and the search list.

// osIdent: what the OS half of a target name predefines. Darwin does
// not define __unix__ (matching clang there), and __ELF__ belongs to
// every ELF platform including bare metal.
var osIdent = map[string][][2]string{
	"linux":   {{"__linux__", "1"}, {"__gnu_linux__", "1"}, {"__unix__", "1"}, {"__ELF__", "1"}},
	"macos":   {{"__APPLE__", "1"}, {"__MACH__", "1"}},
	"elf":     {{"__ELF__", "1"}},
	"windows": {{"_WIN32", "1"}},
}

// archIdent: what the architecture half predefines. gcc's spellings,
// both with and without the trailing underscores, because system
// headers test both.
var archIdent = map[string][][2]string{
	"x86_64":  {{"__x86_64__", "1"}, {"__x86_64", "1"}, {"__amd64__", "1"}, {"__amd64", "1"}},
	"aarch64": {{"__aarch64__", "1"}},
	"i386":    {{"__i386__", "1"}, {"__i386", "1"}},
}

// Predefines computes the target-dependent macros: the identity set (who the
// target is), then the model facts sysroot's builtin headers are written
// against (what its types are). The names are gcc's spellings, on purpose —
// system headers already test them. This is the inversion the preprocessor
// README documents: these are facts about a target, phase 4 does not import
// types, so they are computed here and handed in as text. All of it precedes
// the caller's -D/-U, so -U__APPLE__ strips an identity macro with no new
// mechanism.
//
// Without these the system headers do not compile: <stdio.h> on Darwin needs
// __LP64__, __arm64__ and __SIZE_TYPE__, and one of them wrong is a struct of
// the wrong shape rather than an error message.
func (t Target) Predefines() []preprocessor.Predefine {
	m := t.model
	var ds []preprocessor.Predefine
	def := func(name, value string) {
		ds = append(ds, preprocessor.Predefine{Text: name + "=" + value})
	}
	smax := func(bytes int64) uint64 { return 1<<(bytes*8-1) - 1 }
	umax := func(bytes int64) uint64 {
		if bytes >= 8 {
			return ^uint64(0)
		}
		return 1<<(bytes*8) - 1
	}

	// ---- identity ----
	arch, osname := SplitTarget(t.name)
	for _, d := range archIdent[arch] {
		def(d[0], d[1])
	}
	for _, d := range osIdent[osname] {
		def(d[0], d[1])
	}

	// Apple spells aarch64 its own way, and Darwin's machine/_types.h
	// dispatches on exactly this spelling. Apple's headers also test
	// __LITTLE_ENDIAN__ directly (clang defines it there; gcc on
	// Linux does not, so it is not in the general set below).
	if osname == "macos" && arch == "aarch64" {
		def("__arm64__", "1")
		def("__arm64", "1")
	}
	if osname == "macos" {
		def("__LITTLE_ENDIAN__", "1")
	}

	undef := func(name string) {
		ds = append(ds, preprocessor.Predefine{Kind: preprocessor.PredefineUndef, Text: name})
	}

	// The Windows SDK dispatches on MSVC's spellings, so a Windows
	// target speaks them. _WIN64 is a fact of pointer width, stated
	// from the model rather than repeated per entry.
	if osname == "windows" {
		undef("__GNUC__")
		undef("__GNUC_MINOR__")
		undef("__GNUC_PATCHLEVEL__")

		// _MSC_EXTENSIONS is not a claim to be MSVC — that is _MSC_VER,
		// which vcc does not define and does not want, for the reason
		// __GNUC__ is absent above. It is the answer to one question the
		// SDK asks with it, and the answer is yes: ntdef.h reads it to
		// decide whether a nameless struct or union member may be written,
		// and a nameless member is §6.7.2.1p13, which vcc implements.
		//
		// Without it every such member is given a name — OVERLAPPED's
		// offset fields become ovlp.s.Offset — and the ordinary Win32 code
		// that writes ovlp.Offset does not compile. What else the SDK gates
		// on it is a zero-length array at the end of a struct, which vcc
		// also implements, and SAL annotations behind _PREFAST_, which
		// nothing here defines.
		def("_MSC_EXTENSIONS", "1")

		// _CRT_DECLARE_NONSTDC_NAMES asks the ucrt for open, read, write,
		// close, strdup and the rest of the names it spells with a leading
		// underscore as well.
		//
		// It is not a preference either. The header's own guard is
		//
		//	(!defined _CRT_DECLARE_NONSTDC_NAMES && !__STDC__)
		//
		// and MSVC does not define __STDC__ — not even under /std:c11,
		// which was checked rather than assumed — so under MSVC those
		// names are there. vcc does define it, being entitled to
		// (§6.10.8.1) and having no reason not to, and the same headers
		// then hide everything a Windows program written for those
		// compilers uses: zlib's gzlib.c calls open() and gets six
		// undeclared identifiers.
		//
		// So the switch the header offers for exactly this is set, and
		// the visible name set is MSVC's. A program that wants the other
		// one can say -U _CRT_DECLARE_NONSTDC_NAMES; the target's
		// predefines come first so that it can.
		def("_CRT_DECLARE_NONSTDC_NAMES", "1")

		if m.SizePtr == 8 {
			def("_WIN64", "1")
		}
		switch arch {
		case "x86_64":
			def("_M_X64", "100")
			def("_M_AMD64", "100")
		case "aarch64":
			def("_M_ARM64", "1")
		}
	}

	// LP64 is a fact the model already states; say it in the spelling
	// headers test. Windows (LLP64) correctly falls out.
	if m.SizeLong == 8 && m.SizePtr == 8 {
		def("__LP64__", "1")
		def("_LP64", "1")
	}

	// Byte order. Every target vcc currently models is little-endian;
	// a big-endian entry would move this into the target table as a
	// field, the way ldblKind is. glibc's headers reach these fast.
	def("__ORDER_LITTLE_ENDIAN__", "1234")
	def("__ORDER_BIG_ENDIAN__", "4321")
	def("__ORDER_PDP_ENDIAN__", "3412")
	def("__BYTE_ORDER__", "__ORDER_LITTLE_ENDIAN__")

	// ---- model facts ----

	// limits.h's inputs
	def("__CHAR_BIT__", "8")
	if !m.CharSigned {
		def("__CHAR_UNSIGNED__", "1")
	}
	def("__SCHAR_MAX__", "127")
	def("__SHRT_MAX__", fmt.Sprint(smax(m.SizeShort)))
	def("__INT_MAX__", fmt.Sprint(smax(m.SizeInt)))
	def("__LONG_MAX__", fmt.Sprintf("%dL", smax(m.SizeLong)))
	def("__LONG_LONG_MAX__", fmt.Sprintf("%dLL", smax(m.SizeLongLong)))

	// sizeof
	def("__SIZEOF_SHORT__", fmt.Sprint(m.SizeShort))
	def("__SIZEOF_INT__", fmt.Sprint(m.SizeInt))
	def("__SIZEOF_LONG__", fmt.Sprint(m.SizeLong))
	def("__SIZEOF_LONG_LONG__", fmt.Sprint(m.SizeLongLong))
	def("__SIZEOF_POINTER__", fmt.Sprint(m.SizePtr))

	// stddef.h / stdint.h types. size_t is the unsigned integer type
	// of pointer width: unsigned long where long is that wide (LP64),
	// unsigned long long otherwise (LLP64).
	sizeType, ptrdiffType := "unsigned long", "long"
	if m.SizeLong != m.SizePtr {
		sizeType, ptrdiffType = "unsigned long long", "long long"
	}
	def("__SIZE_TYPE__", sizeType)
	def("__SIZE_MAX__", fmt.Sprintf("%dULL", umax(m.SizePtr)))
	def("__PTRDIFF_TYPE__", ptrdiffType)
	def("__PTRDIFF_MAX__", fmt.Sprintf("%dLL", smax(m.SizePtr)))

	// wchar_t / wint_t
	wtype, wmax, wmin := kindC(m.WCharKind, m)
	def("__WCHAR_TYPE__", wtype)
	def("__WCHAR_MAX__", wmax)
	def("__WCHAR_MIN__", wmin)
	def("__WINT_TYPE__", t.wint)
	if strings.HasPrefix(t.wint, "unsigned") {
		def("__WINT_MAX__", fmt.Sprintf("%dU", umax(m.SizeInt)))
		def("__WINT_MIN__", "0U")
	} else {
		def("__WINT_MAX__", fmt.Sprint(smax(m.SizeInt)))
		def("__WINT_MIN__", fmt.Sprintf("(-%d - 1)", smax(m.SizeInt)))
	}

	// intmax_t: long long on every current target, 64 bits.
	def("__INTMAX_TYPE__", "long long")
	def("__UINTMAX_TYPE__", "unsigned long long")
	def("__INTMAX_MAX__", fmt.Sprintf("%dLL", smax(8)))

	// float.h's inputs
	def("__FLT_EVAL_METHOD__", "0")
	def("__FLT_RADIX__", "2")
	for _, d := range fltDefs {
		def(d[0], d[1])
	}
	for _, d := range dblDefs {
		def(d[0], d[1])
	}
	for _, d := range ldblDefs[t.ldbl] {
		def(d[0], d[1])
	}
	return ds
}

// kindC spells a basic kind as C and gives its range, for wchar_t.
func kindC(k types.Kind, m types.Model) (typ, max, min string) {
	switch k {
	case types.Int:
		n := uint64(1)<<(m.SizeInt*8-1) - 1
		return "int", fmt.Sprint(n), fmt.Sprintf("(-%d - 1)", n)
	case types.UInt:
		return "unsigned int", fmt.Sprintf("%dU", uint64(1)<<(m.SizeInt*8)-1), "0U"
	case types.UShort:
		return "unsigned short", fmt.Sprint(uint64(1)<<(m.SizeShort*8) - 1), "0"
	case types.Long:
		n := uint64(1)<<(m.SizeLong*8-1) - 1
		return "long", fmt.Sprintf("%dL", n), fmt.Sprintf("(-%dL - 1L)", n)
	}
	return "int", "2147483647", "(-2147483647 - 1)"
}

// IEEE single and double are the same on every target vcc models;
// only long double varies, by ldblKind.
var fltDefs = [][2]string{
	{"__FLT_MANT_DIG__", "24"}, {"__FLT_DIG__", "6"},
	{"__FLT_MIN_EXP__", "(-125)"}, {"__FLT_MIN_10_EXP__", "(-37)"},
	{"__FLT_MAX_EXP__", "128"}, {"__FLT_MAX_10_EXP__", "38"},
	{"__FLT_MAX__", "3.40282347e+38F"},
	{"__FLT_EPSILON__", "1.19209290e-7F"},
	{"__FLT_MIN__", "1.17549435e-38F"},
	{"__FLT_DENORM_MIN__", "1.40129846e-45F"},
}

var dblDefs = [][2]string{
	{"__DBL_MANT_DIG__", "53"}, {"__DBL_DIG__", "15"},
	{"__DBL_MIN_EXP__", "(-1021)"}, {"__DBL_MIN_10_EXP__", "(-307)"},
	{"__DBL_MAX_EXP__", "1024"}, {"__DBL_MAX_10_EXP__", "308"},
	{"__DBL_MAX__", "1.7976931348623157e+308"},
	{"__DBL_EPSILON__", "2.2204460492503131e-16"},
	{"__DBL_MIN__", "2.2250738585072014e-308"},
	{"__DBL_DENORM_MIN__", "4.9406564584124654e-324"},
}

var ldblDefs = map[ldblKind][][2]string{
	ldblDouble: {
		{"__LDBL_MANT_DIG__", "53"}, {"__LDBL_DIG__", "15"},
		{"__LDBL_MIN_EXP__", "(-1021)"}, {"__LDBL_MIN_10_EXP__", "(-307)"},
		{"__LDBL_MAX_EXP__", "1024"}, {"__LDBL_MAX_10_EXP__", "308"},
		{"__LDBL_MAX__", "1.7976931348623157e+308L"},
		{"__LDBL_EPSILON__", "2.2204460492503131e-16L"},
		{"__LDBL_MIN__", "2.2250738585072014e-308L"},
		{"__LDBL_DENORM_MIN__", "4.9406564584124654e-324L"},
		{"__DECIMAL_DIG__", "17"},
	},
	ldblX87: {
		{"__LDBL_MANT_DIG__", "64"}, {"__LDBL_DIG__", "18"},
		{"__LDBL_MIN_EXP__", "(-16381)"}, {"__LDBL_MIN_10_EXP__", "(-4931)"},
		{"__LDBL_MAX_EXP__", "16384"}, {"__LDBL_MAX_10_EXP__", "4932"},
		{"__LDBL_MAX__", "1.18973149535723176502e+4932L"},
		{"__LDBL_EPSILON__", "1.08420217248550443401e-19L"},
		{"__LDBL_MIN__", "3.36210314311209350626e-4932L"},
		{"__LDBL_DENORM_MIN__", "3.64519953188247460253e-4951L"},
		{"__DECIMAL_DIG__", "21"},
	},
	ldblQuad: {
		{"__LDBL_MANT_DIG__", "113"}, {"__LDBL_DIG__", "33"},
		{"__LDBL_MIN_EXP__", "(-16381)"}, {"__LDBL_MIN_10_EXP__", "(-4931)"},
		{"__LDBL_MAX_EXP__", "16384"}, {"__LDBL_MAX_10_EXP__", "4932"},
		{"__LDBL_MAX__", "1.189731495357231765085759326628007e+4932L"},
		{"__LDBL_EPSILON__", "1.925929944387235853055977942584927e-34L"},
		{"__LDBL_MIN__", "3.362103143112093506262677817321753e-4932L"},
		{"__LDBL_DENORM_MIN__", "6.475175119438025110924438958227646e-4966L"},
		{"__DECIMAL_DIG__", "36"},
	},
}
