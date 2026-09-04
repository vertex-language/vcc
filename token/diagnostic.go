package token

import (
	"fmt"
	"sort"
)

// Severity classifies a diagnostic. There is no non-pedantic mode:
// a constraint violation is Error because the standard says "shall".
type Severity uint8

const (
	Note Severity = iota
	Warn
	Error
)

func (s Severity) String() string {
	switch s {
	case Note:
		return "note"
	case Warn:
		return "warning"
	case Error:
		return "error"
	}
	return "severity(" + itoa(int(s)) + ")"
}

// Diagnostic is one report with a non-empty span in some File's
// position space. The File that owns the span must render it.
type Diagnostic struct {
	Pos      Pos
	End      Pos
	Severity Severity
	Message  string
}

// Print renders the diagnostic through the File that owns its span:
// name:line:col: severity: message, in raw (as-typed) coordinates.
func (d Diagnostic) Print(f *File) string {
	p := f.Position(d.Pos)
	return fmt.Sprintf("%s:%d:%d: %s: %s", p.Filename, p.Line, p.Column, d.Severity, d.Message)
}

// SortDiagnostics orders by position, then extent, then message,
// stably — so merged phase 1–2, scanner, and parser slices interleave
// deterministically.
func SortDiagnostics(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Pos != b.Pos {
			return a.Pos < b.Pos
		}
		if a.End != b.End {
			return a.End < b.End
		}
		return a.Message < b.Message
	})
}