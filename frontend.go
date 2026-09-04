package vcc

import (
	"strings"

	"github.com/vertex-language/vcc/analyzer"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/parser"
	"github.com/vertex-language/vcc/preprocessor"
	"github.com/vertex-language/vcc/token"
)

// Source is the translation unit as the parser will read it: the input, run
// through phase 4 when it should be, with the include graph flattened into one
// position space.
//
// It is the rung a tool that wants to scan or dump the preprocessed program
// stops at — `scanner.Scan` over what comes back is exactly what the parser
// sees. Positions in it are the preprocessed text's own; the rungs that carry
// a unit all the way through are the ones that map diagnostics back.
func (c *Compiler) Source(in Input) (*token.File, []Diagnostic, error) {
	f, _, _, diags, err := c.source(in)
	if err != nil {
		return nil, nil, err
	}
	return f, c.report(diags), nil
}

// Preprocess runs phase 4 and returns its output as C source — the artifact
// `--emit i` writes.
//
// Unlike the bridge behind Source this keeps pragma lines, and an input that
// was already preprocessed comes back byte for byte: the guarantee is that the
// output re-enters as .i input and produces the same program, and both a
// dropped #pragma and re-spaced whitespace break it.
//
// Diagnostics come back whether or not they are errors. Output produced
// alongside an error is what phase 4 got to before giving up; a caller writing
// an artifact should check HasErrors first.
func (c *Compiler) Preprocess(in Input) ([]byte, []Diagnostic, error) {
	raw, toks, ran, diags, err := c.stream(in)
	if err != nil {
		return nil, nil, err
	}
	if !ran {
		return raw.Source(), c.report(diags), nil
	}
	var b strings.Builder
	if err := printTokens(&b, toks, printOpts{}); err != nil {
		return nil, nil, err
	}
	return []byte(b.String()), c.report(diags), nil
}

// Parse produces the syntax tree, with no analysis over it.
//
// The tree owns its storage: call file.Release when finished with it, after
// which every node is invalid. This is the one rung that hands the arena to a
// caller, because it is the one whose caller asked for the tree; everything
// above it releases internally.
//
// ast.File carries the position space its spans resolve through, as File.Unit.
func (c *Compiler) Parse(in Input, mode parser.Mode) (*ast.File, []Diagnostic, error) {
	f, _, _, diags, err := c.source(in)
	if err != nil {
		return nil, nil, err
	}
	tree, parseDiags := parser.ParseFile(f, mode)
	diags = append(diags, mapDiagnostics(f, nil, parseDiags)...)
	return tree, c.report(diags), nil
}

// Check runs the front end — load, preprocess, parse, analyze — and reports
// what it found. No diagnostics is success, and a warning is not a failure,
// which is why this returns them rather than an error: reporting them is the
// operation.
func (c *Compiler) Check(in Input) ([]Diagnostic, error) {
	u, err := c.frontend(in)
	if err != nil {
		return nil, err
	}
	defer u.release()
	return c.report(u.diagnostics()), nil
}

// ---- the shared middle ----

// unit is one translation unit carried through the front end.
type unit struct {
	target Target
	file   *token.File // the position space every diagnostic resolves through
	smap   srcMap      // that space mapped back to the source the user wrote
	tree   *ast.File
	info   *analyzer.Info

	pp    []Diagnostic       // phase 4's, sited already
	diags []token.Diagnostic // parse, analysis and lowering, merged and sorted
	ppErr bool               // phase 4 reported an error
}

// frontend runs phases 1-7's analysis half over one input.
//
// The analyzer runs even after parse errors: a partial parse is a usable one,
// and Bad* nodes analyze silently — so the diagnostic set stays "each mistake
// exactly once" rather than "everything after the first is silence".
func (c *Compiler) frontend(in Input) (*unit, error) {
	t, err := c.target()
	if err != nil {
		return nil, err
	}
	f, smap, packs, pp, err := c.source(in)
	if err != nil {
		return nil, err
	}

	tree, parseDiags := parser.ParseFile(f, parser.DefaultMode)
	info, checkDiags := analyzer.Check(f, tree, t.Model(), packs)

	diags := make([]token.Diagnostic, 0, len(parseDiags)+len(checkDiags))
	diags = append(diags, parseDiags...)
	diags = append(diags, checkDiags...)
	token.SortDiagnostics(diags)

	return &unit{
		target: t, file: f, smap: smap, tree: tree, info: info,
		pp: pp, diags: diags, ppErr: HasErrors(pp),
	}, nil
}

// failed reports whether the front end found an error, without rendering
// anything. It is separate from diagnostics because the decision it feeds —
// whether to lower — has to be made before the diagnostics are handed out.
func (u *unit) failed() bool {
	if u.ppErr {
		return true
	}
	for _, d := range u.diags {
		if d.Severity == token.Error {
			return true
		}
	}
	return false
}

// diagnostics is every diagnostic this unit produced, sited: phase 4's first,
// in the order it found them, then the rest mapped back out of the
// preprocessed text.
func (u *unit) diagnostics() []Diagnostic {
	out := make([]Diagnostic, 0, len(u.pp)+len(u.diags))
	out = append(out, u.pp...)
	return append(out, mapDiagnostics(u.file, u.smap, u.diags)...)
}

// release returns the tree's backing storage. Every node is invalid
// afterwards, so callers copy what they need first — which for a unit that has
// already produced its artifact is nothing.
func (u *unit) release() {
	if u.tree != nil {
		u.tree.Release()
	}
}

// stream loads one input and runs phase 4 over it when it should.
//
// raw is the file as loaded — the position space phase 4's own diagnostics
// resolve through. toks is phase 4's output, and ran says whether there was
// any: that is the difference Preprocess needs, since an .i file passes
// through untouched rather than being re-printed from a scan of itself.
//
// ran is a flag rather than a nil check on toks, because phase 4 returns no
// tokens for a file whose every line was skipped — `#ifdef NEVER` around the
// whole of it — and reading that as "was not preprocessed" hands the parser
// the raw file, directives and all.
//
// A fresh Preprocessor per input is not an accident: the macro table is per
// translation unit, and two inputs are two of them.
func (c *Compiler) stream(in Input) (raw *token.File, toks []preprocessor.Token, ran bool, diags []Diagnostic, err error) {
	raw, err = in.load()
	if err != nil {
		return nil, nil, false, nil, err
	}
	if !in.preprocessed(c.PP) {
		return raw, nil, false, nil, nil
	}
	cfg, _, err := c.config()
	if err != nil {
		return nil, nil, false, nil, err
	}
	// Per input, not per compiler: two primary files in two directories each
	// look for their quoted includes beside themselves. config is shared and
	// cached, so this is set on a copy.
	cfg.Source = in.mount()
	pre := preprocessor.New(cfg)
	toks, ppDiags := pre.Run(raw)
	for _, d := range ppDiags {
		diags = append(diags, ppDiagnostic(d))
	}
	return raw, toks, true, diags, nil
}

// source is stream plus the reparse bridge: the unit the parser reads, and the
// map from its positions back to the ones the user wrote.
func (c *Compiler) source(in Input) (*token.File, srcMap, []analyzer.PackAt, []Diagnostic, error) {
	raw, toks, ran, diags, err := c.stream(in)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if !ran {
		// Already preprocessed: the positions are the file's own, and so
		// are the offsets of the #pragma pack lines still in it — a pragma
		// survives preprocessing everywhere, because acting on one is the
		// compiler's job and not phase 4's. See pppack.go.
		return raw, nil, packsInSource(raw), diags, nil
	}
	f, m, packs, err := reparse(in, toks)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return f, m, packs, diags, nil
}

// reparse is the bridge until parser.ParseTokens exists.
//
// Phase 4's output spans every file the include graph reached; ParseFile takes
// one *token.File. So the stream is printed and re-scanned as .i input — the
// round trip Preprocess already promises, used as plumbing. The cost is the
// position map, which is why one is recorded.
func reparse(in Input, toks []preprocessor.Token) (*token.File, srcMap, []analyzer.PackAt, error) {
	var b strings.Builder
	var m srcMap
	var packs []analyzer.PackAt
	if err := printTokens(&b, toks, printOpts{dropPragmaLines: true, srcMap: &m, packs: &packs}); err != nil {
		return nil, nil, nil, err
	}
	return token.NewFile(in.name(), []byte(b.String())), m, packs, nil
}
