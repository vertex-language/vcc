package vcc

import (
	"fmt"
	"strings"

	"github.com/vertex-language/vcc/preprocessor"
	"github.com/vertex-language/vcc/token"
)

// A Diagnostic is one report, sited in the file the user wrote.
//
// The site is the point of the type. Phase 4's diagnostics span every file the
// include graph reached, and phases 5-7's arrive in the preprocessed text —
// which is a different position space from the source, and reporting them
// there names a line the file only has after preprocessing. Both are mapped
// back to a Site before they leave the package, so a caller renders one thing
// one way and every diagnostic points at what was typed.
type Diagnostic struct {
	Severity token.Severity
	Site     preprocessor.Site
	Message  string

	// Name is the warning name printed in brackets; empty for errors.
	Name string

	// Notes are secondary positions: the definition site of a macro, the
	// previous declaration, the #if that was never closed.
	Notes []Note
}

// A Note is a secondary position attached below a diagnostic.
type Note struct {
	Site preprocessor.Site
	Msg  string
}

// String renders the diagnostic as file:line:col: severity: message. The
// caret under the source line is a renderer's job, not a value's: it needs
// the source text and a width to lay out against.
func (d Diagnostic) String() string {
	name := ""
	if d.Name != "" {
		name = " [" + d.Name + "]"
	}
	return fmt.Sprintf("%s: %s: %s%s", SiteString(d.Site), d.Severity, d.Message, name)
}

// SiteString renders one site as file:line:col, or names the generator of a
// token that no file produced.
func SiteString(s preprocessor.Site) string {
	if s.Origin == nil {
		return "<unknown>"
	}
	if s.Origin.File == nil {
		return s.Origin.Name() // a generated token: # or ## built it
	}
	p := s.Origin.File.Position(s.Pos)
	return fmt.Sprintf("%s:%d:%d", s.Origin.Name(), p.Line, p.Column)
}

// HasErrors reports whether any diagnostic is an error — which is the
// question every caller of a phase asks next.
func HasErrors(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == token.Error {
			return true
		}
	}
	return false
}

// A DiagnosticError is the error a compile returns when the program is wrong,
// as opposed to the invocation. Errors of every other kind mean vcc could not
// run: an unknown target, an unreadable file, a module that fails verify.
//
//	var de *vcc.DiagnosticError
//	if errors.As(err, &de) {
//		for _, d := range de.Diagnostics { … }
//	}
type DiagnosticError struct {
	Diagnostics []Diagnostic
}

func (e *DiagnosticError) Error() string {
	var b strings.Builder
	n := 0
	for _, d := range e.Diagnostics {
		if d.Severity != token.Error {
			continue
		}
		if n > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(d.String())
		n++
	}
	if n == 0 {
		return "compilation failed"
	}
	return b.String()
}

// ppDiagnostic converts one of phase 4's, which is sited already.
func ppDiagnostic(d preprocessor.Diagnostic) Diagnostic {
	out := Diagnostic{Severity: d.Severity, Site: d.Site, Message: d.Msg, Name: d.Name}
	for _, n := range d.Notes {
		out.Notes = append(out.Notes, Note{Site: n.Site, Msg: n.Msg})
	}
	return out
}

// Sited pairs diagnostics from a package that reports in one file's position
// space — scanner, parser, analyzer — with the file that owns them, so they
// can be rendered beside the ones this package returns.
func Sited(f *token.File, diags []token.Diagnostic) []Diagnostic {
	return mapDiagnostics(f, nil, diags)
}

// mapDiagnostics sites phases 5-7's diagnostics.
//
// m is the map recorded while phase 4's output was printed for the parser to
// re-read; it is nil for input that never went through phase 4, whose spans
// are already in the file the user wrote. A span that maps to nothing falls
// back to the printed text, which is still better than dropping it.
func mapDiagnostics(f *token.File, m srcMap, diags []token.Diagnostic) []Diagnostic {
	own := &preprocessor.Origin{File: f}
	out := make([]Diagnostic, 0, len(diags))
	// System-header warnings already reported, by header and message. See
	// the rule below.
	seen := map[string]bool{}
	for _, d := range diags {
		site := preprocessor.Site{Origin: own, Pos: d.Pos, End: d.End}
		if m != nil {
			// Clamped, because a span may end one past the last byte:
			// a parser reporting "expected ')'" at end of input sites it
			// on the EOF token, whose end is the position after a file
			// that has no position after it. Offset would panic, and a
			// diagnostic is the last thing that should take the compiler
			// down with it.
			if s, ok := m.site(offsetIn(f, d.Pos), offsetIn(f, d.End)); ok &&
				s.Origin != nil && s.Origin.File != nil {
				site = s
			}
		}
		// A warning sited in a system header is the header's to answer for,
		// not each #include of it: sysroot.Entry.System says such a warning
		// reports once per header, and until here only phase 4 applied it
		// (Preprocessor.warn), because the parser and the analyzer run over
		// the reparse bridge and cannot see an Origin. Here it is known.
		//
		// It is what <windows.h> costs without it. combaseapi.h, winuser.h
		// and two others each write DEFINE_ENUM_FLAG_OPERATORS(X); which is
		// nothing followed by a semicolon in C, so a hello-world that
		// includes the header collects seven identical remarks about an
		// empty external declaration in code the user did not write.
		if d.Severity == token.Warn && site.Origin != nil && site.Origin.System {
			key := site.Origin.Name() + "\x00" + d.Message
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append(out, Diagnostic{Severity: d.Severity, Site: site, Message: d.Message})
	}
	return out
}

// offsetIn is f.Offset with the range enforced rather than asserted.
func offsetIn(f *token.File, p token.Pos) int32 {
	off := int(p) - 1
	if off < 0 {
		off = 0
	}
	if off > f.Size() {
		off = f.Size()
	}
	return int32(off)
}
