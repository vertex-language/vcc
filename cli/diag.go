package cli

import (
	"bytes"
	"fmt"
	"io"

	"github.com/vertex-language/vcc"
	"github.com/vertex-language/vcc/preprocessor"
	"github.com/vertex-language/vcc/token"
)

// One renderer, for one kind of diagnostic.
//
// Every diagnostic the library returns is sited in a real file — phase 4's
// where it found them, and the later phases' mapped back out of the
// preprocessed text — so there is nothing left here to decide. What is left is
// presentation: the source line, the caret under it, the notes below it, and
// the chain of #includes that reached the file.

// printDiags renders each diagnostic and reports whether any was an error.
func printDiags(w io.Writer, diags []vcc.Diagnostic) bool {
	for _, d := range diags {
		fmt.Fprintln(w, d.String())
		printSiteSnippet(w, d.Site)
		for _, n := range d.Notes {
			fmt.Fprintf(w, "%s: note: %s\n", vcc.SiteString(n.Site), n.Msg)
			printSiteSnippet(w, n.Site)
		}
		printIncludeChain(w, d.Site)
	}
	return vcc.HasErrors(diags)
}

func printSiteSnippet(w io.Writer, s preprocessor.Site) {
	if s.Origin == nil || s.Origin.File == nil || !s.Pos.IsValid() {
		return
	}
	printSnippetAt(w, s.Origin.File, s.Pos, s.End)
}

// printIncludeChain prints gcc's "In file included from" trail, outermost
// last, so the reader walks from the header back to the file they compiled.
//
// Each Origin's IncludePos lives in its Parent's file — token's invariant #2
// says a Pos is meaningless outside the File that produced it — so the walk
// advances the child alongside the parent and never reuses a Pos across
// position spaces.
func printIncludeChain(w io.Writer, s preprocessor.Site) {
	if s.Origin == nil {
		return
	}
	for child := s.Origin; child.Parent != nil; child = child.Parent {
		parent := child.Parent
		if parent.File == nil {
			continue
		}
		p := parent.File.Position(child.IncludePos)
		fmt.Fprintf(w, "    in file included from %s:%d\n", parent.Name(), p.Line)
	}
}

// printSnippetAt underlines one span, in Raw (as-typed) coordinates, so the
// caret lands on what the user wrote even through trigraphs and splices.
func printSnippetAt(w io.Writer, f *token.File, pos, end token.Pos) {
	src := f.Source()
	p := f.Position(pos)

	// The raw line containing the start of the span.
	lo := p.Offset - (p.Column - 1)
	hi := p.Offset
	for hi < len(src) && src[hi] != '\n' && src[hi] != '\r' {
		hi++
	}
	line := src[lo:hi]

	// Underline width: the raw extent, clamped to this line. Raw widens over
	// splices and trigraphs, so ??< underlines all three bytes.
	width := len(f.Raw(pos, end))
	if p.Column-1+width > len(line) {
		width = len(line) - (p.Column - 1)
	}
	if width < 1 {
		width = 1
	}

	// Tabs stay tabs in the pad line so the caret column matches.
	pad := make([]byte, p.Column-1)
	for i := range pad {
		if line[i] == '\t' {
			pad[i] = '\t'
		} else {
			pad[i] = ' '
		}
	}

	fmt.Fprintf(w, "    %s\n    %s%s\n", line, pad, bytes.Repeat([]byte("^"), width))
}
