// Package preprocessor implements translation phase 4 of ISO C11 §6.10:
// directive execution, macro replacement, and include resolution.
//
// The package is a pure function of its Config. It reads fs.FS mounts handed
// to it and never touches os, so where headers come from is decided in exactly
// one other place, and phase 4 is testable against fstest.MapFS with no host
// involved.
//
// The directive grammar lives here and nowhere else in the tree. On .i input
// this package is skipped entirely and a '#' opening a line is trivia.
package preprocessor

import (
	"fmt"
	"path"

	"github.com/vertex-language/vcc/scanner"
	"github.com/vertex-language/vcc/token"
)

// Diagnostic is phase 4's diagnostic. It is not token.Diagnostic because a
// token.Diagnostic is a span with no file, and phase 4's output spans every
// file the include graph reached.
type Diagnostic struct {
	Severity token.Severity
	Site     Site
	Msg      string
	Name     string // the warning name printed in brackets; empty for errors
	Notes    []Note
}

// Note is a secondary position attached below a diagnostic: the definition
// site of a macro, the previous definition, the #if that was never closed.
type Note struct {
	Site Site
	Msg  string
}

// reader is the per-file state of the directive walk.
type reader struct {
	org   *Origin
	toks  []Token
	i     int
	conds []cond

	// Multiple-include optimization, gcc's scheme. miValid stays true only
	// while nothing has happened that could invalidate an include guard:
	// no token outside a conditional, and no directive other than an opening
	// conditional or the null directive. sawToken records the first text
	// token, which closes the window in which a guard may begin.
	miValid      bool
	sawToken     bool
	guardFound   string
	pendingGuard string // guard of the outermost #endif just closed

	// once records that this file asked not to be read again, with
	// #pragma once. It is the other half of the multiple-include
	// optimization and is not subject to miValid: the pragma states the
	// conclusion outright instead of leaving it to be inferred from the
	// file's shape, so a token before it takes nothing away.
	once bool
}

// Preprocessor holds one translation unit's phase-4 state.
type Preprocessor struct {
	// counter is __COUNTER__'s next value. It counts expansions over the
	// whole translation unit, never resetting per file, which is what makes
	// the name it builds unique.
	counter int

	cfg    Config
	macros *Table
	gen    *Gen
	deps   *Deps

	files map[string]*cached
	stack []*reader
	out   []Token
	diags []Diagnostic

	pasteCache map[string]struct {
		kind token.Kind
		ok   bool
	}

	depth     int
	lineDelta int
	fileName  string
	warnedGNU map[string]bool
}

// New returns a preprocessor configured for one translation unit.
func New(cfg Config) *Preprocessor {
	cfg = cfg.Default()
	p := &Preprocessor{
		cfg:       cfg,
		macros:    NewTable(),
		gen:       NewGen(),
		files:     map[string]*cached{},
		warnedGNU: map[string]bool{},
		pasteCache: map[string]struct {
			kind token.Kind
			ok   bool
		}{},
	}
	if cfg.TrackDeps {
		p.deps = &Deps{}
	}
	p.installPredefines()
	return p
}

// Run preprocesses one primary source file and returns the token stream phase
// 5 reads, plus every diagnostic phases 1 through 4 produced.
//
// The result is a single sequence drawn from many files; each token carries
// the Origin its span belongs to, so nothing downstream has to guess.
func (p *Preprocessor) Run(f *token.File) ([]Token, []Diagnostic) {
	org := &Origin{File: f}
	if p.cfg.Source.FS != nil {
		// Give the primary file the same standing a header has: a mount and a
		// path within it, so a quoted #include resolves beside it.
		org.Mount = &p.cfg.Source
		org.Path = path.Base(f.Name())
	}
	toks, diags := scanPP(f)
	for _, d := range diags {
		p.fromToken(f, d)
	}
	if p.deps != nil {
		p.deps.Target = f.Name()
		p.deps.add(f.Name())
	}

	r := &reader{org: org, toks: p.wrap(toks, org), miValid: true}
	p.stack = append(p.stack, r)

	for _, inc := range p.cfg.PreIncludes {
		p.include(r, inc, false, Site{Origin: org, Pos: f.Pos(0), End: f.Pos(1)}, false)
	}
	p.run(r)
	p.stack = p.stack[:len(p.stack)-1]

	sortDiagnostics(p.diags)
	return p.out, p.diags
}

// Macros exposes the table after a run, for `vcc env` and for tests.
func (p *Preprocessor) Macros() *Table { return p.macros }

// Deps returns the recorded dependency set, or nil when TrackDeps was false.
func (p *Preprocessor) Deps() *Deps { return p.deps }

// run walks one file: directive lines are executed, text lines are expanded.
func (p *Preprocessor) run(r *reader) {
	for {
		t, ok := r.peek()
		if !ok {
			break
		}
		if t.Kind == token.HASH && t.StartsLine() {
			r.next()
			p.directive(r, t, r.takeLine())
			continue
		}
		if r.skipping() {
			r.skipLine()
			continue
		}
		r.sawToken = true
		r.miValid = false
		p.expandText(r)
	}
	r.finishConds(p)

	// The file was fully read with a guard still standing: record it so the
	// next #include of this file can skip opening it.
	if r.miValid && r.guardFound == "" {
		r.guardFound = r.pendingGuard
	}
}

// expandText expands from the current position to the next directive.
//
// The stream stops at a line-opening '#', which is what makes a macro
// invocation unable to straddle a directive: the argument list simply runs out
// of tokens and is reported as unterminated, rather than silently swallowing
// an #endif.
func (p *Preprocessor) expandText(r *reader) {
	s := &stream{more: func() (Token, bool) {
		t, ok := r.peek()
		if !ok || (t.Kind == token.HASH && t.StartsLine()) {
			return Token{}, false
		}
		r.next()
		return t, true
	}}
	p.out = p.expandInto(s, p.out)
	// Anything the expander read but did not consume goes back.
	r.unread(s.buf[s.i:])
}

func (r *reader) peek() (Token, bool) {
	for r.i < len(r.toks) && r.toks[r.i].Kind == token.EOF {
		r.i++
	}
	if r.i >= len(r.toks) {
		return Token{}, false
	}
	return r.toks[r.i], true
}

func (r *reader) next() (Token, bool) {
	t, ok := r.peek()
	if ok {
		r.i++
	}
	return t, ok
}

func (r *reader) unread(ts []Token) {
	if len(ts) == 0 {
		return
	}
	rest := append(append([]Token(nil), ts...), r.toks[r.i:]...)
	r.toks, r.i = rest, 0
}

// takeLine returns the rest of the logical line. Phase 2 already spliced
// backslash-newline away, so "the rest of the line" is exactly the tokens up
// to the next one carrying FlagNLBefore.
func (r *reader) takeLine() []Token {
	start := r.i
	for r.i < len(r.toks) {
		t := r.toks[r.i]
		if t.Kind == token.EOF {
			break
		}
		if r.i > start && t.StartsLine() {
			break
		}
		r.i++
	}
	return r.toks[start:r.i]
}

func (r *reader) skipLine() { r.takeLine() }

// wrap lifts scanner output into phase 4's token type.
func (p *Preprocessor) wrap(toks []token.Token, org *Origin) []Token {
	out := make([]Token, 0, len(toks))
	for _, t := range toks {
		if t.Kind == token.COMMENT && !p.cfg.KeepComments {
			continue
		}
		out = append(out, Token{
			Kind:   t.Kind,
			Flags:  t.Flags,
			Pos:    t.Pos,
			End:    t.End,
			Origin: org,
		})
	}
	return out
}

// scanPP runs the scanner in preprocessing mode.
//
// ScanPP differs from the ordinary mode in three ways, all of them because a
// pp-token is not yet a C token: pp-numbers keep their spans without being
// classified as integer or floating constants, malformed-literal diagnostics
// are deferred to phase 7 (#if 0 may legally hide what phase 7 would reject),
// and a line-opening '#' is returned as HASH rather than swallowed as trivia.
func scanPP(f *token.File) ([]token.Token, []token.Diagnostic) {
	return scanner.Scan(f, scanner.ScanPP)
}

func (p *Preprocessor) errorf(s Site, format string, a ...any) {
	p.diags = append(p.diags, Diagnostic{
		Severity: token.Error,
		Site:     s,
		Msg:      fmt.Sprintf(format, a...),
	})
}

// warn reports a warning and reports whether it was emitted. A warning
// sited in a system header is the header's mistake, not each #include of
// it: report once per header, per name. Callers attaching a note must
// check the return — note() binds to the last diagnostic appended,
// whatever that is, so a note after a suppressed warning would attach
// to something unrelated.
func (p *Preprocessor) warn(name string, s Site, format string, a ...any) bool {
	if s.Origin != nil && s.Origin.System {
		key := s.Origin.Name() + "\x00" + name
		if p.warnedGNU[key] {
			return false
		}
		p.warnedGNU[key] = true
	}
	p.diags = append(p.diags, Diagnostic{
		Severity: token.Warn,
		Site:     s,
		Name:     name,
		Msg:      fmt.Sprintf(format, a...),
	})
	return true
}

// note attaches to the diagnostic just reported.
func (p *Preprocessor) note(s Site, format string, a ...any) {
	if len(p.diags) == 0 {
		return
	}
	d := &p.diags[len(p.diags)-1]
	d.Notes = append(d.Notes, Note{Site: s, Msg: fmt.Sprintf(format, a...)})
}

// fromScan lifts a phases 1–3 diagnostic into phase 4's position
// space under the given Origin, preserving severity. Warnings route
// through warn() under the name "scan", so a warning in a system
// header (a trigraph, say) reports once per header, the same
// treatment every phase-4 warning gets.
func (p *Preprocessor) fromScan(org *Origin, d token.Diagnostic) {
	site := Site{Origin: org, Pos: d.Pos, End: d.End}
	if d.Severity == token.Warn {
		p.warn("scan", site, "%s", d.Message)
		return
	}
	p.diags = append(p.diags, Diagnostic{
		Severity: d.Severity,
		Site:     site,
		Msg:      d.Message,
	})
}

// fromToken lifts a primary-source-file diagnostic: the file is its
// own origin, with no inclusion chain. Included files go through
// fromScan with the Origin readFile builds.
func (p *Preprocessor) fromToken(f *token.File, d token.Diagnostic) {
	p.fromScan(&Origin{File: f}, d)
}

// sortDiagnostics orders by file, then position, then extent, stably — the
// same contract token.SortDiagnostics holds within one file, extended across
// the include graph so a run is byte-identical every time.
func sortDiagnostics(ds []Diagnostic) {
	// Insertion sort: diagnostic counts are small, and stability is required.
	for i := 1; i < len(ds); i++ {
		for j := i; j > 0 && less(ds[j], ds[j-1]); j-- {
			ds[j], ds[j-1] = ds[j-1], ds[j]
		}
	}
}

func less(a, b Diagnostic) bool {
	an, bn := a.Site.Origin.Name(), b.Site.Origin.Name()
	if an != bn {
		return an < bn
	}
	if a.Site.Pos != b.Site.Pos {
		return a.Site.Pos < b.Site.Pos
	}
	return a.Site.End < b.Site.End
}