package analyzer

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// Phases 5–6: literals arrive as raw spans and leave as values.
// Everything here is a pure function of the spelling and the Model.

// IntValue is a decoded integer constant: value plus the type
// §6.4.4.1p5's table assigns it.
type IntValue struct {
	Value uint64
	Type  types.Type
}

// DecodeIntConst decodes an integer constant's spelling. The scanner
// already enforced the lexical grammar; this assigns value and type,
// reporting only "too large".
func DecodeIntConst(text string, m types.Model, report func(string)) IntValue {
	base := 10
	switch {
	case strings.HasPrefix(text, "0x"), strings.HasPrefix(text, "0X"):
		base, text = 16, text[2:]
	case strings.HasPrefix(text, "0b"), strings.HasPrefix(text, "0B"):
		// gcc's binary constant, and C23's. Like hexadecimal and octal it
		// takes an unsigned candidate type where a decimal constant does
		// not, which the caller below reads off base != 10.
		base, text = 2, text[2:]
	case len(text) > 1 && text[0] == '0':
		base, text = 8, text[1:]
	}

	// MSVC sized suffix: [u|U]i{8,16,32,64}. The suffix names a
	// fixed-width type directly, bypassing the standard suffix/base
	// table. Strip it before the u/l loop below so the digit scanner
	// sees only digits.
	msvcWidth, msvcUnsigned := parseMSVCSuffix(text)
	if msvcWidth > 0 {
		// Strip the suffix (it's at the end of text).
		n := len(text)
		if msvcUnsigned {
			n-- // 'u'/'U'
		}
		// Count the suffix length: 'i' + digits of width
		switch msvcWidth {
		case 8:
			n -= 2 // "i8"
		case 16:
			n -= 3 // "i16"
		case 32:
			n -= 3 // "i32"
		case 64:
			n -= 3 // "i64"
		}
		text = text[:n]
	}

	var u, l int
	for len(text) > 0 {
		switch c := text[len(text)-1]; {
		case c == 'u' || c == 'U':
			u++
			text = text[:len(text)-1]
			continue
		case c == 'l' || c == 'L':
			l++
			text = text[:len(text)-1]
			continue
		}
		break
	}

	// A digit outside the base contributes nothing rather than a nonsense
	// value. The diagnostic for one is the scanner's, which judged the
	// spelling; this only has to not compute something absurd from it.
	var val uint64
	overflow := false
	for i := 0; i < len(text); i++ {
		if digitVal(text[i]) >= base {
			break
		}
		d := uint64(digitVal(text[i]))
		nv := val*uint64(base) + d
		if nv < val || (val != 0 && nv/uint64(base) != val) {
			overflow = true
		}
		val = nv
	}

	// MSVC sized suffix: the width names the exact type.
	if msvcWidth > 0 {
		var cands []types.Kind
		switch {
		case msvcWidth == 8 && msvcUnsigned:
			cands = []types.Kind{types.UChar}
		case msvcWidth == 8:
			cands = []types.Kind{types.SChar}
		case msvcWidth == 16 && msvcUnsigned:
			cands = []types.Kind{types.UShort}
		case msvcWidth == 16:
			cands = []types.Kind{types.Short}
		case msvcWidth == 32 && msvcUnsigned:
			cands = []types.Kind{types.UInt}
		case msvcWidth == 32:
			cands = []types.Kind{types.Int}
		case msvcWidth == 64 && msvcUnsigned:
			cands = []types.Kind{types.ULongLong}
		case msvcWidth == 64:
			cands = []types.Kind{types.LongLong}
		}
		if !overflow {
			for _, k := range cands {
				if val <= m.IntMax(types.Typ(k)) {
					return IntValue{val, types.Typ(k)}
				}
			}
		}
		report("integer constant is too large for any integer type")
		return IntValue{val, types.Typ(types.ULongLong)}
	}

	// §6.4.4.1p5: the first type in the suffix/base column that fits.
	var cands []types.Kind
	switch {
	case u > 0 && l >= 2:
		cands = []types.Kind{types.ULongLong}
	case u > 0 && l == 1:
		cands = []types.Kind{types.ULong, types.ULongLong}
	case u > 0:
		cands = []types.Kind{types.UInt, types.ULong, types.ULongLong}
	case l >= 2 && base == 10:
		cands = []types.Kind{types.LongLong}
	case l >= 2:
		cands = []types.Kind{types.LongLong, types.ULongLong}
	case l == 1 && base == 10:
		cands = []types.Kind{types.Long, types.LongLong}
	case l == 1:
		cands = []types.Kind{types.Long, types.ULong, types.LongLong, types.ULongLong}
	case base == 10:
		cands = []types.Kind{types.Int, types.Long, types.LongLong}
	default:
		cands = []types.Kind{types.Int, types.UInt, types.Long, types.ULong,
			types.LongLong, types.ULongLong}
	}
	if !overflow {
		for _, k := range cands {
			if val <= m.IntMax(types.Typ(k)) {
				return IntValue{val, types.Typ(k)}
			}
		}
	}
	report("integer constant is too large for any integer type")
	return IntValue{val, types.Typ(types.ULongLong)}
}

// parseMSVCSuffix checks whether text ends with an MSVC sized integer
// suffix: [u|U]i{8,16,32,64}. Returns (width, unsigned). Returns (0,
// false) if no MSVC suffix is present.
func parseMSVCSuffix(text string) (width int, unsigned bool) {
	n := len(text)
	if n == 0 {
		return 0, false
	}
	// Try to match i8, i16, i32, i64 at the end.
	for _, w := range []struct {
		s string
		v int
	}{{"i64", 64}, {"I64", 64}, {"i32", 32}, {"I32", 32}, {"i16", 16}, {"I16", 16}, {"i8", 8}, {"I8", 8}} {
		if strings.HasSuffix(text, w.s) {
			pos := n - len(w.s)
			if pos > 0 && (text[pos-1] == 'u' || text[pos-1] == 'U') {
				return w.v, true
			}
			return w.v, false
		}
	}
	return 0, false
}

func digitVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	// Not a digit in any base, which is what makes the caller's check
	// against the base catch it.
	return 99
}

// DecodeFloatConst decodes a floating constant's spelling to value
// and type.
func DecodeFloatConst(text string, report func(string)) (float64, types.Type) {
	t := types.Typ(types.Double)
	if len(text) > 0 {
		switch text[len(text)-1] {
		case 'f', 'F':
			t, text = types.Typ(types.Float), text[:len(text)-1]
		case 'l', 'L':
			t, text = types.Typ(types.LongDouble), text[:len(text)-1]
		}
	}
	v, err := strconv.ParseFloat(text, 64) // handles 0x1.8p3
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			report("floating constant out of range")
		} else {
			report("this is not a floating constant")
		}
	}
	return v, t
}

// DecodeCharConst decodes a character constant, prefix and quotes
// included. A plain multi-character constant has type int and the
// implementation-defined value (v << 8) | next, accumulated left to
// right.
func DecodeCharConst(text string, m types.Model, report func(string)) IntValue {
	elemMax, typ := uint32(0xFF), types.Typ(types.Int)
	switch text[0] {
	case 'L':
		typ, text = types.Typ(m.WCharKind), text[1:]
		elemMax = uint32(m.IntMax(typ))
	case 'u':
		typ, text = types.Typ(types.UShort), text[1:]
		elemMax = 0xFFFF
	case 'U':
		typ, text = types.Typ(types.UInt), text[1:]
		elemMax = 0xFFFFFFFF
	}
	body := strings.TrimSuffix(strings.TrimPrefix(text, "'"), "'")

	var val uint64
	n := 0
	for i := 0; i < len(body); n++ {
		cv, adv := decodeOne(body[i:], elemMax, report)
		val = val<<8 | uint64(cv)&0xFF
		if n == 0 {
			val = uint64(cv)
		}
		i += adv
	}
	if n > 1 && typ.Kind() != types.Int {
		report("multi-character constant with an encoding prefix")
	}
	return IntValue{val, typ}
}

// StringValue is one decoded, concatenated phase-6 string run:
// element type per the run's prefix, code units in Data, terminating
// NUL included — so array length is len(Data).
type StringValue struct {
	Elem types.Type
	Data []uint32
}

// DecodeString decodes a StringLit's segments and concatenates them
// (§6.4.5p5). Differing encoding prefixes across segments — other
// than plain combining with anything — are reported.
func DecodeString(unit *token.File, s *ast.StringLit, m types.Model, report func(string)) StringValue {
	prefix := byte(0)
	for _, seg := range s.Segs {
		text := string(unit.Slice(seg.Lo, seg.Hi))
		p := segPrefix(text)
		switch {
		case p == 0 || prefix == 0:
			if p != 0 {
				prefix = p
			}
		case p != prefix:
			report("string literal segments have differing encoding prefixes")
		}
	}

	elem, elemMax, utf8Out := types.Typ(types.Char), uint32(0xFF), true
	switch prefix {
	case 'u':
		elem, elemMax, utf8Out = types.Typ(types.UShort), 0xFFFF, false
	case 'U':
		elem, elemMax, utf8Out = types.Typ(types.UInt), 0xFFFFFFFF, false
	case 'L':
		elem, utf8Out = types.Typ(m.WCharKind), false
		elemMax = uint32(m.IntMax(elem))
	}

	var data []uint32
	for _, seg := range s.Segs {
		text := string(unit.Slice(seg.Lo, seg.Hi))
		text = text[strings.IndexByte(text, '"')+1:]
		text = strings.TrimSuffix(text, `"`)
		for i := 0; i < len(text); {
			if text[i] == '\\' {
				cv, adv := decodeOne(text[i:], elemMax, report)
				if utf8Out && cv > 0x7F && isUCNStart(text[i:]) {
					var buf [4]byte
					for _, b := range buf[:utf8.EncodeRune(buf[:], rune(cv))] {
						data = append(data, uint32(b))
					}
				} else {
					data = append(data, cv)
				}
				i += adv
				continue
			}
			if utf8Out {
				data = append(data, uint32(text[i]))
				i++
			} else {
				r, size := utf8.DecodeRuneInString(text[i:])
				data = append(data, uint32(r))
				i += size
			}
		}
	}
	return StringValue{Elem: elem, Data: append(data, 0)}
}

func segPrefix(text string) byte {
	switch {
	case strings.HasPrefix(text, `u8"`):
		return '8'
	case text[0] == 'u', text[0] == 'U', text[0] == 'L':
		return text[0]
	}
	return 0
}

func isUCNStart(s string) bool {
	return len(s) > 1 && (s[1] == 'u' || s[1] == 'U')
}

// decodeOne decodes one source character or escape, returning its
// value and bytes consumed. Numeric escapes are range-checked
// against the element type; UCNs against §6.4.3's constraints.
func decodeOne(s string, elemMax uint32, report func(string)) (uint32, int) {
	if s[0] != '\\' {
		return uint32(s[0]), 1
	}
	c := s[1]
	switch c {
	case 'a':
		return 7, 2
	case 'b':
		return 8, 2
	case 'f':
		return 12, 2
	case 'n':
		return 10, 2
	case 'r':
		return 13, 2
	case 't':
		return 9, 2
	case 'v':
		return 11, 2
	case '\'', '"', '?', '\\':
		return uint32(c), 2
	case 'x':
		i, v := 2, uint64(0)
		for i < len(s) && isHex(s[i]) {
			v = v*16 + uint64(digitVal(s[i]))
			i++
		}
		if v > uint64(elemMax) {
			report("hexadecimal escape sequence out of range")
		}
		return uint32(v), i
	case 'u', 'U':
		need := 4
		if c == 'U' {
			need = 8
		}
		v := uint64(0)
		i := 2
		for ; i < 2+need && i < len(s) && isHex(s[i]); i++ {
			v = v*16 + uint64(digitVal(s[i]))
		}
		switch {
		case v >= 0xD800 && v <= 0xDFFF, v > 0x10FFFF:
			report("universal character name names an invalid code point")
		case v < 0xA0 && v != '$' && v != '@' && v != '`':
			report("universal character name below U+00A0")
		}
		return uint32(v), i
	default:
		if c >= '0' && c <= '7' {
			i, v := 1, uint32(0)
			for i < len(s) && i <= 3 && s[i] >= '0' && s[i] <= '7' {
				v = v*8 + uint32(s[i]-'0')
				i++
			}
			if v > elemMax {
				report("octal escape sequence out of range")
			}
			return v, i
		}
		// Unknown escapes were the scanner's diagnostic; value falls
		// back to the character itself.
		return uint32(c), 2
	}
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
