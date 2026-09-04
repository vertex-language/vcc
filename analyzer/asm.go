package analyzer

// gcc's inline assembly, as far as this layer can read it.
//
// The template is opaque here and stays that way: what a constraint letter
// means is the target's, and the reference `%0` resolves against an operand
// list this package does not renumber. What is checkable without a target is
// the C in it — the operand expressions are expressions, an output has to
// designate an object, a symbolic name has to be unique, and a label an
// `asm goto` branches to has to exist.

import (
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

func (c *checker) checkAsm(s *ast.AsmStmt) {
	seen := map[string]*ast.AsmOperand{}
	for _, o := range s.Outputs {
		c.checkAsmOperand(o, seen, true)
	}
	for _, o := range s.Inputs {
		c.checkAsmOperand(o, seen, false)
	}
	for _, l := range s.Labels {
		if l != nil {
			c.asmLabels = append(c.asmLabels, l)
		}
	}
}

func (c *checker) checkAsmOperand(o *ast.AsmOperand, seen map[string]*ast.AsmOperand, out bool) {
	if o == nil || o.X == nil {
		return
	}
	if o.Name != nil {
		n := c.name(o.Name)
		if _, dup := seen[n]; dup {
			c.report(o.Name, "duplicate asm operand name '"+n+"'")
		} else {
			seen[n] = o
		}
	}

	t := c.expr(o.X)
	if !out {
		return
	}

	// An output is written, so it has to be somewhere a value can be put.
	// The three ways it can fail are the three ways an assignment's left
	// operand can, which is what an output is.
	if !IsLvalue(o.X) {
		c.report(o.X, "an asm output must designate an object, and this expression does not")
		return
	}
	if t == nil {
		return
	}
	if types.QualsOf(t)&types.QConst != 0 {
		c.report(o.X, "cannot write to "+t.String()+" through an asm output; it is const-qualified")
	} else if types.IsArray(t) {
		c.report(o.X, "an array is not a modifiable lvalue, so it cannot be an asm output")
	}
}

// IsLvalue reports whether an expression designates an object, judged by its
// syntax alone.
//
// Syntax is enough for the question this answers — whether an asm output has
// somewhere to be written — and is all that is available in the two places
// that ask it, one of which runs before types are known. §6.3.2.1's lvalue is
// the same list: a name, a dereference, a subscript, a member of either, and
// a compound literal, through any number of parentheses.
func IsLvalue(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return IsLvalue(e.X)
	case *ast.Ident, *ast.IndexExpr, *ast.SelectorExpr, *ast.CompoundLit, *ast.StringLit:
		return true
	case *ast.UnaryExpr:
		return e.Op == token.MUL
	}
	return false
}
