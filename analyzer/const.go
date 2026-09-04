package analyzer

import (
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// Integer constant expressions (§6.6). evalInt returns (value, ok):
// ok is false when the expression isn't one — silently, because a
// non-constant array length is a VLA, not a mistake. Callers that
// required a constant report it themselves via requireConst.
//
// sizeof folds in both its forms. The expression form needs the operand's
// type, which c.expr computes — quietly, so that the operand is not reported
// twice: it is walked again where the sizeof itself is walked, and inside a
// _Static_assert condition it is not walked anywhere else at all.

// quietType returns an expression's type with nothing reported about it.
// The operand of a sizeof is wanted for its type alone, and whatever is
// wrong with it belongs to the walk that evaluates it, not to this one.
func (c *checker) quietType(e ast.Expr) types.Type {
	c.quiet++
	t := c.expr(e)
	c.quiet--
	return t
}

func (c *checker) requireConst(e ast.Expr, what string) (int64, bool) {
	v, ok := c.evalInt(e)
	if !ok {
		c.report(e, what+" must be an integer constant expression")
		return 0, false
	}
	c.info.Consts[e] = v
	return v, true
}

func (c *checker) evalInt(e ast.Expr) (int64, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		switch e.Kind {
		case token.INT_LIT:
			v := DecodeIntConst(string(c.unit.Slice(e.Lo, e.Hi)), c.model, func(string) {})
			return int64(v.Value), true
		case token.CHAR_LIT:
			v := DecodeCharConst(string(c.unit.Slice(e.Lo, e.Hi)), c.model, func(string) {})
			return int64(v.Value), true
		}
		return 0, false

	case *ast.Ident:
		if s := c.lookup(c.name(e)); s != nil && s.kind == symEnumConst {
			return s.value, true
		}
		return 0, false

	case *ast.ParenExpr:
		return c.evalInt(e.X)

	case *ast.UnaryExpr:
		if e.Op == token.AND {
			// The address of an object over a constant base — the
			// offsetof idiom. See evalAddr.
			return c.evalAddr(e.X)
		}
		v, ok := c.evalInt(e.X)
		if !ok {
			return 0, false
		}
		switch e.Op {
		case token.ADD:
			return v, true
		case token.SUB:
			return -v, true
		case token.TILDE:
			return ^v, true
		case token.NOT:
			return b2i(v == 0), true
		}
		return 0, false

	case *ast.BinaryExpr:
		x, ok := c.evalInt(e.X)
		if !ok {
			return 0, false
		}
		// && and || short-circuit even in constant expressions:
		// 0 && (1/0) is constant.
		switch e.Op {
		case token.LAND:
			if x == 0 {
				return 0, true
			}
			y, ok := c.evalInt(e.Y)
			return b2i(y != 0), ok
		case token.LOR:
			if x != 0 {
				return 1, true
			}
			y, ok := c.evalInt(e.Y)
			return b2i(y != 0), ok
		}
		y, ok := c.evalInt(e.Y)
		if !ok {
			return 0, false
		}
		switch e.Op {
		case token.ADD:
			return x + y, true
		case token.SUB:
			return x - y, true
		case token.MUL:
			return x * y, true
		case token.QUO, token.REM:
			if y == 0 {
				c.report(e, "division by zero in constant expression")
				return 0, false
			}
			if e.Op == token.QUO {
				return x / y, true
			}
			return x % y, true
		case token.SHL:
			return x << (uint64(y) & 63), true
		case token.SHR:
			return x >> (uint64(y) & 63), true
		case token.AND:
			return x & y, true
		case token.OR:
			return x | y, true
		case token.XOR:
			return x ^ y, true
		case token.EQL:
			return b2i(x == y), true
		case token.NEQ:
			return b2i(x != y), true
		case token.LSS:
			return b2i(x < y), true
		case token.GTR:
			return b2i(x > y), true
		case token.LEQ:
			return b2i(x <= y), true
		case token.GEQ:
			return b2i(x >= y), true
		}
		return 0, false // COMMA: not permitted in constant expressions

	case *ast.CondExpr:
		v, ok := c.evalInt(e.Cond)
		if !ok {
			return 0, false
		}
		if v != 0 {
			return c.evalInt(e.Then)
		}
		return c.evalInt(e.Else)

	case *ast.CastExpr:
		v, ok := c.evalInt(e.X)
		if !ok {
			return 0, false
		}
		t := c.typeName(e.Type)
		if !types.IsInteger(t) {
			return 0, false
		}
		bits, signed := c.model.IntBits(t)
		if bits >= 64 {
			return v, true
		}
		u := uint64(v) & (1<<uint(bits) - 1)
		if signed && u&(1<<uint(bits-1)) != 0 {
			return int64(u | ^uint64(1<<uint(bits)-1)), true
		}
		return int64(u), true

	case *ast.SizeofExpr:
		var t types.Type
		if e.Type != nil {
			t = c.typeName(e.Type)
		} else if e.X != nil {
			// §6.5.3.4p2 leaves the operand unevaluated unless its type is
			// variably modified, so what is wanted is the operand's type and
			// never its value. quietType is how that type is had without
			// reporting the operand a second time.
			t = c.quietType(e.X)
		}
		if t == nil {
			return 0, false
		}
		if c.hasVLA(t) {
			// A variable-length array's size is computed where the sizeof
			// is, so it is not a constant expression and the operand really
			// is evaluated (§6.5.3.4p2).
			return 0, false
		}
		if sz, ok := c.model.Sizeof(t); ok {
			return sz, true
		}
		return 0, false

	case *ast.TypesCompatibleExpr:
		// §6.6 admits only the operators it lists, but this is gcc's
		// extension and gcc documents the result as usable in an integer
		// constant expression — which is the whole point, since it exists to
		// let a macro branch on a type at compile time.
		//
		// Compatibility is vcc's own (§6.2.7p1, top-level qualifiers
		// dropped, per the builtin's definition). One implementation-defined
		// answer differs from clang's: vcc fixes an enum's compatible
		// integer type as int (§6.7.2.2p4), so `(enum E, int)` is 1 here and
		// 0 where the enum's type was chosen as unsigned int. Answering with
		// vcc's own type system is what keeps this builtin, _Generic, and
		// assignment saying the same thing about the same two types.
		if e.A == nil || e.B == nil {
			return 0, false
		}
		a, b := c.typeName(e.A), c.typeName(e.B)
		if a == nil || b == nil {
			return 0, false
		}
		return b2i(types.CompatibleIgnoringQuals(a, b)), true

	case *ast.AlignofExpr:
		if a, ok := c.model.Alignof(c.typeName(e.Type)); ok {
			return a, true
		}
		return 0, false

	case *ast.OffsetofExpr:
		// §6.6 admits only the operators it lists, but offsetof's whole
		// purpose is to be usable where a constant is required, and gcc,
		// clang and MSVC all fold this. lower computes the same number the
		// same way for the case where no constant was demanded.
		if e.Type == nil || e.Member == nil {
			return 0, false
		}
		_, off, ok := c.designate(c.typeName(e.Type), e.Member)
		return off, ok
	}
	return 0, false
}

// evalAddr folds an address constant whose base is an integer: the byte
// offset of the object &e names, from a pointer that was written as a
// number. It reports false for every other lvalue, since the address of a
// real object is not an integer constant expression.
//
// It exists for one idiom, which C has had no other way to write since
// before it had offsetof:
//
//	#define offsetof(s, m) ((size_t)&(((s *)0)->m))
//
// It is what MSVC's own <stddef.h> expands offsetof to whenever _MSC_VER
// is defined, what the Windows SDK's FIELD_OFFSET falls back to, and what
// a great deal of C written before __builtin_offsetof existed still says.
// gcc and clang fold it and document that they do.
func (c *checker) evalAddr(e ast.Expr) (int64, bool) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return c.evalAddr(e.X)

	case *ast.UnaryExpr:
		// *p: the object at a pointer, whose address is the pointer.
		if e.Op == token.MUL {
			return c.evalPtr(e.X)
		}
		return 0, false

	case *ast.SelectorExpr:
		if e.Sel == nil {
			return 0, false
		}
		var base int64
		var rec types.Type
		if e.Op == token.ARROW {
			// p->m: the pointer is the base, and it has to be a constant.
			b, ok := c.evalPtr(e.X)
			if !ok {
				return 0, false
			}
			p, ok := types.Unqualify(types.Decay(c.quietType(e.X))).(*types.Pointer)
			if !ok {
				return 0, false
			}
			base, rec = b, p.Elem
		} else {
			b, ok := c.evalAddr(e.X)
			if !ok {
				return 0, false
			}
			base, rec = b, c.quietType(e.X)
		}
		n, ok := c.model.Offsetof(rec, c.name(e.Sel))
		return base + n, ok

	case *ast.IndexExpr:
		// a[i], over either a constant pointer or an object this function
		// already has an offset for.
		base, ok := c.evalAddr(e.X)
		if !ok {
			if base, ok = c.evalPtr(e.X); !ok {
				return 0, false
			}
		}
		i, ok := c.evalInt(e.Index)
		if !ok {
			return 0, false
		}
		t := types.Decay(c.quietType(e.X))
		p, ok := types.Unqualify(t).(*types.Pointer)
		if !ok {
			return 0, false
		}
		sz, ok := c.model.Sizeof(p.Elem)
		if !ok {
			return 0, false
		}
		return base + i*sz, true
	}
	return 0, false
}

// evalPtr folds an expression of pointer type to the address it names, as a
// number, for evalAddr to add a member offset to.
//
// Only a pointer written as a number folds: a cast of an integer constant,
// which is what `(struct S *)0` is, or an address evalAddr itself already
// has a number for. The address of a real object is not one of those, and
// falls out as not constant — which is the answer, since &x + 1 is an
// address constant and not an integer constant expression.
//
// evalInt refuses a cast to a pointer, and is right to: `(int *)0` has
// pointer type, and folding it there would make it an integer constant
// expression everywhere. This is the one context that wants its number.
func (c *checker) evalPtr(e ast.Expr) (int64, bool) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return c.evalPtr(e.X)

	case *ast.CastExpr:
		if _, ok := types.Unqualify(c.typeName(e.Type)).(*types.Pointer); !ok {
			return 0, false
		}
		return c.evalPtr(e.X)

	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return c.evalAddr(e.X)
		}
	}
	// A null pointer constant is an integer constant expression, and so is
	// anything else written as one here.
	return c.evalInt(e)
}

// designate walks __builtin_offsetof's member designator — a chain of
// selections and subscripts against the type, naming nothing in scope — and
// returns the type each step lands on together with its byte offset from
// the base of the record.
func (c *checker) designate(base types.Type, e ast.Expr) (types.Type, int64, bool) {
	switch e := e.(type) {
	case *ast.Ident:
		off, ok := c.model.Offsetof(base, c.name(e))
		if !ok {
			return nil, 0, false
		}
		t, ok := c.memberType(base, c.name(e))
		return t, off, ok

	case *ast.SelectorExpr:
		inner, off, ok := c.designate(base, e.X)
		if !ok || e.Sel == nil {
			return nil, 0, false
		}
		n, ok := c.model.Offsetof(inner, c.name(e.Sel))
		if !ok {
			return nil, 0, false
		}
		t, ok := c.memberType(inner, c.name(e.Sel))
		return t, off + n, ok

	case *ast.IndexExpr:
		at, off, ok := c.designate(base, e.X)
		if !ok {
			return nil, 0, false
		}
		arr, ok := types.Unqualify(at).(*types.Array)
		if !ok {
			return nil, 0, false
		}
		i, ok := c.evalInt(e.Index)
		if !ok {
			return nil, 0, false
		}
		sz, ok := c.model.Sizeof(arr.Elem)
		if !ok {
			return nil, 0, false
		}
		return arr.Elem, off + i*sz, true
	}
	return nil, 0, false
}

// memberType is the type of a record's member, found the way Offsetof finds
// its offset: named members first, then through the anonymous ones.
func (c *checker) memberType(t types.Type, name string) (types.Type, bool) {
	r, ok := types.Unqualify(t).(*types.Record)
	if !ok {
		return nil, false
	}
	for _, f := range r.Fields {
		if f.Name == name {
			return f.Type, true
		}
	}
	for _, f := range r.Fields {
		if f.Name != "" {
			continue
		}
		if _, ok := types.Unqualify(f.Type).(*types.Record); !ok {
			continue
		}
		if ft, ok := c.memberType(f.Type, name); ok {
			return ft, true
		}
	}
	return nil, false
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}