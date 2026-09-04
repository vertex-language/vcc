package vcc

import (
	"fmt"

	"github.com/vertex-language/ir"
	"github.com/vertex-language/ir/verify"

	"github.com/vertex-language/vcc/lower"
	"github.com/vertex-language/vcc/token"
)

// IR carries one input through phase 7 and returns the module it lowered to —
// what `--emit vir` prints.
//
// The module is never nil. A tree the front end rejected lowers to an empty
// module rather than to nothing, because a partial module is what broken input
// should produce here; the diagnostics say whether it is a whole one.
func (c *Compiler) IR(in Input) (*ir.Module, []Diagnostic, error) {
	u, err := c.frontend(in)
	if err != nil {
		return nil, nil, err
	}
	defer u.release()

	// A tree the front end rejected is not lowered. Lowering one can only add
	// faults of its own — an expression that failed to parse leaves a poisoned
	// operand behind, and what the IR then reports is the poison rather than
	// the mistake — and those sort by position among the real diagnostics,
	// where they bury the line the caller has to fix.
	if u.failed() {
		return ir.NewModule(in.moduleName(), u.target.IR()), c.report(u.diagnostics()), nil
	}

	mod, ldiags := lower.Lower(u.file, u.tree, u.info, lower.Options{
		Name:         in.moduleName(),
		Target:       u.target.IR(),
		Model:        u.target.Model(),
		SymbolPrefix: u.target.SymbolPrefix(),
	})
	u.diags = append(u.diags, ldiags...)
	token.SortDiagnostics(u.diags)
	return mod, c.report(u.diagnostics()), nil
}

// Object compiles one input all the way to an object file's bytes.
//
// Bytes and diagnostics are the two answers, and an error is neither: a caller
// checks HasErrors to learn that the program was rejected, and gets an error
// only when vcc could not run — an unknown target, an unreadable file, a
// module that does not verify.
func (c *Compiler) Object(in Input) ([]byte, []Diagnostic, error) {
	t, err := c.target()
	if err != nil {
		return nil, nil, err
	}
	mod, diags, err := c.IR(in)
	if err != nil {
		return nil, nil, err
	}
	if HasErrors(diags) {
		return nil, diags, nil
	}

	// verify.Module runs between the two halves, always. It is the authority
	// on whether a module is sound, it is cheap next to instruction selection,
	// and what it catches is a bug in vcc — which is worth a sentence naming
	// the module rather than whatever the backend would have said three phases
	// later.
	if err := verify.Module(mod); err != nil {
		return nil, diags, fmt.Errorf("internal: the module lowered from %s does not verify: %w", in.name(), err)
	}

	data, err := emitObject(mod, t, c.producer(), c.March)
	if err != nil {
		return nil, diags, fmt.Errorf("%s: %w", in.name(), err)
	}
	return data, diags, nil
}
