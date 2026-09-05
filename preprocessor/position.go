package preprocessor

import "github.com/vertex-language/vcc/token"

// Origin identifies the position space a token's Pos and End live in.
//
// token's invariant #2 says a Pos is meaningless outside the File that
// produced it. Phase 4's output draws tokens from every file the include
// graph reached, so a bare token.Token can no longer say where it is from.
// Origin restores that, and carries the inclusion chain a diagnostic in a
// header needs to print "In file included from ...".
//
// An Origin with a nil File is the generated arena: the home of tokens that
// # and ## built and no source file contains.
type Origin struct {
	File *token.File // nil for generated tokens
	Gen  *Gen        // non-nil iff File is nil

	// Mount is the search-list entry this file was resolved against; nil for
	// the primary source file and for the generated arena. Path is the file's
	// path relative to that mount, which is what a quoted #include resolves
	// sibling headers against.
	Mount *Mount
	Path  string

	// Parent is the file that included this one, nil for the primary source
	// file and for the generated arena. IncludePos is the position of the
	// #include directive within Parent.File.
	Parent     *Origin
	IncludePos token.Pos

	// System reports whether this file was found through a mount marked
	// System. Warnings sited in a system header report once per header
	// rather than once per inclusion; see include.go.
	System bool

	// Guard is the controlling macro of an include guard, discovered when
	// the file was fully read. Empty until then, and empty forever for
	// files that are not guarded. See include.go's multiple-include check.
	Guard string
}

// Depth reports how many files enclose this one. The primary source file is 0.
func (o *Origin) Depth() int {
	n := 0
	for p := o.Parent; p != nil; p = p.Parent {
		n++
	}
	return n
}

// Name is the path as written on the command line or in the #include that
// found this file — never absolute, so __FILE__ and diagnostics do not leak
// the build machine's directory layout.
func (o *Origin) Name() string {
	switch {
	case o == nil:
		return ""
	case o.File != nil:
		return o.File.Name()
	default:
		return "<generated>"
	}
}

// Site is a span in a specific position space: enough to render one
// file:line:col without consulting anything else.
type Site struct {
	Origin   *Origin
	Pos, End token.Pos
}

// Valid reports whether s names a real position.
func (s Site) Valid() bool { return s.Origin != nil && s.Pos.IsValid() }

// Expansion records how a token came to exist. It is nil for tokens read
// straight out of a file, and non-nil for every token a macro produced.
//
// Use is the invocation site — the position a diagnostic points at, because
// that is what the user typed. Def is where this particular token sits in the
// macro's replacement list — the position the accompanying note points at.
// Outer chains through nested expansions, innermost first.
type Expansion struct {
	Macro string
	Use   Site
	Def   Site
	Outer *Expansion
}

// Root returns the outermost expansion, whose Use site is in a real file.
func (e *Expansion) Root() *Expansion {
	for e != nil && e.Outer != nil {
		e = e.Outer
	}
	return e
}

// Site returns the position a diagnostic about t should point at: the use
// site of the outermost expansion for a macro-produced token, and the token's
// own span otherwise.
func (t Token) Site() Site {
	if r := t.Exp.Root(); r != nil && r.Use.Valid() {
		return r.Use
	}
	return Site{Origin: t.Origin, Pos: t.Pos, End: t.End}
}

// Notes returns the note sites that should accompany a diagnostic about t,
// outermost expansion last, so the reader walks from the definition they are
// looking at back out to the code they wrote. Empty for ordinary tokens.
func (t Token) Notes() []Site {
	var out []Site
	for e := t.Exp; e != nil; e = e.Outer {
		if e.Def.Valid() {
			out = append(out, e.Def)
		}
	}
	return out
}
