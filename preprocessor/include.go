package preprocessor

import (
	"io/fs"
	"path"
	"strings"

	"github.com/vertex-language/vcc/token"
)

// Deps accumulates what --deps writes: the target, and every file #include
// reached, in first-seen order.
type Deps struct {
	Target string
	Files  []string
	seen   map[string]bool
}

func (d *Deps) add(name string) {
	if d == nil {
		return
	}
	if d.seen == nil {
		d.seen = map[string]bool{}
	}
	if !d.seen[name] {
		d.seen[name] = true
		d.Files = append(d.Files, name)
	}
}

// Write renders the Makefile fragment build systems consume: one rule, then a
// phony target per header so a deleted header does not break the rebuild. This
// is -MMD -MF -MP collapsed to the one shape that is actually read.
func (d *Deps) Write(w *strings.Builder) {
	w.WriteString(d.Target)
	w.WriteString(":")
	for _, f := range d.Files {
		w.WriteString(" \\\n  ")
		w.WriteString(escapeMake(f))
	}
	w.WriteString("\n")
	for _, f := range d.Files[1:] {
		w.WriteString("\n")
		w.WriteString(escapeMake(f))
		w.WriteString(":\n")
	}
}

func escapeMake(s string) string {
	return strings.NewReplacer(" ", `\ `, "#", `\#`, "$", "$$").Replace(s)
}

// cached is one entry of the open-once cache. Content is read at most once per
// translation unit; Guard is the controlling macro discovered when the file was
// fully read, and is what lets the second #include skip the file entirely.
//
// diags holds the phases 1–3 diagnostics scanning produced, deferred:
// they are reported on the first read, through the real Origin, so they
// carry the inclusion chain and the System treatment — open() has
// neither.
type cached struct {
	file  *token.File
	toks  []Token
	diags []token.Diagnostic
	guard string
	done  bool

	// once is set when the file said #pragma once while it was read. Unlike
	// guard it needs no macro to still be defined to hold: the file asked
	// not to be read again, and nothing the program does afterwards
	// withdraws the request.
	once bool
}

// doInclude implements §6.10.2.
func (p *Preprocessor) doInclude(r *reader, line []Token, at Site) {
	name, angled, ok := p.headerName(line, at)
	if !ok {
		return
	}
	if len(p.stack) >= p.cfg.MaxIncludeDepth {
		p.errorf(at, "#include nested too deeply (limit %d)", p.cfg.MaxIncludeDepth)
		p.note(at, "a header that includes itself needs an include guard")
		return
	}
	p.include(r, name, angled, at, false)
}

// doIncludeNext is gcc's #include_next: the same search, resumed after the
// directory this file was found in.
//
// It exists because a header may want to wrap the one it shadows — glibc's
// <limits.h> includes the compiler's, and a build that puts its own
// <stdio.h> ahead of the platform's still wants the platform's underneath.
// There is no ISO spelling for that, and a header that uses it cannot be
// rewritten by the person compiling it.
func (p *Preprocessor) doIncludeNext(r *reader, line []Token, at Site) {
	name, angled, ok := p.headerName(line, at)
	if !ok {
		return
	}
	if len(p.stack) >= p.cfg.MaxIncludeDepth {
		p.errorf(at, "#include_next nested too deeply (limit %d)", p.cfg.MaxIncludeDepth)
		return
	}
	p.include(r, name, angled, at, true)
}

// headerName recovers the header-name pp-token, which phase 3 cannot produce:
// <stdio.h> scans as LSS IDENT DOT IDENT GTR everywhere except here, and only
// this call site knows the context.
//
// The quoted form is one STRING_LIT and needs no reconstruction. The angled
// form is rebuilt from the raw bytes between '<' and '>', not from the token
// spellings, because the characters between them are not tokens: <sys/types.h>
// must survive with its slash and dot intact.
func (p *Preprocessor) headerName(line []Token, at Site) (name string, angled, ok bool) {
	if len(line) == 0 {
		p.errorf(at, "#include expects \"FILENAME\" or <FILENAME>")
		return "", false, false
	}
	switch {
	case line[0].Kind == token.STRING_LIT && !strings.HasPrefix(line[0].Text(), "u") &&
		!strings.HasPrefix(line[0].Text(), "L") && !strings.HasPrefix(line[0].Text(), "U"):
		p.expectEnd(line[1:], "#include")
		return strings.Trim(line[0].Text(), `"`), false, true

	case line[0].Kind == token.LSS:
		for i := 1; i < len(line); i++ {
			if line[i].Kind != token.GTR {
				continue
			}
			org := line[0].Origin
			if org == nil || org.File == nil {
				break
			}
			p.expectEnd(line[i+1:], "#include")
			return string(org.File.Slice(line[0].End, line[i].Pos)), true, true
		}
		p.errorf(at, "missing '>' in #include")
		return "", false, false
	}

	// Neither form: the line is a macro that expands to one. §6.10.2p4.
	expanded := p.expandClosed(line)
	if len(expanded) > 0 && !sameTokens(expanded, line) {
		return p.headerName(expanded, at)
	}
	p.errorf(at, "#include expects \"FILENAME\" or <FILENAME>")
	return "", false, false
}

func sameTokens(a, b []Token) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || a[i].Text() != b[i].Text() {
			return false
		}
	}
	return true
}

// include resolves and reads a header.
//
// The search is one list. A quoted include looks in the including file's own
// directory first and then walks it; an angled include skips that first step.
// That is exactly what ISO describes, with no -iquote/-isystem/-idirafter
// tower layered on top.
func (p *Preprocessor) include(r *reader, name string, angled bool, at Site, next bool) {
	if path.IsAbs(name) {
		p.errorf(at, "absolute path in #include: %q", name)
		p.note(at, "use -I and a relative path so the build is reproducible")
		return
	}

	// #include_next resumes the list after the entry this file came from, so
	// a header can reach the one it shadows. A file found outside the search
	// list — the primary source — has no position in it, and the search runs
	// from the top.
	start := 0
	if next {
		for i := range p.cfg.Search {
			if r.org.Mount == &p.cfg.Search[i] {
				start = i + 1
				break
			}
		}
	}

	var mounts []*Mount
	var rels []string
	if !angled && !next && r.org.Mount != nil {
		dir := path.Dir(r.org.Path)
		mounts = append(mounts, r.org.Mount)
		rels = append(rels, path.Join(dir, name))
	}
	for i := start; i < len(p.cfg.Search); i++ {
		mounts = append(mounts, &p.cfg.Search[i])
		rels = append(rels, name)
	}

	for i, m := range mounts {
		rel := path.Clean(rels[i])
		if strings.HasPrefix(rel, "..") {
			continue
		}
		display := path.Join(m.Name, rel)
		c, err := p.open(m, rel, display)
		if err != nil {
			continue
		}
		p.deps.add(display)

		// The multiple-include optimization: a file whose entire contents sit
		// inside #ifndef GUARD ... #endif, with nothing outside, need not be
		// opened again while GUARD is defined. A file that said #pragma once
		// need not be opened again at all — it asked, and there is no macro
		// standing behind the request that the program could undefine.
		if c.done && (c.once || (c.guard != "" && p.macros.Defined(c.guard))) {
			return
		}
		p.readFile(c, m, rel, display, at, r.org)
		return
	}

	if next {
		// Nothing further down the list has it, which is what a wrapper
		// header at the bottom of the list will find. Saying nothing is
		// wrong; naming it as not found is right.
		p.errorf(at, "%q file not found after this directory", name)
		return
	}
	p.errorf(at, "%q file not found", name)
	if len(p.cfg.Search) == 0 {
		p.note(at, "no include directories are configured; use -I")
	} else {
		p.note(at, "searched %d directories; run `vcc env` to see the resolved list", len(p.cfg.Search))
	}
}

// open reads a file at most once per translation unit. Scanning
// diagnostics are stashed, not reported: they wait for readFile,
// where an Origin with the inclusion chain exists.
func (p *Preprocessor) open(m *Mount, rel, display string) (*cached, error) {
	if c, ok := p.files[display]; ok {
		return c, nil
	}
	src, err := fs.ReadFile(m.FS, rel)
	if err != nil {
		return nil, err
	}
	f := token.NewFile(display, src)
	toks, diags := scanPP(f)
	c := &cached{file: f, diags: diags}
	c.toks = p.wrap(toks, nil) // origin attached per-read, below
	p.files[display] = c
	return c, nil
}

func (p *Preprocessor) readFile(c *cached, m *Mount, rel, display string, at Site, parent *Origin) {
	org := &Origin{
		File:       c.file,
		Mount:      m,
		Path:       rel,
		Parent:     parent,
		IncludePos: at.Pos,
		System:     m.System,
		Guard:      c.guard,
	}
	toks := make([]Token, len(c.toks))
	copy(toks, c.toks)
	for i := range toks {
		toks[i].Origin = org
	}
	r := &reader{org: org, toks: toks, miValid: true}

	// Phases 1–3 diagnostics, deferred from open(): reported here, on
	// the first read only, through the real Origin — so they carry
	// the include chain and the System treatment. A scanner mistake
	// in a header is the header's, once, like any other diagnostic.
	if !c.done {
		for _, d := range c.diags {
			p.fromScan(org, d)
		}
	}

	p.stack = append(p.stack, r)
	p.run(r)
	p.stack = p.stack[:len(p.stack)-1]

	// Record what we learned, so the next #include of this file can skip it.
	if !c.done {
		c.done = true
		c.guard = r.guardFound
		c.once = r.once
	}
}
