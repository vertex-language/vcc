package vcc

import (
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/vertex-language/vcc/analyzer"
	"github.com/vertex-language/vcc/preprocessor"
	"github.com/vertex-language/vcc/scanner"
	"github.com/vertex-language/vcc/token"
)

// srcSpan maps one span of the reparsed text back to where it was written.
//
// Phase 4's output is printed and re-scanned so the parser can read it (see
// reparse), which puts every position phases 5-7 report in that printed text
// rather than in the file the user typed. A two-line program that includes
// <stdio.h> then reports its own second line as line 248 of itself. The
// mapping is recorded as the text is printed, where both ends are known.
type srcSpan struct {
	lo, hi int32 // offsets in the printed text
	site   preprocessor.Site
}

// srcMap is the spans of one printed stream, in ascending order — which is
// the order they were written, so the lookup is a binary search.
type srcMap []srcSpan

// at finds the span covering an offset, or the one nearest before it.
func (m srcMap) at(off int32) (srcSpan, bool) {
	i := sort.Search(len(m), func(i int) bool { return m[i].hi > off })
	if i == len(m) {
		return srcSpan{}, false
	}
	return m[i], true
}

// site maps a span of the printed text back to a site in the source.
//
// The start decides the file: a span that begins in one file and ends in
// another is a macro expansion straddling the two, and the end is dropped
// rather than producing a span no file contains. Where both ends are in the
// same file the whole extent is kept, so an expression underlines as an
// expression and not as its first token.
func (m srcMap) site(lo, hi int32) (preprocessor.Site, bool) {
	first, ok := m.at(lo)
	if !ok || !first.site.Valid() {
		return preprocessor.Site{}, false
	}
	out := first.site
	if hi > lo {
		if last, ok := m.at(hi - 1); ok && last.site.Origin == out.Origin &&
			last.site.End > out.Pos {
			out.End = last.site.End
		}
	}
	return out, true
}

// printOpts is what the two consumers of printTokens disagree about.
// They agree about everything else, which is why there is one printer.
type printOpts struct {
	// srcMap, when non-nil, collects one span per token as the stream is
	// printed. The reparse bridge asks for it; --emit i does not, since its
	// output is the artifact rather than an intermediate to map back through.
	srcMap *srcMap

	// dropPragmaLines omits the pragma lines phase 4 passed through.
	// The reparse bridge needs it: the re-scan runs without ScanPP,
	// where a line-opening '#' is trivia plus a once-per-file warning,
	// and the parser has no use for the line either. --emit i must
	// not set it — a dropped #pragma breaks the promise that the
	// output re-enters as the same program.
	dropPragmaLines bool

	// packs, when non-nil, collects the #pragma pack state: one entry per
	// change, at the printed offset from which it applies. The reparse
	// bridge asks for it, because dropping the line is exactly what leaves
	// the phases above with no other way to learn it. See pppack.go.
	packs *[]analyzer.PackAt
}

// printTokens writes a phase-4 token stream back out as C source.
//
// Two rules, and the second is the one that matters. Line structure follows
// the tokens that came straight from a file: a macro's replacement list may
// have been written across several lines in its #define, and honouring those
// newlines would scatter the output for no gain.
//
// The other rule is paste avoidance. `#define PLUS +` then `+PLUS` must print
// `+ +`, never `++`, or the output does not re-enter as the same program.
//
// A pragma line phase 4 passed through opens with a generated '#' minted by
// doPragma. Under dropPragmaLines the line is skipped: a skipped line's
// tokens all came straight from the source file, so the skip ends at the
// first token that opens a line — or at the first expanded token, which by
// construction cannot belong to a directive line.
//
// When the line is printed rather than skipped, that same boundary decides
// where it ends, and the token after it opens a new line whatever its own
// flags say. A directive runs to the end of its line, so anything printed
// after a #pragma is part of that pragma — and the token that follows one is
// very often expanded, which is exactly the case the newline rule above
// suppresses. The Windows SDK writes #pragma warning(disable: 6530)
// immediately before a _CRT_STDIO_INLINE function, and printing the two on one
// line turns the whole declaration into the pragma's arguments.
func printTokens(w io.Writer, toks []preprocessor.Token, opts printOpts) error {
	var b strings.Builder
	var prev preprocessor.Token
	have := false
	skipPragma := false
	inPragma := false

	// The pragma line being read, and the state it feeds. Both halves are
	// collected whether the line is printed or skipped: --emit i prints it
	// and wants no state, the bridge skips it and wants nothing else.
	var pack packState
	var pragma []preprocessor.Token
	var pragmaAt int32
	endPragma := func() {
		if opts.packs != nil && len(pragma) > 0 {
			pack.apply(packLine(pragma[1:]), pragmaAt) // past the minted 'pragma'
		}
		pragma = nil
	}

	for _, t := range toks {
		if t.Kind == token.EOF {
			continue
		}
		endsPragma := inPragma && (t.Exp != nil || t.Flags.Has(token.FlagNLBefore))
		if endsPragma {
			inPragma = false
			endPragma()
		}
		if skipPragma {
			if t.Exp == nil && !t.Flags.Has(token.FlagNLBefore) {
				pragma = append(pragma, t)
				continue
			}
			skipPragma = false
			endPragma()
		}
		if opensPragmaLine(t) {
			pragmaAt = int32(b.Len())
			if opts.dropPragmaLines {
				skipPragma = true
				continue
			}
			inPragma = true
		}
		if inPragma {
			pragma = append(pragma, t)
		}
		switch {
		case !have:
		case endsPragma:
			b.WriteByte('\n')
		case t.Flags.Has(token.FlagNLBefore) && (t.Exp == nil || t.Kind == token.HASH):
			b.WriteByte('\n')
		case t.Spaced() || needSpace(prev, t):
			b.WriteByte(' ')
		}
		if opts.srcMap != nil {
			lo := int32(b.Len())
			*opts.srcMap = append(*opts.srcMap, srcSpan{
				lo: lo, hi: lo + int32(len(t.Text())), site: t.Site(),
			})
		}
		b.WriteString(t.Text())
		prev, have = t, true
	}
	endPragma() // a pragma on the last line ends at the end of the stream
	if have {
		b.WriteByte('\n')
	}
	if opts.packs != nil {
		*opts.packs = pack.out
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// opensPragmaLine reports whether t is the generated '#' doPragma mints for a
// pragma it passed through. Generated, so it belongs to no file, and at the
// start of a line, because that is where a directive begins.
func opensPragmaLine(t preprocessor.Token) bool {
	return t.Kind == token.HASH && t.Exp == nil &&
		t.Origin != nil && t.Origin.File == nil &&
		t.Flags.Has(token.FlagNLBefore)
}

var (
	spaceMu    sync.Mutex
	spaceCache = map[string]bool{}
)

// needSpace reports whether two adjacent spellings would lex as something
// other than the two tokens themselves.
//
// The cache is shared and guarded: a compiler may be driven from a worker
// pool, and this is the one piece of state a translation unit does not own.
//
// This asks the scanner rather than consulting a character-class table,
// because the table is where every preprocessor's paste-avoidance bugs live:
// `.` `.` `.` is the obvious one, `>` `>=`, `+` `++`, and hex-float `p` `+`
// are the ones that get missed. Same trick ## uses to validate a paste, and
// the result is cached because the same pair recurs constantly.
func needSpace(a, b preprocessor.Token) bool {
	joined := a.Text() + b.Text()
	spaceMu.Lock()
	v, ok := spaceCache[joined]
	spaceMu.Unlock()
	if ok {
		return v
	}
	f := token.NewFile("<paste-check>", []byte(joined+"\n"))
	toks, diags := scanner.Scan(f, 0)

	need := true
	if len(diags) == 0 && len(toks) == 3 && toks[2].Kind == token.EOF &&
		toks[0].Kind == a.Kind && toks[1].Kind == b.Kind &&
		string(f.Slice(toks[0].Pos, toks[0].End)) == a.Text() {
		need = false
	}
	spaceMu.Lock()
	spaceCache[joined] = need
	spaceMu.Unlock()
	return need
}
