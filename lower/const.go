package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/analyzer"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// Constant expressions, for the one place C requires them and VIR cannot
// compute them: the initializer of an object with static storage duration.
//
// §L declines a general constant-expression grammar in initializers — reloc
// admits what a relocation record admits and no more. So this folds in Go and
// hands VIR a literal or a symbol-plus-displacement, and reports anything it
// cannot reduce to one rather than emitting an approximation.

// constExpr folds e as an initializer for an object of type t.
func (u *unit) constExpr(e ast.Expr, t types.Type) (ir.Init, bool) {
	t = types.Unqualify(t)

	if isPtrType(t) {
		if sym, off, ok := u.constAddr(e); ok {
			if sym == nil {
				return ir.Lit(ir.Int(off)), true
			}
			init := ir.RelocInit(sym)
			if off != 0 {
				init = init.Plus(ir.Int(off))
			}
			return init, true
		}
	}
	if isFloatType(t) {
		if f, ok := u.constFloat(e); ok {
			return ir.Lit(ir.Float(f)), true
		}
		return ir.ZeroInit, false
	}
	if v, ok := u.constInt(e); ok {
		bits, signed := u.model.IntBits(t)
		if !signed && bits < 64 {
			return ir.Lit(ir.Uint(uint64(v) & (1<<uint(bits) - 1))), true
		}
		return ir.Lit(ir.Int(v)), true
	}
	// An integer-typed initializer naming an address: `long x = (long)&y;`
	if sym, off, ok := u.constAddr(e); ok && sym != nil {
		init := ir.RelocInit(sym)
		if off != 0 {
			init = init.Plus(ir.Int(off))
		}
		return init, true
	}
	return ir.ZeroInit, false
}

// constInt folds an integer constant expression.
//
// Info.Consts answers wherever the analyzer already required constness — an
// array length, a case label, an enumerator, a bit-field width. Everywhere
// else this folds, because an initializer's constness is a §6.6 requirement
// the analyzer documented itself as deferring.
func (u *unit) constInt(e ast.Expr) (int64, bool) {
	if v, ok := u.constOf(e); ok {
		return v, true
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return u.constInt(e.X)

	case *ast.BasicLit:
		switch e.Kind {
		case token.INT_LIT:
			return int64(u.decodeInt(e).Value), true
		case token.CHAR_LIT:
			iv := analyzer.DecodeCharConst(string(u.src.Slice(e.Pos(), e.End())), u.model, func(string) {})
			return int64(iv.Value), true
		}

	case *ast.Ident:
		if o := u.lookup(u.name(e)); o != nil && o.isEnum {
			return o.val, true
		}

	case *ast.SizeofExpr:
		var t types.Type
		if e.Type != nil {
			t = u.typeOf(e.Type)
		} else {
			t = u.staticType(e.X)
		}
		if a, ok := asArray(t); ok && a.Form != types.FixedArray {
			return 0, false
		}
		return u.sizeof(t, e), true

	case *ast.AlignofExpr:
		return u.alignof(u.typeOf(e.Type), e), true

	case *ast.CastExpr:
		v, ok := u.constInt(e.X)
		if !ok {
			return 0, false
		}
		t := types.Unqualify(u.typeOf(e.Type))
		if !types.IsInteger(t) && !isPtrType(t) {
			return 0, false
		}
		bits, signed := u.model.IntBits(t)
		if bits >= 64 || bits == 0 {
			return v, true
		}
		m := int64(1)<<uint(bits) - 1
		v &= m
		if signed && v&(1<<uint(bits-1)) != 0 {
			v |= ^m
		}
		return v, true

	case *ast.UnaryExpr:
		// Explicit offsetof handling: (size_t)&((T *)0)->m
		if e.Op == token.AND {
			if sym, off, ok := u.constDesignator(e.X); ok && sym == nil {
				return off, true
			}
			return 0, false
		}
		v, ok := u.constInt(e.X)
		if !ok {
			break
		}
		switch e.Op {
		case token.ADD:
			return v, true
		case token.SUB:
			return -v, true
		case token.TILDE:
			return ^v, true
		case token.NOT:
			if v == 0 {
				return 1, true
			}
			return 0, true
		}

	case *ast.CondExpr:
		c, ok := u.constInt(e.Cond)
		if !ok {
			break
		}
		if c != 0 {
			return u.constInt(e.Then)
		}
		return u.constInt(e.Else)

	case *ast.BinaryExpr:
		x, ok1 := u.constInt(e.X)
		y, ok2 := u.constInt(e.Y)
		if !ok1 || !ok2 {
			break
		}
		switch e.Op {
		case token.ADD:
			return x + y, true
		case token.SUB:
			return x - y, true
		case token.MUL:
			return x * y, true
		case token.QUO:
			if y == 0 {
				u.errorf(e, "division by zero in a constant expression")
				return 0, false
			}
			return x / y, true
		case token.REM:
			if y == 0 {
				u.errorf(e, "division by zero in a constant expression")
				return 0, false
			}
			return x % y, true
		case token.AND:
			return x & y, true
		case token.OR:
			return x | y, true
		case token.XOR:
			return x ^ y, true
		case token.SHL:
			return x << uint(y&63), true
		case token.SHR:
			return x >> uint(y&63), true
		case token.LAND:
			return b2i(x != 0 && y != 0), true
		case token.LOR:
			return b2i(x != 0 || y != 0), true
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
	}

	return 0, false
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (u *unit) constFloat(e ast.Expr) (float64, bool) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return u.constFloat(e.X)
	case *ast.BasicLit:
		if e.Kind == token.FLOAT_LIT {
			f, _ := analyzer.DecodeFloatConst(string(u.src.Slice(e.Pos(), e.End())),
				func(msg string) { u.errorf(e, "%s", msg) })
			return f, true
		}
	case *ast.CastExpr:
		return u.constFloat(e.X)
	case *ast.UnaryExpr:
		f, ok := u.constFloat(e.X)
		if !ok {
			break
		}
		switch e.Op {
		case token.ADD:
			return f, true
		case token.SUB:
			return -f, true
		}
	case *ast.BinaryExpr:
		x, ok1 := u.constFloat(e.X)
		y, ok2 := u.constFloat(e.Y)
		if ok1 && ok2 {
			switch e.Op {
			case token.ADD:
				return x + y, true
			case token.SUB:
				return x - y, true
			case token.MUL:
				return x * y, true
			case token.QUO:
				if y != 0 {
					return x / y, true
				}
			}
		}
	}
	if v, ok := u.constInt(e); ok {
		return float64(v), true
	}
	return 0, false
}

// constAddr folds an address constant to a symbol and a byte displacement.
//
// A nil symbol with a displacement is the null-based form the offsetof macro
// in <stddef.h> relies on. Everything reachable is here: the address of a
// static object, of a function, of a string literal, plus member selection
// and constant subscripting on top of one.
func (u *unit) constAddr(e ast.Expr) (ir.Symbol, int64, bool) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return u.constAddr(e.X)

	case *ast.CastExpr:
		return u.constAddr(e.X)

	case *ast.StringLit:
		return u.stringSymbol(u.decodeString(e)), 0, true

	case *ast.CompoundLit:
		// An array literal is its own address, the same as an array name.
		// The struct case arrives through & and constDesignator below.
		if _, isArr := asArray(u.compoundLitType(e)); !isArr {
			return nil, 0, false
		}
		return u.constCompoundLit(e)

	case *ast.Ident:
		o := u.lookup(u.name(e))
		if o == nil || !o.isStatic() {
			return nil, 0, false
		}
		// An array or function name is its own address.
		if _, isArr := asArray(o.typ); isArr {
			return u.imported(o), 0, true
		}
		if _, isFn := asFunc(o.typ); isFn {
			return u.imported(o), 0, true
		}
		return nil, 0, false

	case *ast.UnaryExpr:
		switch e.Op {
		case token.AND:
			return u.constDesignator(e.X)
		case token.MUL:
			return u.constAddr(e.X)
		}

	case *ast.BinaryExpr:
		if e.Op != token.ADD && e.Op != token.SUB {
			break
		}
		sym, off, ok := u.constAddr(e.X)
		if !ok {
			if e.Op != token.ADD {
				break
			}
			sym, off, ok = u.constAddr(e.Y)
			if !ok {
				break
			}
			n, ok2 := u.constInt(e.X)
			if !ok2 {
				break
			}
			return sym, off + n*u.pointeeSize(e.Y), true
		}
		n, ok2 := u.constInt(e.Y)
		if !ok2 {
			break
		}
		if e.Op == token.SUB {
			n = -n
		}
		return sym, off + n*u.pointeeSize(e.X), true

	case *ast.IndexExpr, *ast.SelectorExpr:
		// Explicit handling for decay: if it decays to an array or func ptr, it's an address
		t := u.staticType(e)
		if _, isArr := asArray(t); isArr {
			return u.constDesignator(e)
		}
		if _, isFn := asFunc(t); isFn {
			return u.constDesignator(e)
		}
	}

	// Safe fallback to int because constInt will not call constAddr unconditionally
	if v, ok := u.constInt(e); ok {
		return nil, v, true // an integer used as an address: null, or absolute
	}
	return nil, 0, false
}

// constCompoundLit folds a compound literal to the object it names.
//
// Only at file scope: §6.5.2.5p5 gives a literal inside a function automatic
// storage duration, and the address of an automatic object is not a constant
// however it is spelled.
func (u *unit) constCompoundLit(e *ast.CompoundLit) (ir.Symbol, int64, bool) {
	if u.fn != nil {
		return nil, 0, false
	}
	return u.staticCompoundLit(e, u.compoundLitType(e)), 0, true
}

// constDesignator folds the operand of & into a symbol and displacement.
func (u *unit) constDesignator(e ast.Expr) (ir.Symbol, int64, bool) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return u.constDesignator(e.X)

	case *ast.CompoundLit:
		return u.constCompoundLit(e)

	case *ast.Ident:
		o := u.lookup(u.name(e))
		if o == nil || !o.isStatic() {
			return nil, 0, false
		}
		return u.imported(o), 0, true

	case *ast.IndexExpr:
		sym, off, ok := u.constAddr(e.X)
		if !ok {
			if sym, off, ok = u.constDesignator(e.X); !ok {
				return nil, 0, false
			}
		}
		n, ok2 := u.constInt(e.Index)
		if !ok2 {
			return nil, 0, false
		}
		return sym, off + n*u.pointeeSize(e.X), true

	case *ast.SelectorExpr:
		var (
			sym ir.Symbol
			off int64
			ok  bool
			rt  types.Type
		)
		if e.Op == token.ARROW {
			sym, off, ok = u.constAddr(e.X)
			rt = u.staticType(e.X)
			if p, isP := asPointer(u.decayType(rt)); isP {
				rt = p.Elem
			}
		} else {
			sym, off, ok = u.constDesignator(e.X)
			rt = u.staticType(e.X)
		}
		if !ok {
			return nil, 0, false
		}
		r, isRec := asRecord(rt)
		if !isRec {
			return nil, 0, false
		}
		path := u.types.member(r, u.name(e.Sel))
		if path == nil {
			return nil, 0, false
		}
		moff, last := u.types.offsetOf(path)
		if last.Bit {
			return nil, 0, false
		}
		return sym, off + moff, true
	}
	return nil, 0, false
}

// pointeeSize is the scale for pointer arithmetic on an expression.
func (u *unit) pointeeSize(e ast.Expr) int64 {
	t := u.decayType(u.staticType(e))
	if p, ok := asPointer(t); ok {
		if n, ok := u.model.Sizeof(types.Unqualify(p.Elem)); ok {
			return n
		}
	}
	return 1
}
