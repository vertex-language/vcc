package preprocessor

// cond is one frame of the #if stack.
type cond struct {
	site      Site
	taken     bool   // some group in this chain has been taken
	active    bool   // this group is the one being processed
	sawElse   bool
	directive string // for the diagnostic on an unterminated chain
	guard     string // controlling macro of a candidate include guard
}

// live reports whether tokens are currently being kept: every enclosing
// conditional must be active.
func (r *reader) live() bool {
	for i := range r.conds {
		if !r.conds[i].active {
			return false
		}
	}
	return true
}

// skipping is live()'s inverse, named for readability at the call sites where
// the question is "do I need to evaluate this".
func (r *reader) skipping() bool { return !r.live() }

func (r *reader) pushCond(c cond) { r.conds = append(r.conds, c) }

func (r *reader) topCond() *cond {
	if len(r.conds) == 0 {
		return nil
	}
	return &r.conds[len(r.conds)-1]
}

// beginIf pushes a frame. Inside a skipped group the condition is not
// evaluated at all — §6.10.1p6 says a skipped group's directives are only
// checked for nesting, which is what makes `#ifdef HAVE_X` / `#include <x.h>`
// safe when the header does not exist.
func (r *reader) beginIf(p *Preprocessor, name string, at Site, eval func() bool) {
	if r.skipping() {
		r.pushCond(cond{site: at, directive: name})
		return
	}
	v := eval()
	r.pushCond(cond{site: at, directive: name, taken: v, active: v})
}

func (r *reader) doElif(p *Preprocessor, at Site, eval func() bool) {
	c := r.topCond()
	if c == nil {
		p.errorf(at, "#elif without #if")
		return
	}
	if c.sawElse {
		p.errorf(at, "#elif after #else")
		return
	}
	// Only evaluate when the chain is otherwise reachable and nothing has
	// been taken yet: a #elif in a dead branch must not report on its
	// expression. The *enclosing* frames are the question — this frame's
	// own active flag was legitimately false (its earlier arm was not
	// taken) and is about to be overwritten, exactly as in doElse.
	// Asking skipping() here would consult the flag just cleared and make
	// every #elif dead.
	c.active = false
	if c.taken || r.parentSkipping() {
		return
	}
	if eval() {
		c.taken, c.active = true, true
	}
}

func (r *reader) doElse(p *Preprocessor, at Site) {
	c := r.topCond()
	if c == nil {
		p.errorf(at, "#else without #if")
		return
	}
	if c.sawElse {
		p.errorf(at, "#else after #else")
		return
	}
	c.sawElse = true
	c.active = !c.taken && !r.parentSkipping()
	if c.active {
		c.taken = true
	}
}

// parentSkipping reports whether an *enclosing* conditional is inactive, which
// is the question #else and #elif must ask: this frame's own active flag is
// about to be overwritten.
func (r *reader) parentSkipping() bool {
	for i := 0; i < len(r.conds)-1; i++ {
		if !r.conds[i].active {
			return true
		}
	}
	return false
}

func (r *reader) doEndif(p *Preprocessor, at Site) {
	if len(r.conds) == 0 {
		p.errorf(at, "#endif without #if")
		return
	}
	c := r.conds[len(r.conds)-1]
	r.conds = r.conds[:len(r.conds)-1]

	// Closing the outermost frame: if it was a guard candidate, remember it.
	// Re-arm miValid — directive() cleared it before dispatching here — so
	// that only trailing whitespace keeps the guard valid; any further token
	// or directive will clear it again.
	if len(r.conds) == 0 {
		if c.guard != "" {
			r.pendingGuard = c.guard
			r.miValid = true
		} else {
			r.pendingGuard = ""
		}
	}
}

// finish reports any conditional left open at end of file, pointing at the
// directive that opened it rather than at the end of the file.
func (r *reader) finishConds(p *Preprocessor) {
	for i := range r.conds {
		p.errorf(r.conds[i].site, "unterminated %s", r.conds[i].directive)
	}
	r.conds = nil
}