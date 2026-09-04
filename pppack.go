package vcc

import (
	"strconv"

	"github.com/vertex-language/vcc/analyzer"
	"github.com/vertex-language/vcc/preprocessor"
	"github.com/vertex-language/vcc/scanner"
	"github.com/vertex-language/vcc/token"
)

// #pragma pack, read where the pragma line is last seen.
//
// The pragma sets a ceiling on how strictly the members of the structs after
// it are aligned, and it is not attached to any of them: it is a setting
// that runs from one line to the next one that changes it. Nothing in the
// grammar carries that, so somebody has to remember it across the file, and
// the place that can is the printer — phase 4 passes the line through, the
// bridge that re-scans phase 4's output drops it (a directive is not
// something phase 7 reads), and in between the whole stream goes past in
// order with the printed offsets known. See analyzer.PackAt.
//
// Ignoring it is not the conservative choice it looks like. The Windows SDK
// puts BITMAPFILEHEADER between pshpack2.h and poppack.h, which makes it
// fourteen bytes rather than sixteen; a compiler that reads past the pragma
// lays it out two bytes too long and writes .bmp files nothing else can
// read, with nothing in the program to say why.

// packState is the stack #pragma pack maintains.
type packState struct {
	cur   int64   // the ceiling in force, 0 for the target's own alignment
	stack []int64 // push, innermost last
	// names are the labels MSVC's push and pop accept, each holding the
	// depth its push was made at, so that a pop by name unwinds to it.
	names map[string]int
	out   []analyzer.PackAt
}

// record notes the current value as applying from a printed offset. A run
// that changes nothing is not recorded, so a header pushing and popping
// around a struct leaves two entries rather than four.
func (p *packState) record(off int32) {
	if n := len(p.out); n > 0 && p.out[n-1].Pack == p.cur {
		return
	}
	if len(p.out) == 0 && p.cur == 0 {
		return
	}
	p.out = append(p.out, analyzer.PackAt{Off: off, Pack: p.cur})
}

// apply reads one pragma line's tokens and updates the stack.
//
// line begins at the word after `pragma`, so a pack directive is `pack`
// followed by a parenthesized argument list. Anything else is another
// pragma and is not this function's business.
//
// The forms are MSVC's, which are also gcc's where the two overlap:
//
//	pack()                  the target's own alignment again
//	pack(n)                 n
//	pack(push)              remember, change nothing
//	pack(push, n)           remember, then n
//	pack(push, id)          remember under a label
//	pack(push, id, n)       remember under a label, then n
//	pack(pop)               back to the last remembered
//	pack(pop, id)           back to the one labelled id
//	pack(pop, id, n)        back to that one, then n
//
// A form this does not recognize — MSVC's `pack(show)`, or a spelling from
// somewhere else — leaves the state alone. That is the right failure: the
// pragma affects every struct after it, so guessing would be wrong for the
// rest of the file rather than for one line.
func (p *packState) apply(line []packTok, off int32) {
	if len(line) == 0 || line[0].text != "pack" {
		return
	}
	args, ok := packArgs(line[1:])
	if !ok {
		return
	}
	switch {
	case len(args) == 0:
		p.cur = 0

	case args[0] == "push":
		p.stack = append(p.stack, p.cur)
		rest := args[1:]
		if len(rest) > 0 && !isPackNumber(rest[0]) {
			if p.names == nil {
				p.names = map[string]int{}
			}
			p.names[rest[0]] = len(p.stack) - 1
			rest = rest[1:]
		}
		if len(rest) == 1 {
			if n, ok := packNumber(rest[0]); ok {
				p.cur = n
			}
		}

	case args[0] == "pop":
		rest := args[1:]
		depth := len(p.stack) - 1
		if len(rest) > 0 && !isPackNumber(rest[0]) {
			d, known := p.names[rest[0]]
			if !known {
				return // a label nothing pushed: leave the stack alone
			}
			depth = d
			delete(p.names, rest[0])
			rest = rest[1:]
		}
		if depth < 0 || depth >= len(p.stack) {
			return // pop with nothing pushed
		}
		p.cur = p.stack[depth]
		p.stack = p.stack[:depth]
		if len(rest) == 1 {
			if n, ok := packNumber(rest[0]); ok {
				p.cur = n
			}
		}

	case len(args) == 1:
		if n, ok := packNumber(args[0]); ok {
			p.cur = n
		}

	default:
		return
	}
	p.record(off)
}

// packArgs pulls the comma-separated spellings out of a parenthesized list,
// and reports false for anything that is not one.
func packArgs(ts []packTok) ([]string, bool) {
	if len(ts) < 2 || ts[0].kind != token.LPAREN || ts[len(ts)-1].kind != token.RPAREN {
		return nil, false
	}
	var args []string
	for _, t := range ts[1 : len(ts)-1] {
		if t.kind == token.COMMA {
			continue
		}
		if t.kind != token.IDENT && t.kind != token.INT_LIT {
			return nil, false
		}
		args = append(args, t.text)
	}
	return args, true
}

// packNumber is an alignment the pragma may ask for: a power of two from one
// to sixteen, which is what MSVC accepts and what an x86 target can honour.
func packNumber(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 1 || n > 16 || n&(n-1) != 0 {
		return 0, false
	}
	return n, true
}

func isPackNumber(s string) bool {
	_, ok := packNumber(s)
	return ok
}

// packTok is all a pack directive's operands are read as: a kind and a
// spelling. Both token streams the compiler carries reduce to it — phase 4's,
// which is where a .c file's pragma is seen, and the scanner's, which is
// where an already-preprocessed one's is.
type packTok struct {
	kind token.Kind
	text string
}

func packLine(ts []preprocessor.Token) []packTok {
	out := make([]packTok, len(ts))
	for i, t := range ts {
		out[i] = packTok{kind: t.Kind, text: t.Text()}
	}
	return out
}

// packsInSource reads the #pragma pack lines out of source that has already
// been preprocessed.
//
// A .i file does not go through phase 4, and a pragma is not phase 4's
// anyway: gcc and clang both leave #pragma lines in -E output precisely
// because the compiler proper is what acts on them. So the same reading
// happens here, over the file's own text, and the offsets are the file's own
// — which is what the analyzer looks its records up by.
//
// Without this, `vcc build --emit i` followed by compiling that output would
// silently lay every packed struct out differently from the .c it came from,
// which is the round trip the artifact promises. It is also what lets a
// build that preprocesses with clang keep its layouts.
func packsInSource(f *token.File) []analyzer.PackAt {
	toks, _ := scanner.Scan(f, scanner.ScanPP)
	var st packState
	for i := 0; i < len(toks); i++ {
		// A directive is a '#' opening a logical line, and this one has to
		// be `# pragma pack`.
		if toks[i].Kind != token.HASH || !toks[i].Flags.Has(token.FlagNLBefore) {
			continue
		}
		at := int32(toks[i].Pos) - 1
		line := make([]packTok, 0, 8)
		i++
		for ; i < len(toks); i++ {
			if toks[i].Kind == token.EOF || toks[i].Flags.Has(token.FlagNLBefore) {
				i--
				break
			}
			line = append(line, packTok{
				kind: toks[i].Kind,
				text: string(f.Slice(toks[i].Pos, toks[i].End)),
			})
		}
		if len(line) > 0 && line[0].text == "pragma" {
			st.apply(line[1:], at)
		}
	}
	return st.out
}
