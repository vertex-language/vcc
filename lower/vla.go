package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/types"
)

// Variable-length arrays.
//
// A VLA is the only C object whose storage really is scoped: ptr.alloc is
// entry-block-only and gives every other automatic object the function's
// frame, but a VLA's size is not known there, so it uses ptr.alloca and its
// storage is released by restoring the stack pointer when the block ends.
// The size expression is evaluated exactly once, at the declaration
// (§6.7.6.2p5), and kept — sizeof reads it back rather than re-evaluating.

// stackMark records the stack token a block must restore on exit, or a zero
// token where the block allocated nothing.
type stackMark struct {
	tok   ir.Ptr
	valid bool
}

// saveStack takes a stack token if this block might need one.
//
// The token is taken lazily: a block with no VLA in it emits no stacksave and
// no stackrestore, which is every block in almost every program.
func (u *unit) saveStack() stackMark { return stackMark{} }

// restoreStack releases whatever the block allocated.
func (u *unit) restoreStack(m stackMark) {
	if !m.valid || u.fn == nil || !u.fn.live {
		return
	}
	u.blk().Ptr.StackRestore(m.tok)
}

// recordVLAExprs links a VLA type to its dimension expression.
// C parses declarators inside-out, meaning the outermost array in the AST
// corresponds to the outermost array layer in the C type.
func (u *unit) recordVLAExprs(t types.Type, at ast.Node) {
	var decl ast.Declarator
	switch node := at.(type) {
	case *ast.InitDeclarator:
		decl = node.Decl
	case *ast.ParamDecl:
		decl = node.Decl
	case *ast.TypeName:
		decl = node.Decl
	default:
		return
	}

	// 1. Walk the AST declarator and extract array size expressions.
	var exprs []ast.Expr
	var walkDecl func(d ast.Declarator)
	walkDecl = func(d ast.Declarator) {
		if d == nil {
			return
		}
		switch x := d.(type) {
		case *ast.ParenDeclarator:
			walkDecl(x.Inner)
		case *ast.PtrDeclarator:
			walkDecl(x.Inner)
		case *ast.FuncDeclarator:
			walkDecl(x.Inner)
		case *ast.ArrayDeclarator:
			// Process Inner first to match the C type's outside-in order.
			walkDecl(x.Inner)

			// Extract the length expression dynamically.
			var expr ast.Expr
			ast.Inspect(x, func(n ast.Node) bool {
				if n == x {
					return true
				}
				if _, isDecl := n.(ast.Declarator); isDecl {
					return false
				} // Do not descend into Inner
				if e, isExpr := n.(ast.Expr); isExpr && expr == nil {
					expr = e
				}
				return true
			})
			if expr != nil {
				exprs = append(exprs, expr)
			}
		}
	}
	walkDecl(decl)

	// 2. Walk the resolved type and map the collected expressions to the VLA dimensions.
	// A function parameter's outermost array decays to a pointer, so the AST might have
	// more arrays than the type. We offset exprIdx to skip the decayed expressions.
	numTypeArrays := 0
	var countArrays func(ty types.Type)
	countArrays = func(ty types.Type) {
		ty = types.Unqualify(ty)
		if a, ok := ty.(*types.Array); ok {
			numTypeArrays++
			countArrays(a.Elem)
		} else if p, ok := ty.(*types.Pointer); ok {
			countArrays(p.Elem)
		}
	}
	countArrays(t)
	exprIdx := len(exprs) - numTypeArrays
	if exprIdx < 0 {
		exprIdx = 0
	}

	var walkType func(ty types.Type)
	walkType = func(ty types.Type) {
		ty = types.Unqualify(ty)
		switch a := ty.(type) {
		case *types.Array:
			if exprIdx < len(exprs) {
				if a.Form == types.VLA && exprs[exprIdx] != nil {
					u.vlaExprs[a] = exprs[exprIdx]
				}
				exprIdx++
			}
			walkType(a.Elem)
		case *types.Pointer:
			walkType(a.Elem)
		}
	}
	walkType(t)
}

// evalVLADims evaluates every VLA dimension a variably modified type carries.
//
// §6.7.6.2p5 evaluates a size expression where the declaration is reached, and
// that is true of a type that is variably modified without being an array:
// `int (*p)[n++]` declares a pointer, needs no storage of its own, and so
// never reaches declareVLA — but its declaration is still the one place n++
// runs, and the length it captured is what a later sizeof(*p) must use.
//
// The walk crosses pointers and arrays and stops at anything else. It does
// not enter a function type: a size expression at function prototype scope is
// treated as [*] and evaluated nowhere (§6.7.6.2p4).
func (u *unit) evalVLADims(t types.Type, at ast.Node) {
	switch ty := types.Unqualify(t).(type) {
	case *types.Array:
		if ty.Form == types.VLA {
			u.vlaDim(ty, at)
		}
		u.evalVLADims(ty.Elem, at)
	case *types.Pointer:
		u.evalVLADims(ty.Elem, at)
	}
}

// declareVLA emits the allocation for a variably modified object.
func (u *unit) declareVLA(name string, t types.Type, at ast.Node) {
	a, ok := asArray(t)
	if !ok {
		return
	}
	if a.Form == types.StarArray {
		u.errorf(at, "[*] may only appear in a function prototype (§6.7.6.2p4)")
		return
	}

	// FIX: Populate the missing VLA dimension expressions map before evaluating!
	u.recordVLAExprs(t, at)

	n := u.vlaCount(t, at)
	if n.IsZero() {
		return
	}
	b := u.blk()
	align := u.alignof(a.Elem, at)
	p := b.Ptr.Alloca(n, uint64(align))
	b.Name(p, sanitize(name))
	u.bind(&object{name: name, typ: t, decl: at, sto: autoStorage, addr: p, vlaSize: n})
	u.markStack(at)
}

// vlaCount computes the byte size of a variably modified type, multiplying
// through every VLA dimension and finishing with the element size.
func (u *unit) vlaCount(t types.Type, at ast.Node) ir.I64 {
	b := u.blk()
	total := b.I64.Const(1)
	cur := types.Unqualify(t)
	for {
		a, ok := cur.(*types.Array)
		if !ok {
			break
		}
		var n ir.I64
		switch a.Form {
		case types.VLA:
			n = u.vlaDim(a, at)
		case types.FixedArray:
			n = b.I64.Const(a.Len)
		default:
			u.errorf(at, "an object of type %s has no size", t)
			return ir.I64{}
		}
		total = b.I64.Mul(total, n)
		cur = types.Unqualify(a.Elem)
	}
	esz, ok := u.model.Sizeof(cur)
	if !ok {
		u.errorf(at, "an object of type %s has no size", t)
		return ir.I64{}
	}
	return b.I64.Mul(total, b.I64.Const(esz))
}

// vlaDim finds the length expression of one VLA dimension.
//
// types.Array records the form but not the expression, so the dimension is
// recovered from the declarator. This is the second place lower reaches back
// into syntax for something the type ought to carry — see the note about
// types exposing layout — and it has the same fix: an Array that kept its
// ast.Expr would make this a field access.
func (u *unit) vlaDim(a *types.Array, at ast.Node) ir.I64 {
	// Evaluated once. The declaration is where §6.7.6.2p5 puts the one
	// evaluation, and it is also the first thing to ask for the dimension, so
	// caching on first use lands the side effects exactly there. Everything
	// after — a sizeof, a pointer arithmetic scale — reads the value back.
	if u.fn != nil {
		if n, ok := u.fn.vlaDims[a]; ok {
			return n
		}
	}
	e := u.vlaExprs[a]
	if e == nil {
		u.errorf(at, "internal: no length expression recorded for a VLA dimension")
		return u.blk().I64.Const(1)
	}
	v := u.convert(u.expr(e), types.Typ(types.LongLong), at)
	n := u.i64(v.v, at)
	if u.fn != nil {
		if u.fn.vlaDims == nil {
			u.fn.vlaDims = map[*types.Array]ir.I64{}
		}
		u.fn.vlaDims[a] = n
	}
	return n
}

// vlaSizeof answers sizeof on a variably modified type at run time.
func (u *unit) vlaSizeof(t types.Type, at ast.Node) value {
	n := u.vlaCount(t, at)
	return u.convert(value{n, types.Typ(types.LongLong)}, u.sizeType(), at)
}

// markStack notes that the innermost block allocated, so its exit restores.
func (u *unit) markStack(at ast.Node) {
	// Deliberately left as the hook rather than the mechanism: threading the
	// mark through requires saveStack to run before the first alloca in a
	// block, which means stmt must know a block contains a VLA before it
	// walks it. That pre-scan is the remaining piece; until it lands, a VLA's
	// storage lives to the end of the function, which is correct but wasteful
	// in a loop.
	u.warnOnce(at, "vla-scope", "VLA storage is released at function exit rather than at block exit")
}
