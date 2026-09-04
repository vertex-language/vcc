package preprocessor

import "github.com/vertex-language/vcc/token"

// Builtin marks the macros whose replacement list is computed rather than
// stored. They are not in the macro table's ordinary sense — ts() consults
// predefined.go for these — but they occupy names, can be #undef'd, and can
// be shadowed by a #define, so they live in the same table.
type Builtin uint8

const (
	NotBuiltin Builtin = iota
	BuiltinFile
	BuiltinLine
	BuiltinDate
	BuiltinTime
	BuiltinCounter
	BuiltinSTDC // __STDC__, __STDC_VERSION__, __STDC_HOSTED__ are plain values
)

// Macro is one entry in the macro table.
//
// Params holds the formal parameter names in order — Prosser's fp(T). For a
// variadic macro the trailing __VA_ARGS__ is not in Params; Variadic records
// it, and select() maps the last formal to everything past the named ones.
// Body is the replacement list — Prosser's ts(T) — with the parameters left
// as ordinary IDENT tokens, looked up by name during subst.
//
// ObjLike distinguishes #define M x from #define M() x. The difference is not
// len(Params): a function-like macro with an empty parameter list has zero
// params and is still only invoked when followed by '('.
type Macro struct {
	ID       int // dense, assigned on definition; hide sets store these
	Name     string
	ObjLike  bool
	Variadic bool
	Params   []string
	Body     []Token

	Def     Site
	Builtin Builtin

	// Used records whether the macro was ever expanded or tested. Nothing in
	// the language depends on it; the unused-macro warning does.
	Used bool
}

// Param reports the index of name in the macro's formal parameters, or -1.
// __VA_ARGS__ resolves to the variadic slot, which sits one past the named
// parameters.
func (m *Macro) Param(name string) int {
	if m.ObjLike {
		return -1
	}
	for i, p := range m.Params {
		if p == name {
			return i
		}
	}
	if m.Variadic && name == "__VA_ARGS__" {
		return len(m.Params)
	}
	return -1
}

// Arity is the number of argument slots select() must fill: the named
// parameters plus the variadic slot when there is one.
func (m *Macro) Arity() int {
	if m.Variadic {
		return len(m.Params) + 1
	}
	return len(m.Params)
}

// Table is the macro table: names to definitions, plus the ID allocator hide
// sets index into.
//
// The table is per-translation-unit state, not per-file: a #define in a header
// outlives the header, which is the whole point of a header.
type Table struct {
	byName map[string]*Macro
	next   int
}

// NewTable returns an empty table.
func NewTable() *Table {
	return &Table{byName: make(map[string]*Macro, 512)}
}

// Lookup returns the macro named name, or nil.
func (t *Table) Lookup(name string) *Macro { return t.byName[name] }

// Defined reports whether name is currently defined — the `defined` operator's
// question, and #ifdef's.
func (t *Table) Defined(name string) bool { _, ok := t.byName[name]; return ok }

// Define enters m, assigning its ID. It returns the macro previously bound to
// the name, if any, so the caller can apply §6.10.3p2: a redefinition is
// permitted only when the two definitions are identical, and must be reported
// otherwise. Define itself does not report — directive.go owns diagnostics.
func (t *Table) Define(m *Macro) (prev *Macro) {
	prev = t.byName[m.Name]
	if prev != nil {
		// Reuse the ID so hide sets captured under the old definition still
		// name this macro. A redefinition that reaches here is either
		// identical (harmless) or already reported.
		m.ID = prev.ID
	} else {
		m.ID = t.next
		t.next++
	}
	t.byName[m.Name] = m
	return prev
}

// Undef removes name and reports whether it was bound. §6.10.3.5p2: undefining
// a name that is not defined is not an error.
func (t *Table) Undef(name string) bool {
	if _, ok := t.byName[name]; !ok {
		return false
	}
	delete(t.byName, name)
	return true
}

// Len reports how many macros are defined.
func (t *Table) Len() int { return len(t.byName) }

// Each calls f for every defined macro, in unspecified order.
func (t *Table) Each(f func(*Macro)) {
	for _, m := range t.byName {
		f(m)
	}
}

// SameDefinition implements §6.10.3p2's identity test: two definitions are the
// same if they have the same kind, the same spelling of parameters in the same
// order, and replacement lists that agree token by token in spelling and in
// whitespace separation. "Whitespace separation" is presence, not amount, so
// this compares FlagAdjacent and not the trivia itself.
func SameDefinition(a, b *Macro) bool {
	if a.ObjLike != b.ObjLike || a.Variadic != b.Variadic || len(a.Params) != len(b.Params) {
		return false
	}
	for i := range a.Params {
		if a.Params[i] != b.Params[i] {
			return false
		}
	}
	if len(a.Body) != len(b.Body) {
		return false
	}
	for i := range a.Body {
		x, y := a.Body[i], b.Body[i]
		if x.Kind != y.Kind || x.Text() != y.Text() {
			return false
		}
		// The first token of a replacement list is separated from the
		// '#define' line by definition; only interior separation counts.
		if i > 0 && x.Spaced() != y.Spaced() {
			return false
		}
	}
	return true
}

// Reserved reports whether name is one the program may not #define or #undef.
// §6.10.8p2 forbids both for the standard predefined macros and for `defined`;
// the target and feature macros predefined.go installs are ordinary macros the
// program may legally shadow, so they are not listed here.
func Reserved(name string) bool {
	switch name {
	case "defined",
		"__FILE__", "__LINE__", "__DATE__", "__TIME__",
		"__STDC__", "__STDC_VERSION__", "__STDC_HOSTED__":
		return true
	}
	return false
}

var _ = token.NoPos