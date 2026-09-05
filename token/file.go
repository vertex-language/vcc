package token

import (
	"bytes"
	"fmt"
	"sort"
)

// Position is a resolved location in raw (as-typed) bytes, so
// diagnostics line up with what the user wrote even through trigraphs
// and splices. Line and Column are 1-based; Offset is a raw byte
// offset.
type Position struct {
	Filename string
	Offset   int
	Line     int
	Column   int
}

func (p Position) String() string {
	return fmt.Sprintf("%s:%d:%d", p.Filename, p.Line, p.Column)
}

// File owns one translation unit's position space. NewFile runs
// phases 1–2 (trigraph replacement, then line splicing) and keeps the
// mapping from translated text back to raw bytes.
type File struct {
	name string
	src  []byte // raw, as typed
	text []byte // translated, what the scanner reads

	// Per translated byte: the raw byte range it came from.
	// nil on the fast path (no '?' and no '\\' in src), where
	// translated offsets are raw offsets.
	rawLo, rawHi []int32

	lineStarts []int32 // raw offsets of line starts
	diags      []Diagnostic
}

// NewFile translates src through phases 1–2 and returns its File.
// Trigraph replacements are reported at Warn severity.
//
// §5.1.1.2 leaves a non-empty file that does not end in a newline
// undefined; vcc defines it: end of file terminates the final line,
// silently. The hazard the rule guards against — the last line of an
// included file textually fusing with the first line after the
// #include — cannot occur here, because inclusion merges token
// streams, never bytes. A file whose final newline is consumed by a
// line splice is still reported, as a warning: the author wrote a
// continuation that continues into nothing, which is almost always a
// truncated file.
func NewFile(name string, src []byte) *File {
	f := &File{name: name, src: src}
	f.scanLines()

	spliceAtEOF := false
	if bytes.IndexByte(src, '?') < 0 && bytes.IndexByte(src, '\\') < 0 {
		f.text = src // fast path: identity mapping
	} else {
		spliceAtEOF = f.translate()
	}

	if spliceAtEOF {
		f.report(Warn, len(f.text)-1, "backslash-newline at end of file")
	}
	return f
}

// report appends one diagnostic with a non-empty span clamped to the
// translated text. A file whose translation is empty has no span to
// carry one; nothing is reported.
func (f *File) report(sev Severity, off int, msg string) {
	n := len(f.text)
	if n == 0 {
		return
	}
	if off < 0 {
		off = 0
	}
	if off >= n {
		off = n - 1
	}
	p := f.Pos(off)
	f.diags = append(f.diags, Diagnostic{Pos: p, End: p + 1, Severity: sev, Message: msg})
}

var trigraphs = map[byte]byte{
	'=': '#', '(': '[', '/': '\\', ')': ']', '\'': '^',
	'<': '{', '!': '|', '>': '}', '-': '~',
}

// translate runs phases 1–2 in one pass, building text and the
// translated→raw mapping. Reports each trigraph at Warn severity.
// Returns whether the file's final bytes were consumed by a splice.
func (f *File) translate() (spliceAtEOF bool) {
	src, n := f.src, len(f.src)
	text := make([]byte, 0, n)
	lo := make([]int32, 0, n)
	hi := make([]int32, 0, n)

	for i := 0; i < n; {
		c, start, end := src[i], i, i+1

		// Phase 1: trigraph replacement.
		if c == '?' && i+2 < n && src[i+1] == '?' {
			if r, ok := trigraphs[src[i+2]]; ok {
				p := Pos(len(text) + 1)
				f.diags = append(f.diags, Diagnostic{
					Pos: p, End: p + 1, Severity: Warn,
					Message: fmt.Sprintf("trigraph ??%c translated to %c", src[i+2], r),
				})
				c, end = r, i+3
			}
		}

		// Phase 2: line splicing. The backslash may itself be the
		// product of ??/; the newline is never a trigraph.
		if c == '\\' && end < n {
			switch src[end] {
			case '\n':
				i = end + 1
				spliceAtEOF = i == n
				continue
			case '\r':
				i = end + 1
				if i < n && src[i] == '\n' {
					i++
				}
				spliceAtEOF = i == n
				continue
			}
		}

		text = append(text, c)
		lo = append(lo, int32(start))
		hi = append(hi, int32(end))
		i = end
		spliceAtEOF = false
	}

	f.text, f.rawLo, f.rawHi = text, lo, hi
	return spliceAtEOF
}

// scanLines records raw line starts. \n, \r\n, and lone \r all
// terminate a line.
func (f *File) scanLines() {
	starts := []int32{0}
	src := f.src
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\n':
			starts = append(starts, int32(i+1))
		case '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			starts = append(starts, int32(i+1))
		}
	}
	f.lineStarts = starts
}

// Name returns the file name given to NewFile.
func (f *File) Name() string { return f.name }

// Text returns the translated text — what the scanner reads.
func (f *File) Text() []byte { return f.text }

// Source returns the raw bytes — what the user typed.
func (f *File) Source() []byte { return f.src }

// Size is the length of the translated text. Pos(Size()) is the
// valid one-past-the-end position (the scanner's EOF token).
func (f *File) Size() int { return len(f.text) }

// Pos converts a translated-text offset in [0, Size()] to a Pos.
func (f *File) Pos(offset int) Pos {
	if offset < 0 || offset > len(f.text) {
		panic(fmt.Sprintf("token: Pos offset %d out of range [0, %d]", offset, len(f.text)))
	}
	return Pos(offset + 1)
}

// Offset converts a Pos back to a translated-text offset.
func (f *File) Offset(p Pos) int {
	if !p.IsValid() || int(p) > len(f.text)+1 {
		panic(fmt.Sprintf("token: invalid Pos %d for file %q", p, f.name))
	}
	return int(p) - 1
}

// Slice returns the translated bytes of a span — what the scanner
// read. Feed this to decoders.
func (f *File) Slice(pos, end Pos) []byte {
	return f.text[f.Offset(pos):f.Offset(end)]
}

// Raw returns the raw bytes of a span — what the user typed. It
// widens to cover a whole trigraph or splice when the span cuts
// through one. Underline this in diagnostics.
func (f *File) Raw(pos, end Pos) []byte {
	lo, hi := f.Offset(pos), f.Offset(end)
	if f.rawLo == nil {
		return f.src[lo:hi]
	}
	if lo >= hi {
		r := f.rawOff(lo)
		return f.src[r:r]
	}
	return f.src[f.rawLo[lo]:f.rawHi[hi-1]]
}

// rawOff maps a translated offset to the raw offset of its first byte;
// Size() maps to len(Source()).
func (f *File) rawOff(off int) int {
	if f.rawLo == nil {
		return off
	}
	if off >= len(f.text) {
		return len(f.src)
	}
	return int(f.rawLo[off])
}

// rawAfter maps a translated offset to the raw offset just past the
// byte before it — the start of any inter-token trivia (including
// spliced-away bytes).
func (f *File) rawAfter(off int) int {
	if f.rawLo == nil {
		return off
	}
	if off <= 0 {
		return 0
	}
	if off > len(f.text) {
		return len(f.src)
	}
	return int(f.rawHi[off-1])
}

// Position resolves a Pos to raw-byte offset, line, and column.
func (f *File) Position(p Pos) Position {
	raw := f.rawOff(f.Offset(p))
	i := sort.Search(len(f.lineStarts), func(i int) bool {
		return int(f.lineStarts[i]) > raw
	}) - 1
	return Position{
		Filename: f.name,
		Offset:   raw,
		Line:     i + 1,
		Column:   raw - int(f.lineStarts[i]) + 1,
	}
}

// Between returns the raw trivia (whitespace, comments, spliced
// newlines) between two tokens — for formatters.
func (f *File) Between(prev, next Token) []byte {
	lo := f.rawAfter(f.Offset(prev.End))
	hi := f.rawOff(f.Offset(next.Pos))
	if lo > hi {
		lo = hi
	}
	return f.src[lo:hi]
}

// Diagnostics returns the phase 1–2 diagnostics, sorted. The scanner
// merges these into its own slice.
func (f *File) Diagnostics() []Diagnostic {
	SortDiagnostics(f.diags)
	return f.diags
}
