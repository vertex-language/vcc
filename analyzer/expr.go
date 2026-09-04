package analyzer

import (
	"strconv"
	"strings"

	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// Expression typing, and the §6.5 constraints that need it.
//
// The rule this file works to is that a nil type means "not known", and
// nothing is reported about an operand whose type is not known. A checker that
// guesses produces a diagnostic about code that is correct, which is worse
// than saying nothing: the user cannot fix it, and learns to ignore the
// compiler. Every check below therefore begins by establishing that it knows
// both types.
//
// What is reported is the constraint violations that a program cannot mean:
// assigning a pointer to an integer, calling a function with the wrong number
// of arguments, adding two structs. What is not reported is anything that
// needs type compatibility to be more exact than types.Compatible is.

// expr types an expression and reports what it can about it.
//
// The type returned is the expression's own — an array is still an array, a
// function still a function. Contexts that perform §6.3.2.1's conversions call
// rvalue instead.
func (c *checker) expr(e ast.Expr) types.Type {
	if e == nil {
		return nil
	}
	switch e := e.(type) {
	case *ast.BadExpr:
		return nil

	case *ast.ParenExpr:
		return c.expr(e.X)

	case *ast.BasicLit:
		return c.literalType(e)

	case *ast.StringLit:
		return c.stringType(e)

	case *ast.Ident:
		return c.identType(e)

	case *ast.GenericExpr:
		return c.genericType(e)

	case *ast.IndexExpr:
		return c.indexType(e)

	case *ast.CallExpr:
		return c.callType(e)

	case *ast.SelectorExpr:
		return c.selectorType(e)

	case *ast.IncDecExpr:
		t := c.expr(e.X)
		c.requireScalar(e, t, e.Op.String())
		return t

	case *ast.CompoundLit:
		// §6.7.9p22 reaches here too: `(int[]){1,2,3}` is an int[3], and the
		// length is the one thing a compound literal's type name may leave
		// open. The completed type replaces what typeName recorded, which is
		// where lower reads it back.
		t := c.completeArray(c.typeName(e.Type), e.Init)
		c.info.Types[e.Type] = t
		c.expr(e.Init)
		return t

	case *ast.UnaryExpr:
		return c.unaryType(e)

	case *ast.SizeofExpr:
		if e.Type != nil {
			c.typeName(e.Type)
		} else {
			c.expr(e.X)
		}
		return c.sizeType()

	case *ast.AlignofExpr:
		if e.Type != nil {
			c.typeName(e.Type)
		}
		return c.sizeType()

	case *ast.CastExpr:
		t := c.typeName(e.Type)
		c.rvalue(e.X)
		return t

	case *ast.BinaryExpr:
		return c.binaryType(e)

	case *ast.CondExpr:
		return c.condType(e)

	case *ast.AssignExpr:
		return c.assignType(e)

	case *ast.StmtExpr:
		return c.stmtExprType(e)

	case *ast.LabelAddrExpr:
		// A label's address is a void *, and the label is in the label
		// namespace: goto's check covers whether it exists.
		if e.Label != nil {
			c.labelRefs = append(c.labelRefs, e.Label)
		}
		return &types.Pointer{Elem: types.Typ(types.Void)}

	case *ast.VaArgExpr:
		c.expr(e.Ap)
		if e.Type == nil {
			return nil
		}
		return c.typeName(e.Type)

	case *ast.TypesCompatibleExpr:
		// Both operands are types, and neither is evaluated. The value is
		// decided here — see evalInt — because a program is entitled to use
		// it in an integer constant expression, which is the only reason the
		// builtin is worth having.
		if e.A != nil {
			c.typeName(e.A)
		}
		if e.B != nil {
			c.typeName(e.B)
		}
		return types.Typ(types.Int)

	case *ast.OffsetofExpr:
		// The member designator names members of the type, not anything in
		// scope, so it is not walked as an expression. lower resolves it
		// against the record.
		if e.Type != nil {
			c.typeName(e.Type)
		}
		return c.sizeType()

	case *ast.InitList:
		for _, it := range e.Items {
			for _, d := range it.Designators {
				if ix, ok := d.(*ast.IndexDesignator); ok {
					c.expr(ix.Index)
				}
			}
			c.expr(it.Value)
		}
		return nil
	}
	return nil
}

// stmtExprType checks a statement expression and returns its type.
//
// gcc's rule: the value is the last statement's, and only an expression
// statement has one — anything else makes the whole thing void. The block is
// a block like any other, so it gets a scope, and a declaration inside it is
// scoped to it.
func (c *checker) stmtExprType(e *ast.StmtExpr) types.Type {
	if e.Body == nil {
		return types.Typ(types.Void)
	}
	c.push()
	defer c.pop()
	var last types.Type = types.Typ(types.Void)
	for i, item := range e.Body.Items {
		if es, ok := item.(*ast.ExprStmt); ok && i == len(e.Body.Items)-1 {
			last = types.Decay(c.expr(es.X))
			continue
		}
		c.checkStmt(item, true)
	}
	return last
}

// rvalue is expr followed by §6.3.2.1's conversions: an array becomes a
// pointer to its first element, a function a pointer to itself.
func (c *checker) rvalue(e ast.Expr) types.Type {
	t := c.expr(e)
	if t == nil {
		return nil
	}
	return types.Decay(t)
}

func (c *checker) sizeType() types.Type { return c.model.SizeType() }

// ---- leaves ----

func (c *checker) literalType(n *ast.BasicLit) types.Type {
	text := string(c.unit.Slice(n.Lo, n.Hi))
	rep := func(msg string) { c.report(n, msg) }
	switch n.Kind {
	case token.INT_LIT:
		return DecodeIntConst(text, c.model, rep).Type
	case token.FLOAT_LIT:
		_, t := DecodeFloatConst(text, rep)
		return t
	case token.CHAR_LIT:
		return DecodeCharConst(text, c.model, rep).Type
	}
	return nil
}

func (c *checker) stringType(n *ast.StringLit) types.Type {
	var sv StringValue
	c.reportOnce(n, func(rep func(string)) {
		sv = DecodeString(c.unit, n, c.model, rep)
	})
	if sv.Elem == nil {
		return nil
	}
	return &types.Array{Elem: sv.Elem, Form: types.FixedArray, Len: int64(len(sv.Data))}
}

// identType resolves an ordinary identifier and reports one that is not
// declared.
//
// C11 removed the implicit declaration (§6.5.1p2), so a name with nothing
// behind it is a constraint violation here rather than a link failure later.
// Each is reported once per translation unit: a misspelling inside a loop is
// one mistake however many times it is written.
func (c *checker) identType(id *ast.Ident) types.Type {
	name := c.name(id)
	if name == "" {
		return nil
	}
	if s := c.lookup(name); s != nil {
		// An enumeration constant has type int (§6.4.4.3p2), which is the
		// type enumType gave it — unless the list did not fit in one and
		// the enumeration widened, where it has the enumeration's type, as
		// it does under gcc and clang. Either way the symbol carries it.
		return s.typ
	}
	if isCompilerBuiltin(name) {
		// The compiler's own builtins, which a header calls and no
		// declaration introduces. lower knows the list; the analyzer only has
		// to not object, and cannot know their types.
		return nil
	}
	if c.quiet > 0 {
		// Not reported, and not remembered as reported either: the real use
		// of this name is still owed its one diagnostic.
		return nil
	}
	if c.undeclared == nil {
		c.undeclared = map[string]bool{}
	}
	if !c.undeclared[name] {
		c.undeclared[name] = true
		c.report(id, "'"+name+"' is undeclared; C11 has no implicit declaration (§6.5.1p2)")
	}
	return nil
}

// isCompilerBuiltin reports whether a name belongs to the compiler rather
// than to any declaration.
//
// The four prefixes are the ones in circulation: gcc's __builtin_ and its
// older __sync_ atomics, and clang's __c11_atomic_ and __atomic_, which its
// <stdatomic.h> is written in terms of. A program cannot declare one of
// these itself — every spelling is reserved — so treating an unknown one as
// declared costs nothing, and lower reports the ones it does not implement
// by name.
func isCompilerBuiltin(name string) bool { return IsCompilerBuiltin(name) }

// IsCompilerBuiltin reports whether a name is one of the reserved builtin
// spellings this package lets through as declared.
//
// It is exported because the two halves of the decision live in different
// packages: what is *declared* is decided here, and what is *implemented* is
// decided in lower, which needs the same test to tell an unimplemented
// builtin from a call to a name the program forgot to declare.
func IsCompilerBuiltin(name string) bool {
	return strings.HasPrefix(name, "__builtin_") ||
		strings.HasPrefix(name, "__sync_") ||
		strings.HasPrefix(name, "__c11_atomic_") ||
		strings.HasPrefix(name, "__atomic_") ||
		name == "__assume" ||
		name == "__noop"
}

// genericType is §6.5.1.1: the controlling expression is not evaluated, its
// type selects an association, and the result is that association's value.
func (c *checker) genericType(e *ast.GenericExpr) types.Type {
	ctrl := c.rvalue(e.Ctrl)
	var chosen, dflt types.Type
	found := false
	for _, a := range e.Assocs {
		if a.Type == nil {
			dflt = c.expr(a.Value)
			continue
		}
		at := c.typeName(a.Type)
		t := c.expr(a.Value)
		if !found && ctrl != nil && at != nil && types.Compatible(types.Unqualify(at), types.Unqualify(ctrl)) {
			chosen, found = t, true
		}
	}
	if found {
		return chosen
	}
	return dflt
}

// ---- postfix ----

func (c *checker) indexType(e *ast.IndexExpr) types.Type {
	x, i := c.rvalue(e.X), c.rvalue(e.Index)
	// §6.5.2.1: one operand is a pointer to a complete object type and the
	// other has integer type. Either order.
	switch {
	case x != nil && types.IsPointer(x):
		if i != nil && !types.IsInteger(i) {
			c.report(e, "array subscript is "+i.String()+", which is not an integer type")
		}
		return types.AsPointer(x).Elem
	case i != nil && types.IsPointer(i):
		return types.AsPointer(i).Elem
	case x != nil && i != nil:
		c.report(e, "cannot subscript a value of type "+x.String())
	}
	return nil
}

// callType checks §6.5.2.2: the callee is a function or a pointer to one, and
// where it has a prototype the arguments agree with it in number and type.
func (c *checker) callType(e *ast.CallExpr) types.Type {
	fnT := c.rvalue(e.Fun)
	args := make([]types.Type, len(e.Args))
	for i, a := range e.Args {
		args[i] = c.rvalue(a)
	}
	if fnT == nil {
		return nil
	}
	ft := types.AsFunc(fnT)
	if ft == nil {
		if p := types.AsPointer(fnT); p != nil {
			ft = types.AsFunc(p.Elem)
		}
	}
	if ft == nil {
		c.report(e.Fun, "called object is "+fnT.String()+", which is not a function or a pointer to one")
		return nil
	}
	if !ft.Proto {
		// `int f();` says nothing about the arguments, so there is nothing to
		// check them against.
		return ft.Ret
	}
	// A lone void parameter is the prototype for "no parameters".
	np := len(ft.Params)
	if np == 1 && types.IsVoid(ft.Params[0].Type) {
		np = 0
	}
	switch {
	case len(args) < np:
		c.report(e, "too few arguments: "+plural(np, "argument")+" expected, "+strconv.Itoa(len(args))+" given")
		return ft.Ret
	case len(args) > np && !ft.Variadic:
		c.report(e, "too many arguments: "+plural(np, "argument")+" expected, "+strconv.Itoa(len(args))+" given")
		return ft.Ret
	}
	for i := 0; i < np && i < len(args); i++ {
		c.checkAssign(e.Args[i], types.AdjustParam(ft.Params[i].Type), args[i],
			"passing argument "+strconv.Itoa(i+1))
	}
	return ft.Ret
}

// selectorType resolves x.m and p->m, and reports the three ways they go
// wrong: the wrong operator for the operand, a member that does not exist,
// and a member selection on something that is not a record at all.
func (c *checker) selectorType(e *ast.SelectorExpr) types.Type {
	x := c.expr(e.X)
	name := c.name(e.Sel)
	if x == nil {
		return nil
	}
	base := x
	if e.Op == token.ARROW {
		p := types.AsPointer(types.Decay(x))
		if p == nil {
			c.report(e, "'->' applied to "+x.String()+", which is not a pointer")
			return nil
		}
		base = p.Elem
	} else if types.IsPointer(types.Unqualify(x)) {
		c.report(e, "'.' applied to "+x.String()+"; use '->'")
		return nil
	}
	r := types.AsRecord(base)
	if r == nil {
		c.report(e, "request for member '"+name+"' in "+base.String()+
			", which is not a structure or union")
		return nil
	}
	if !r.Complete {
		// The definition may still be coming; saying nothing is better than
		// reporting a member of a type that is not yet described.
		return nil
	}
	ft, ok := findMember(r, name)
	if !ok {
		c.report(e.Sel, base.String()+" has no member named '"+name+"'")
		return nil
	}
	// A member of a qualified object is qualified: §6.5.2.3p4.
	return types.Qualify(ft, types.QualsOf(base))
}

// findMember searches a record, descending into anonymous members
// (§6.7.2.1p13).
func findMember(r *types.Record, name string) (types.Type, bool) {
	for _, f := range r.Fields {
		if f.Name == name {
			return f.Type, true
		}
	}
	for _, f := range r.Fields {
		if f.Name != "" {
			continue
		}
		if inner := types.AsRecord(f.Type); inner != nil {
			if t, ok := findMember(inner, name); ok {
				return t, true
			}
		}
	}
	return nil, false
}

// ---- unary ----

func (c *checker) unaryType(e *ast.UnaryExpr) types.Type {
	switch e.Op {
	case token.AND:
		// &x does not convert its operand: that is the whole point of it.
		t := c.expr(e.X)
		if t == nil {
			return nil
		}
		return &types.Pointer{Elem: t}

	case token.MUL:
		t := c.rvalue(e.X)
		if t == nil {
			return nil
		}
		p := types.AsPointer(t)
		if p == nil {
			c.report(e, "cannot dereference "+t.String())
			return nil
		}
		return p.Elem

	case token.NOT:
		t := c.rvalue(e.X)
		c.requireScalar(e, t, "'!'")
		return types.Typ(types.Int)

	case token.TILDE:
		t := c.rvalue(e.X)
		if t != nil && !types.IsInteger(t) {
			c.report(e, "'~' requires an integer operand, not "+t.String())
			return nil
		}
		if t == nil {
			return nil
		}
		return c.model.Promote(t)

	case token.ADD, token.SUB:
		t := c.rvalue(e.X)
		if t != nil && !types.IsArithmetic(t) {
			c.report(e, "unary '"+e.Op.String()+"' requires an arithmetic operand, not "+t.String())
			return nil
		}
		if t == nil {
			return nil
		}
		return c.model.Promote(t)

	case token.INC, token.DEC:
		t := c.expr(e.X)
		c.requireScalar(e, t, "'"+e.Op.String()+"'")
		return t
	}
	c.expr(e.X)
	return nil
}

// ---- binary ----

func (c *checker) binaryType(e *ast.BinaryExpr) types.Type {
	if e.Op == token.COMMA {
		c.rvalue(e.X)
		return c.rvalue(e.Y)
	}
	x, y := c.rvalue(e.X), c.rvalue(e.Y)

	switch e.Op {
	case token.LAND, token.LOR:
		c.requireScalar(e, x, "'"+e.Op.String()+"'")
		c.requireScalar(e, y, "'"+e.Op.String()+"'")
		return types.Typ(types.Int)

	case token.ADD:
		// §6.5.6: either both arithmetic, or one pointer and one integer.
		if x == nil || y == nil {
			return nil
		}
		switch {
		case types.IsArithmetic(x) && types.IsArithmetic(y):
			return c.model.Usual(x, y)
		case types.IsPointer(x) && types.IsInteger(y):
			return x
		case types.IsInteger(x) && types.IsPointer(y):
			return y
		}
		c.report(e, "cannot add "+x.String()+" and "+y.String())
		return nil

	case token.SUB:
		if x == nil || y == nil {
			return nil
		}
		switch {
		case types.IsArithmetic(x) && types.IsArithmetic(y):
			return c.model.Usual(x, y)
		case types.IsPointer(x) && types.IsInteger(y):
			return x
		case types.IsPointer(x) && types.IsPointer(y):
			if !types.CompatibleIgnoringQuals(types.AsPointer(x).Elem, types.AsPointer(y).Elem) {
				c.report(e, "cannot subtract "+y.String()+" from "+x.String()+
					"; the pointed-to types differ")
			}
			return c.model.PtrDiffType()
		}
		c.report(e, "cannot subtract "+y.String()+" from "+x.String())
		return nil

	case token.MUL, token.QUO:
		if x == nil || y == nil {
			return nil
		}
		if !types.IsArithmetic(x) || !types.IsArithmetic(y) {
			c.report(e, "'"+e.Op.String()+"' requires arithmetic operands, not "+
				x.String()+" and "+y.String())
			return nil
		}
		return c.model.Usual(x, y)

	case token.REM, token.AND, token.OR, token.XOR:
		if x == nil || y == nil {
			return nil
		}
		if !types.IsInteger(x) || !types.IsInteger(y) {
			c.report(e, "'"+e.Op.String()+"' requires integer operands, not "+
				x.String()+" and "+y.String())
			return nil
		}
		return c.model.Usual(x, y)

	case token.SHL, token.SHR:
		if x == nil || y == nil {
			return nil
		}
		if !types.IsInteger(x) || !types.IsInteger(y) {
			c.report(e, "'"+e.Op.String()+"' requires integer operands, not "+
				x.String()+" and "+y.String())
			return nil
		}
		// §6.5.7p3: each operand is promoted on its own; the result is the
		// left one's promoted type.
		return c.model.Promote(x)

	case token.LSS, token.GTR, token.LEQ, token.GEQ, token.EQL, token.NEQ:
		c.checkComparison(e, x, y)
		return types.Typ(types.Int)
	}
	return nil
}

// checkComparison is §6.5.8 and §6.5.9: two arithmetic operands, two
// compatible pointers, or a pointer against a null pointer constant.
func (c *checker) checkComparison(e *ast.BinaryExpr, x, y types.Type) {
	if x == nil || y == nil {
		return
	}
	switch {
	case types.IsArithmetic(x) && types.IsArithmetic(y):
		return
	case types.IsPointer(x) && types.IsPointer(y):
		xe, ye := types.AsPointer(x).Elem, types.AsPointer(y).Elem
		if types.IsVoid(xe) || types.IsVoid(ye) {
			return
		}
		if !types.CompatibleIgnoringQuals(xe, ye) {
			c.report(e, "comparison of distinct pointer types "+x.String()+" and "+y.String())
		}
	case types.IsPointer(x) && c.isNullConst(e.Y), types.IsPointer(y) && c.isNullConst(e.X):
		return
	default:
		c.report(e, "cannot compare "+x.String()+" and "+y.String())
	}
}

// ---- conditional and assignment ----

// condType is §6.5.15's result type.
func (c *checker) condType(e *ast.CondExpr) types.Type {
	cond := c.rvalue(e.Cond)
	c.requireScalar(e, cond, "the condition of '?:'")
	t1, t2 := c.rvalue(e.Then), c.rvalue(e.Else)
	if t1 == nil || t2 == nil {
		return nil
	}
	switch {
	case types.IsArithmetic(t1) && types.IsArithmetic(t2):
		return c.model.Usual(t1, t2)
	case types.IsVoid(t1) && types.IsVoid(t2):
		return types.Typ(types.Void)
	case types.IsRecord(t1) && types.CompatibleIgnoringQuals(t1, t2):
		return t1
	case types.IsPointer(t1) && c.isNullConst(e.Else):
		return t1
	case types.IsPointer(t2) && c.isNullConst(e.Then):
		return t2
	case types.IsPointer(t1) && types.IsPointer(t2):
		e1, e2 := types.AsPointer(t1).Elem, types.AsPointer(t2).Elem
		if types.IsVoid(e1) || types.IsVoid(e2) {
			return &types.Pointer{Elem: types.Typ(types.Void)}
		}
		if !types.CompatibleIgnoringQuals(e1, e2) {
			c.report(e, "the branches of '?:' are "+t1.String()+" and "+t2.String()+
				", which are not compatible")
		}
		return t1
	}
	c.report(e, "the branches of '?:' are "+t1.String()+" and "+t2.String()+
		", which have no common type")
	return nil
}

// assignType checks §6.5.16: the left operand is a modifiable lvalue, and for
// simple assignment the right operand is assignable to it.
func (c *checker) assignType(e *ast.AssignExpr) types.Type {
	lhs := c.expr(e.Lhs)
	rhs := c.rvalue(e.Rhs)
	if lhs != nil {
		if types.QualsOf(lhs)&types.QConst != 0 {
			c.report(e.Lhs, "cannot assign to "+lhs.String()+", which is const-qualified")
		} else if types.IsArray(lhs) {
			c.report(e.Lhs, "cannot assign to "+lhs.String()+"; an array is not a modifiable lvalue")
		}
	}
	if e.Op == token.ASSIGN {
		c.checkAssign(e.Rhs, lhs, rhs, "assigning")
		return lhs
	}
	// A compound assignment is the operation followed by a conversion back to
	// the left operand's type, and the operation's own constraints are what
	// bind. Only the ones a wrong type makes meaningless are reported.
	if lhs == nil || rhs == nil {
		return lhs
	}
	switch e.Op {
	case token.ADD_ASSIGN, token.SUB_ASSIGN:
		if types.IsPointer(lhs) {
			if !types.IsInteger(rhs) {
				c.report(e, "cannot add "+rhs.String()+" to "+lhs.String())
			}
			return lhs
		}
		if !types.IsArithmetic(lhs) || !types.IsArithmetic(rhs) {
			c.report(e, "'"+e.Op.String()+"' requires arithmetic operands, not "+
				lhs.String()+" and "+rhs.String())
		}
	case token.MUL_ASSIGN, token.QUO_ASSIGN:
		if !types.IsArithmetic(lhs) || !types.IsArithmetic(rhs) {
			c.report(e, "'"+e.Op.String()+"' requires arithmetic operands, not "+
				lhs.String()+" and "+rhs.String())
		}
	default:
		if !types.IsInteger(lhs) || !types.IsInteger(rhs) {
			c.report(e, "'"+e.Op.String()+"' requires integer operands, not "+
				lhs.String()+" and "+rhs.String())
		}
	}
	return lhs
}

// checkAssign reports §6.5.16.1's constraint violations for one assignment,
// which is also what §6.5.2.2 requires of an argument and §6.8.6.4 of a
// return value.
//
// what names the context, so one function serves all three: "assigning",
// "passing argument 2", "returning".
func (c *checker) checkAssign(at ast.Node, dst, src types.Type, what string) {
	if dst == nil || src == nil || types.IsVoid(dst) {
		return
	}
	switch types.Assignable(dst, src, c.isNullConst(exprOf(at))) {
	case types.AssignOK:
		return
	case types.AssignIntPointer:
		c.report(at, what+" converts between a pointer and an integer without a cast: "+
			src.String()+" to "+dst.String())
	case types.AssignPointerMismatch:
		c.report(at, what+" from incompatible pointer type: "+
			src.String()+" to "+dst.String())
	case types.AssignDiscardsQuals:
		c.report(at, what+" discards a qualifier: "+src.String()+" to "+dst.String())
	default:
		c.report(at, what+" from an incompatible type: "+src.String()+" to "+dst.String())
	}
}

func exprOf(n ast.Node) ast.Expr {
	e, _ := n.(ast.Expr)
	return e
}

// isNullConst reports whether e is §6.3.2.3p3's null pointer constant: an
// integer constant expression with the value 0, or such an expression cast to
// void *.
func (c *checker) isNullConst(e ast.Expr) bool {
	if e == nil {
		return false
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return c.isNullConst(x.X)
	case *ast.CastExpr:
		t := c.info.Types[x.Type]
		if p := types.AsPointer(t); p != nil && types.IsVoid(p.Elem) {
			return c.isNullConst(x.X)
		}
		return false
	}
	v, ok := c.evalInt(e)
	return ok && v == 0
}

// ---- shared reporting ----

func (c *checker) requireScalar(at ast.Node, t types.Type, what string) {
	if t == nil {
		return
	}
	if !types.IsScalar(types.Decay(t)) {
		c.report(at, what+" requires a scalar operand, not "+t.String())
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// declareBuiltinTypes puts the compiler's own type names in file scope.
//
// __builtin_va_list is the type a platform header reaches for when it needs
// the compiler's va_list without including <stdarg.h> — Darwin's
// <sys/_types/_va_list.h> is `typedef __darwin_va_list va_list;` and
// __darwin_va_list is this. It is a real typedef rather than a name the
// parser tolerates and the analyzer guesses at, because the type has to be
// the one vcc's own <stdarg.h> declares or the two spellings of va_list would
// not be the same type.
func (c *checker) declareBuiltinTypes() {
	def := func(name string, t types.Type) {
		c.scopes[0].ordinary[name] = &symbol{kind: symTypedef, typ: t}
	}
	def("__builtin_va_list", &types.Pointer{Elem: types.Typ(types.Void)})

	// gcc's 128-bit integers in their typedef spellings. Nothing declares
	// them, and the parser's tolerated set only gets them as far as a name —
	// which then resolved to int, so a struct with a __uint128_t member came
	// out four bytes short at that member and wrong at every member after it.
	// Darwin's arm/_mcontext.h has one, reached from <stdio.h>.
	//
	// The bare __int128 spelling is not here: it is a type specifier, so it
	// is a keyword the type builder combines with signed and unsigned.
	//
	// The width is what this fixes. Arithmetic on them is still refused,
	// because the IR has no 128-bit register and the software sequences are
	// not written; that is a diagnostic where it was a wrong answer.
	def("__int128_t", types.Typ(types.Int128))
	def("__uint128_t", types.Typ(types.UInt128))

}
