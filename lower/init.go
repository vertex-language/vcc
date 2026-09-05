package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/types"
)

// Initialization, both flavours. A static initializer becomes an ir.Init tree
// checked against the declared type by §19.10; an automatic one becomes
// stores. The shape rules — brace elision, designators, the order §6.7.9p23
// fixes — are the same for both, so they are walked by the same cursor.

// initObject emits the stores that initialize an automatic object.
//
// A partly initialized aggregate is zeroed first and then written, rather
// than having every unmentioned member stored individually: §6.7.9p21 makes
// the rest zero, one memset says so in one instruction, and the alternative
// generates a store per padding byte for a struct with three initialized
// members out of thirty.
func (u *unit) initObject(addr ir.Ptr, t types.Type, init ast.Expr, at ast.Node) {
	// §6.7.9p14: A character array initialized by a string literal, optionally enclosed in braces.
	if list, isList := init.(*ast.InitList); isList && len(list.Items) == 1 && len(list.Items[0].Designators) == 0 {
		if a, isArr := asArray(t); isArr && types.IsInteger(a.Elem) {
			if _, ok := stripParens(list.Items[0].Value).(*ast.StringLit); ok {
				init = list.Items[0].Value
			}
		}
	}

	list, isList := init.(*ast.InitList)
	if !isList {
		if s, ok := stripParens(init).(*ast.StringLit); ok {
			if a, isArr := asArray(t); isArr {
				u.initStringArray(addr, a, s, at)
				return
			}
		}
		u.store(u.expr(init), addr, t, at)
		return
	}

	if u.needsZeroing(t, list) {
		b := u.blk()
		b.MemSet(addr, b.I32.Const(0), b.I64.Const(u.sizeof(t, at)))
	}
	c := &initCursor{u: u, t: t, items: list.Items}
	c.fill(addr, at)
}

// needsZeroing reports whether the initializer leaves anything unmentioned.
// A scalar never does; an aggregate whose item count matches its member count
// and whose items carry no designators does not either.
func (u *unit) needsZeroing(t types.Type, list *ast.InitList) bool {
	switch t := types.Unqualify(t).(type) {
	case *types.Array:
		if t.Form != types.FixedArray {
			return true
		}
		return int64(len(list.Items)) < t.Len || anyDesignated(list)
	case *types.Record:
		if t.Union {
			return true
		}
		return len(list.Items) < len(t.Fields) || anyDesignated(list)
	}
	return false
}

func anyDesignated(list *ast.InitList) bool {
	for _, it := range list.Items {
		if len(it.Designators) > 0 {
			return true
		}
	}
	return false
}

// initCursor walks an initializer list against a type.
//
// The cursor is the standard's own model: §6.7.9p17 describes initialization
// as a current object that advances, with a designator resetting it and brace
// elision letting a subobject consume items from the enclosing list. Keeping
// that state explicit is what makes `struct {int a[2]; int b;} x = {1,2,3};`
// and `= {{1,2},3}` the same three stores.
type initCursor struct {
	u     *unit
	t     types.Type
	items []*ast.InitItem
	i     int
}

func (c *initCursor) done() bool { return c.i >= len(c.items) }

// fill initializes the object at addr from the cursor's remaining items.
func (c *initCursor) fill(addr ir.Ptr, at ast.Node) {
	u := c.u
	switch t := types.Unqualify(c.t).(type) {
	case *types.Array:
		idx := int64(0)
		for !c.done() {
			it := c.items[c.i]
			high := int64(-1)
			if len(it.Designators) > 0 {
				d, ok := it.Designators[0].(*ast.IndexDesignator)
				if !ok {
					u.errorf(it, "a field designator does not apply to an array")
					c.i++
					continue
				}
				if v, ok := u.constInt(d.Index); ok {
					idx = v
				}
				if d, ok := it.Designators[0].(*ast.IndexDesignator); ok && d.High != nil {
					if v, ok := u.constInt(d.High); ok {
						high = v
					}
				}
			}
			if t.Form == types.FixedArray && idx >= t.Len {
				break
			}
			esz := u.sizeof(t.Elem, at)
			ea := c.offset(addr, idx*esz, at)
			c.element(ea, t.Elem, it, at)
			// A designator range initializes every element between the
			// bounds. The expression is emitted once per element, which is
			// what gcc does and what an element of a non-constant type
			// requires.
			for j := idx + 1; j <= high && (t.Form != types.FixedArray || j < t.Len); j++ {
				sub := &initCursor{u: u, t: c.t, items: c.items, i: c.i - 1}
				sub.element(c.offset(addr, j*esz, at), t.Elem, it, at)
			}
			if high >= idx {
				idx = high
			}
			idx++
		}

	case *types.Record:
		fi := 0
		for !c.done() {
			it := c.items[c.i]
			if len(it.Designators) > 0 {
				d, ok := it.Designators[0].(*ast.FieldDesignator)
				if !ok {
					u.errorf(it, "an array designator does not apply to %s", t)
					c.i++
					continue
				}
				name := u.name(d.Name)
				found := -1
				for i, f := range t.Fields {
					if f.Name == name {
						found = i
						break
					}
				}
				if found < 0 {
					u.errorf(it, "%s has no member named %s", t, name)
					c.i++
					continue
				}
				fi = found
			}
			for fi < len(t.Fields) && t.Fields[fi].Name == "" && !t.Fields[fi].BitField {
				fi++
			}
			if fi >= len(t.Fields) {
				break
			}
			f := t.Fields[fi]
			p := u.types.layout(t).Places[fi]
			if p.Skip {
				fi++
				continue
			}
			fa := c.offset(addr, p.Off, at)
			if f.BitField {
				c.i++
				v := u.expr(itemValue(it))
				u.storeLval(v, lval{addr: fa, t: f.Type, bit: true, p: p}, it)
			} else {
				c.element(fa, f.Type, it, at)
			}
			fi++
			if t.Union {
				break // §6.7.9p16: only the first named member of a union
			}
		}

	default:
		if c.done() {
			return
		}
		it := c.items[c.i]
		c.i++
		u.store(u.expr(itemValue(it)), addr, c.t, at)
	}
}

// element initializes one subobject, either from a nested brace list or by
// eliding braces and letting the subobject consume items from this list.
func (c *initCursor) element(addr ir.Ptr, t types.Type, it *ast.InitItem, at ast.Node) {
	u := c.u
	if nested, ok := it.Value.(*ast.InitList); ok {
		c.i++
		sub := &initCursor{u: u, t: t, items: nested.Items}
		if u.needsZeroing(t, nested) {
			b := u.blk()
			b.MemSet(addr, b.I32.Const(0), b.I64.Const(u.sizeof(t, at)))
		}
		sub.fill(addr, at)
		return
	}
	if s, ok := stripParens(it.Value).(*ast.StringLit); ok {
		if a, isArr := asArray(t); isArr {
			c.i++
			u.initStringArray(addr, a, s, at)
			return
		}
	}
	if isScalar(t) {
		c.i++
		u.store(u.expr(itemValue(it)), addr, t, at)
		return
	}
	if _, ok := asRecord(types.Unqualify(t)); ok {
		// A record initialized from a single expression of the same type is
		// a copy, not an elision.
		if v := u.staticType(itemValue(it)); u.compatible(types.Unqualify(v), types.Unqualify(t)) {
			c.i++
			u.store(u.expr(itemValue(it)), addr, t, at)
			return
		}
	}
	// Brace elision: the subobject takes as many items as it needs from here.
	sub := &initCursor{u: u, t: t, items: c.items, i: c.i}
	sub.fill(addr, at)
	c.i = sub.i
}

func (c *initCursor) offset(addr ir.Ptr, off int64, at ast.Node) ir.Ptr {
	if off == 0 {
		return addr
	}
	b := c.u.blk()
	return b.Ptr.Add(addr, b.I64.Const(off))
}

func itemValue(it *ast.InitItem) ast.Expr { return it.Value }

// initStringArray is §6.7.9p14: a character array initialized by a string
// literal takes the literal's characters, and the terminating NUL only if it
// fits.
func (u *unit) initStringArray(addr ir.Ptr, a *types.Array, s *ast.StringLit, at ast.Node) {
	sv := u.decodeString(s)
	esz := u.sizeof(a.Elem, at)
	n := int64(len(sv.Data))
	if a.Form == types.FixedArray && a.Len < n {
		n = a.Len
	}
	b := u.blk()
	sym := u.stringSymbol(sv)
	b.MemCpy(addr, b.Ptr.GetAddr(sym), b.I64.Const(n*esz))
	if a.Form == types.FixedArray && a.Len > n {
		rest := b.Ptr.Add(addr, b.I64.Const(n*esz))
		b.MemSet(rest, b.I32.Const(0), b.I64.Const((a.Len-n)*esz))
	}
}

// ---- static initializers -------------------------------------------------

// staticInit builds the ir.Init tree for an object of static duration.
//
// §19.10 checks its structure against the declared ftype, which is why every
// aggregate produces a List of exactly the right length — including the
// padding fillers the named type carries. Getting that wrong is a verifier
// error rather than a silently misaligned global, which is the reason to
// build it positionally rather than by name.
func (u *unit) staticInit(t types.Type, init ast.Expr) ir.Init {
	if init == nil {
		return ir.ZeroInit
	}

	// §6.7.9p14: A character array initialized by a string literal, optionally enclosed in braces.
	if list, isList := init.(*ast.InitList); isList && len(list.Items) == 1 && len(list.Items[0].Designators) == 0 {
		if a, isArr := asArray(t); isArr && types.IsInteger(a.Elem) {
			if _, ok := stripParens(list.Items[0].Value).(*ast.StringLit); ok {
				init = list.Items[0].Value
			}
		}
	}

	list, isList := init.(*ast.InitList)
	if !isList {
		if s, ok := stripParens(init).(*ast.StringLit); ok {
			if a, isArr := asArray(t); isArr {
				return u.staticString(a, s)
			}
		}
		v, ok := u.constExpr(init, t)
		if !ok {
			u.errorf(init, "initializer for an object with static storage duration is not constant")
			return ir.ZeroInit
		}
		return v
	}
	c := &staticCursor{u: u, items: list.Items}
	return c.fill(t, init)
}

type staticCursor struct {
	u     *unit
	items []*ast.InitItem
	i     int
}

func (c *staticCursor) done() bool { return c.i >= len(c.items) }

func (c *staticCursor) fill(t types.Type, at ast.Node) ir.Init {
	u := c.u
	switch t := types.Unqualify(t).(type) {
	case *types.Array:
		n := t.Len
		if t.Form != types.FixedArray {
			n = int64(len(c.items))
		}
		out := make([]ir.Init, n)
		for i := range out {
			out[i] = ir.ZeroInit
		}
		// The cursor running past the end does not end the initializer: a
		// designator that follows resets it, and `{[7] = 70, [3] = 30, 31}`
		// is three elements, not one. So the bound is checked after the
		// designator has had its say, not before the item is looked at.
		idx := int64(0)
		for !c.done() {
			it := c.items[c.i]
			high := int64(-1)
			if len(it.Designators) > 0 {
				if d, ok := it.Designators[0].(*ast.IndexDesignator); ok {
					if v, ok := u.constInt(d.Index); ok {
						idx = v
					}
					if d.High != nil {
						if v, ok := u.constInt(d.High); ok {
							high = v
						}
					}
				}
			}
			if idx < 0 || idx >= n {
				break
			}
			v := c.element(t.Elem, it, at)
			out[idx] = v
			// gcc's [lo ... hi]: one initializer, every element between the
			// two bounds. The value is built once and shared, which is what
			// a constant initializer is.
			for j := idx + 1; j <= high && j < n; j++ {
				out[j] = v
			}
			if high >= idx {
				idx = high
			}
			idx++
		}
		return ir.List(out...)

	case *types.Record:
		if t.Union {
			return c.unionInit(t, at)
		}
		// Positional, against the VIR type's fields rather than C's, so that
		// padding fillers get their zero entries and §19.10 is satisfied.
		irt := u.types.record(t)
		fields := irt.Fields()
		out := make([]ir.Init, len(fields))
		for i := range out {
			out[i] = ir.ZeroInit
		}
		lay := u.types.layout(t)
		byOffset := map[uint64]int{}
		for i, f := range fields {
			byOffset[f.Offset] = i
		}
		// As with the array above: a designator after the cursor has run off
		// the end puts it back, so the bound is checked once the designator
		// has been applied rather than as a loop condition.
		// Bit-fields do not get a slot of their own: several share one
		// storage unit, and the VIR type carries that unit as a byte array.
		// Their constant values are packed into units here and written into
		// the slots after the walk.
		units := map[int64][]byte{}
		fi := 0
		for !c.done() {
			it := c.items[c.i]
			if len(it.Designators) > 0 {
				if d, ok := it.Designators[0].(*ast.FieldDesignator); ok {
					name := u.name(d.Name)
					for i, f := range t.Fields {
						if f.Name == name {
							fi = i
							break
						}
					}
				}
			}
			if fi < 0 || fi >= len(t.Fields) {
				break
			}
			f := t.Fields[fi]
			p := lay.Places[fi]
			switch {
			case p.Skip || (f.BitField && f.Name == ""):
				// §6.7.9p9: an unnamed member is not initialized, and takes
				// no item with it. The zero-width field is not a member at
				// all.
				fi++
				continue
			case f.BitField:
				u.packBitField(units, f, p, it)
				c.i++
				fi++
				continue
			}
			if slot, ok := byOffset[uint64(p.Off)]; ok {
				out[slot] = c.element(f.Type, it, at)
			}
			fi++
		}
		for off, b := range units {
			if slot, ok := byOffset[uint64(off)]; ok {
				out[slot] = ir.Str(string(b))
			}
		}
		return ir.List(out...)
	}

	if c.done() {
		return ir.ZeroInit
	}
	it := c.items[c.i]
	c.i++
	v, ok := u.constExpr(itemValue(it), t)
	if !ok {
		u.errorf(it, "initializer is not a constant expression")
		return ir.ZeroInit
	}
	return v
}

// unionInit builds the initializer for a union with static storage duration.
//
// A union is one member, not a list of them: §6.7.9p17 initializes the first
// named member unless a designator says otherwise, and §19 spells that as a
// named-field initializer with exactly one entry. Filling a slot per member
// the way a struct does would both name members the program did not
// initialize and, since every member sits at offset zero, put the value in
// whichever one happened to be last.
func (c *staticCursor) unionInit(t *types.Record, at ast.Node) ir.Init {
	u := c.u
	if c.done() {
		return ir.ZeroInit
	}
	lay := u.types.layout(t)

	// The member: the one a designator names, or the first that has storage
	// and a name of its own.
	fi := -1
	it := c.items[c.i]
	if len(it.Designators) > 0 {
		if d, ok := it.Designators[0].(*ast.FieldDesignator); ok {
			name := u.name(d.Name)
			for i, f := range t.Fields {
				if f.Name == name {
					fi = i
					break
				}
			}
		}
	}
	if fi < 0 {
		for i, f := range t.Fields {
			if f.Name != "" && !lay.Places[i].Skip && !f.BitField {
				fi = i
				break
			}
		}
	}
	if fi < 0 || lay.Places[fi].Skip || t.Fields[fi].BitField || t.Fields[fi].Name == "" {
		if fi >= 0 && t.Fields[fi].BitField {
			u.errorf(it, "a constant initializer for a bit-field is not yet lowered")
		}
		c.i++
		return ir.ZeroInit
	}
	f := t.Fields[fi]
	return ir.Fields(ir.Val(f.Name, c.element(f.Type, it, at)))
}

func (c *staticCursor) element(t types.Type, it *ast.InitItem, at ast.Node) ir.Init {
	u := c.u
	if nested, ok := it.Value.(*ast.InitList); ok {
		c.i++
		sub := &staticCursor{u: u, items: nested.Items}
		return sub.fill(t, at)
	}
	if s, ok := stripParens(it.Value).(*ast.StringLit); ok {
		if a, isArr := asArray(t); isArr {
			c.i++
			return u.staticString(a, s)
		}
	}
	if isScalar(t) {
		c.i++
		v, ok := u.constExpr(itemValue(it), t)
		if !ok {
			u.errorf(it, "initializer is not a constant expression")
			return ir.ZeroInit
		}
		return v
	}
	sub := &staticCursor{u: u, items: c.items, i: c.i}
	out := sub.fill(t, at)
	c.i = sub.i
	return out
}

// packBitField folds one bit-field's constant value into the byte pattern of
// its storage unit.
//
// A bit-field has no slot of its own in the VIR type — several of them share
// one unit, and the unit is a byte array — so a constant initializer is a
// question about bytes rather than about values. lower/layout.go fixes the
// packing as low-order bits first, and the target's endianness decides which
// byte of the unit those bits land in.
func (u *unit) packBitField(units map[int64][]byte, f types.Field, p place, it *ast.InitItem) {
	v, ok := u.constInt(itemValue(it))
	if !ok {
		u.errorf(it, "initializer is not a constant expression")
		return
	}
	b := units[p.Off]
	if b == nil {
		b = make([]byte, p.Unit)
		units[p.Off] = b
	}
	// The value contributes its low Width bits; anything above them is the
	// truncation §6.3.1.3 performs, not an error.
	uv := uint64(v)
	if p.Width < 64 {
		uv &= (uint64(1) << uint(p.Width)) - 1
	}
	for i := int64(0); i < p.Width; i++ {
		if uv&(1<<uint(i)) == 0 {
			continue
		}
		bit := p.BitOff + i
		idx := bit / 8
		if u.layout.Endian == ir.BigEndian {
			idx = p.Unit - 1 - idx
		}
		if idx < 0 || idx >= p.Unit {
			continue
		}
		b[idx] |= 1 << uint(bit%8)
	}
}

// staticString builds the initializer for a character array declared with a
// string literal.
func (u *unit) staticString(a *types.Array, s *ast.StringLit) ir.Init {
	sv := u.decodeString(s)
	n := int64(len(sv.Data))
	if a.Form == types.FixedArray {
		n = a.Len
	}
	if esz, _ := u.model.Sizeof(sv.Elem); esz == 1 {
		b := make([]byte, 0, n)
		for i := int64(0); i < n; i++ {
			if i < int64(len(sv.Data)) {
				b = append(b, byte(sv.Data[i]))
			} else {
				b = append(b, 0)
			}
		}
		return ir.Str(string(b))
	}
	out := make([]ir.Init, n)
	for i := range out {
		if int64(i) < int64(len(sv.Data)) {
			out[i] = ir.Lit(ir.Uint(uint64(sv.Data[i])))
		} else {
			out[i] = ir.ZeroInit
		}
	}
	return ir.List(out...)
}

// completeArray gives an incomplete array the length its initializer supplies
// (§6.7.9p22), and leaves every other type alone.
//
// It applies wherever an object is declared, which is why it is one function:
// file scope, block scope, a block-scope static, and a compound literal all
// complete the same way, and the one that did not — the static — produced an
// object of unknown size that nothing could take the sizeof.
func (u *unit) completeArray(t types.Type, init ast.Expr) types.Type {
	a, ok := asArray(t)
	if !ok || a.Form != types.IncompleteArray || init == nil {
		return t
	}
	return &types.Array{Elem: a.Elem, Form: types.FixedArray, Len: u.inferArrayLen(a.Elem, init)}
}

// inferArrayLen deduces the length of an incomplete array from its initializer.
func (u *unit) inferArrayLen(elem types.Type, init ast.Expr) int64 {
	list, isList := init.(*ast.InitList)
	// §6.7.9p14: A character array initialized by a string literal, optionally enclosed in braces.
	if isList && len(list.Items) == 1 && len(list.Items[0].Designators) == 0 && types.IsInteger(elem) {
		if _, ok := stripParens(list.Items[0].Value).(*ast.StringLit); ok {
			init = list.Items[0].Value
			isList = false
		}
	}

	if !isList {
		if s, ok := stripParens(init).(*ast.StringLit); ok {
			if st, ok := asArray(u.stringType(s)); ok {
				return st.Len
			}
		}
		return 1
	}

	maxIdx := int64(-1)
	idx := int64(0)
	for _, it := range list.Items {
		if len(it.Designators) > 0 {
			if d, ok := it.Designators[0].(*ast.IndexDesignator); ok {
				if v, ok := u.constInt(d.Index); ok {
					idx = v
				}
			}
		}
		if idx > maxIdx {
			maxIdx = idx
		}
		idx++
	}
	return maxIdx + 1
}
