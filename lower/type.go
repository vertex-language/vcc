package lower

import (
	"strings"

	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/types"
)

// typeMap is the C-type-to-VIR-type translation, memoized.
//
// Three distinct questions live here and are answered by three functions,
// because C conflates them and VIR does not:
//
//   - regType: what does a value of this type live in? char and short live in
//     i32, because §2 has no narrower register and §6.3.1.1 promotes them
//     there anyway.
//   - storeType: how wide is this type in memory? That is where i8 and i16
//     come back, reachable only through the sub-width load and store verbs.
//   - ftype: what shape does an object of this type have? Aggregates get a
//     named type; scalars get their store type.
type typeMap struct {
	u    *unit
	long ir.RegType // the register type long double lives in

	named map[types.Type]*ir.Type // records, by identity
	lay   map[*types.Record]*recLayout
	sigs  map[string]*ir.Type // func typedefs, by signature spelling
	used  map[string]bool     // taken ir type names
	i128  *ir.Type            // the shape of a 128-bit integer, interned once
}

func newTypeMap(u *unit, longDouble ir.RegType) *typeMap {
	tm := &typeMap{
		u:     u,
		long:  longDouble,
		named: map[types.Type]*ir.Type{},
		lay:   map[*types.Record]*recLayout{},
		sigs:  map[string]*ir.Type{},
		used:  map[string]bool{},
	}
	if tm.long == ir.TypeNone {
		tm.long = tm.inferLongDouble()
	}
	return tm
}

// inferLongDouble picks the namespace long double lives in from the model and
// the layout's extfloat list.
//
// f80 is preferred over f128 where both are admitted: a layout listing both is
// x86-64 System V, where long double is the 80-bit format and __float128 is
// the 128-bit one. A layout admitting neither has a long double that is
// double, which is a legal C implementation and not an error.
func (tm *typeMap) inferLongDouble() ir.RegType {
	m, l := tm.u.model, tm.u.layout
	if m.SizeLongDouble <= m.SizeDouble {
		return ir.TypeF64
	}
	if l.HasExtFloat(ir.TypeF80) {
		return ir.TypeF80
	}
	if l.HasExtFloat(ir.TypeF128) {
		return ir.TypeF128
	}
	return ir.TypeF64
}

// regType returns the register type a value of t lives in, and whether t has
// one at all. Records, void, and function types do not.
func (tm *typeMap) regType(t types.Type) (ir.RegType, bool) {
	switch t := types.Unqualify(t).(type) {
	case *types.Basic:
		switch t.K {
		case types.Bool, types.Char, types.SChar, types.UChar,
			types.Short, types.UShort, types.Int, types.UInt:
			return tm.intReg(t), true
		case types.Long, types.ULong, types.LongLong, types.ULongLong:
			return tm.intReg(t), true
		case types.Float:
			return ir.TypeF32, true
		case types.Double:
			return ir.TypeF64, true
		case types.LongDouble:
			return tm.long, true
		}
	case *types.Enum:
		return tm.intReg(types.Typ(t.Underlying())), true
	case *types.Pointer, *types.Array, *types.Func:
		// Arrays and functions reach expression position only after the
		// conversions of §6.3.2.1, which is to say as pointers.
		return ir.TypePtr, true
	case *types.Record:
		// The one record that is not carried by address. __m128i is a
		// union in the header — sixteen bytes with a member per lane
		// shape, all of them still readable — and a register everywhere
		// it is computed with, which is what __declspec(intrin_type)
		// claims and what makes the intrinsics instructions. The width is
		// checked rather than assumed: the mark says what the record is
		// for, and the register it names is a fixed size.
		if t.Vector {
			if sz, ok := tm.u.model.Sizeof(t); ok && sz == 16 {
				return ir.TypeV128, true
			}
		}
	}
	return ir.TypeNone, false
}

// is128 reports whether t is one of gcc's 128-bit integers. The analyzer
// gives them a width so that a record containing one — Darwin's
// arm/_mcontext.h has one, reached from <stdio.h> — is laid out correctly,
// and lower can carry one around by address for the same reason. What it
// cannot do is compute with one: there is no 128-bit register in the IR and
// the software sequences are not written.
//
// Every operand position that mentions such a value asks regType or
// storeType for it and is told no. That answer is indistinguishable from a
// genuine inconsistency, so the sites that get it used to report
// "internal: … has no register type" — blaming the compiler for a feature it
// simply does not have, and once per mention. unsupported128 is what those
// sites ask first.
func is128(t types.Type) bool {
	b, ok := types.Unqualify(t).(*types.Basic)
	return ok && (b.K == types.Int128 || b.K == types.UInt128)
}

// unsupported128 reports t as unimplemented if it is a 128-bit integer, once
// per translation unit, and says whether it did. A caller that gets true has
// its diagnostic and should poison rather than add one of its own.
func (u *unit) unsupported128(at ast.Node, t types.Type) bool {
	if !is128(t) {
		return false
	}
	u.errorOnce(at, "int128-value", "vcc does not implement values of type "+
		types.Unqualify(t).String()+"; the width is known, so one may be declared "+
		"or laid out in a record, but there is no 128-bit register to compute in")
	return true
}

// intReg picks i32 or i64 for an integer type by its width in the model.
// long is not i64 by name: it is four bytes on ILP32 and on LLP64.
func (tm *typeMap) intReg(t types.Type) ir.RegType {
	if bits, _ := tm.u.model.IntBits(t); bits > 32 {
		return ir.TypeI64
	}
	return ir.TypeI32
}

// storeType returns the memory width of a scalar type.
func (tm *typeMap) storeType(t types.Type) (ir.StoreType, bool) {
	switch t := types.Unqualify(t).(type) {
	case *types.Basic:
		switch t.K {
		case types.Bool, types.Char, types.SChar, types.UChar:
			return ir.StoreI8, true
		case types.Short, types.UShort:
			return ir.StoreI16, true
		case types.Int, types.UInt, types.Long, types.ULong,
			types.LongLong, types.ULongLong:
			if sz, _ := tm.u.model.Sizeof(t); sz > 4 {
				return ir.StoreI64, true
			}
			return ir.StoreI32, true
		case types.Float:
			return ir.StoreF32, true
		case types.Double:
			return ir.StoreF64, true
		case types.LongDouble:
			switch tm.long {
			case ir.TypeF80:
				return ir.StoreF80, true
			case ir.TypeF128:
				return ir.StoreF128, true
			}
			return ir.StoreF64, true
		}
	case *types.Enum:
		return tm.storeType(types.Typ(t.Underlying()))
	case *types.Pointer:
		return ir.StorePtr, true
	case *types.Record:
		if rt, ok := tm.regType(t); ok && rt == ir.TypeV128 {
			return ir.StoreV128, true
		}
	}
	return ir.StoreNone, false
}

// ftype returns the shape of an object of type t.
func (tm *typeMap) ftype(t types.Type) ir.FType {
	t = types.Unqualify(t)
	if st, ok := tm.storeType(t); ok {
		return st.FType()
	}
	switch t := t.(type) {
	case *types.Array:
		n := t.Len
		if t.Form != types.FixedArray || n < 0 {
			// An incomplete or variably modified array has no shape here.
			// A tentative definition of one is completed by its initializer;
			// a VLA never becomes a module-scope object at all.
			n = 0
		}
		return ir.Array(uint64(n), tm.ftype(t.Elem))
	case *types.Record:
		return tm.record(t).FType()
	case *types.Basic:
		if t.K == types.Int128 || t.K == types.UInt128 {
			return tm.int128().FType()
		}
	}
	tm.u.errorf(tm.u.file, "internal: %s has no VIR object shape", t)
	return ir.StoreI8.FType()
}

// int128 is the object shape of a 128-bit integer: sixteen bytes, aligned to
// sixteen. There is no 128-bit register to compute in, but an object of one
// still has to exist — Darwin's arm/_mcontext.h puts a __uint128_t in a
// struct reached from <stdio.h>, and until this existed, declaring any object
// of that struct failed with an internal error about a missing object shape.
//
// A bare byte array would give the right size and the wrong alignment, since
// a global's alignment comes from its shape unless _Alignas overrode it. A
// named type carries one explicitly, which is how records already do it.
func (tm *typeMap) int128() *ir.Type {
	if tm.i128 == nil {
		tm.i128 = tm.u.mod.Struct(tm.uniqueName("__vcc_int128")).
			Field("v", ir.Array(16, ir.StoreI8.FType())).
			Align(16)
	}
	return tm.i128
}

// record interns a C record as a named VIR type.
//
// The mapping is a storage description, not a transcription. Plain members
// appear at the byte offsets vcc computed, so that a global initializer can
// name one and so that offsetof @T agrees with the loads lower emits.
// Bit-field storage and interior padding appear as byte-array fillers, since
// VIR has no bit-field concept and does not need one: a bit-field access is
// a load, a shift, and a mask, all of which lower emits itself.
//
// A tail filler is appended whenever the last member ends before sizeof does.
// It costs one field and removes any dependence on how VIR rounds a struct's
// size, which is what makes sizeof(T) in C and sizeof @T in VIR the same
// number by construction rather than by coincidence.
func (tm *typeMap) record(r *types.Record) *ir.Type {
	if t, ok := tm.named[r]; ok {
		return t
	}
	l := tm.layout(r)
	name := tm.typeName(r)

	if r.Union {
		t := tm.u.mod.Union(name)
		tm.named[r] = t
		for i, f := range r.Fields {
			if l.Places[i].Bit || l.Places[i].Skip || f.Name == "" {
				continue
			}
			t.Field(f.Name, tm.ftype(f.Type))
		}
		// One filler carrying the union's real width, so that a union whose
		// widest member is a bit-field group still has a size.
		t.Field("__vcc_storage", ir.Array(uint64(l.Size), ir.StoreI8.FType()))
		t.Align(uint64(l.Align))
		return t
	}

	t := tm.u.mod.Struct(name)
	tm.named[r] = t
	var end int64
	pad := 0
	for i, f := range r.Fields {
		p := l.Places[i]
		if p.Skip {
			continue
		}
		var (
			off int64
			ft  ir.FType
		)
		if p.Bit {
			// Emit one filler per storage unit, not per bit-field: several
			// bit-fields share a unit and only the first reaches this.
			if p.Off < end {
				continue
			}
			off, ft = p.Off, ir.Array(uint64(p.Unit), ir.StoreI8.FType())
		} else {
			off, ft = p.Off, tm.ftype(f.Type)
		}
		if off > end {
			t.FieldAt(fillerName(&pad), ir.Array(uint64(off-end), ir.StoreI8.FType()), uint64(end))
		}
		fname := f.Name
		if fname == "" || p.Bit {
			fname = fillerName(&pad)
		}
		t.FieldAt(fname, ft, uint64(off))
		if sz, ok := tm.u.model.Sizeof(types.Unqualify(f.Type)); ok && !p.Bit {
			end = off + sz
		} else if p.Bit {
			end = off + p.Unit
		}
	}
	if end < l.Size {
		t.FieldAt(fillerName(&pad), ir.Array(uint64(l.Size-end), ir.StoreI8.FType()), uint64(end))
	}
	t.Align(uint64(l.Align))
	return t
}

func fillerName(n *int) string {
	*n++
	return "__vcc_pad" + itoa(*n)
}

// typeName mints the VIR name for a record: its tag where it has one, a
// generated name where it does not, suffixed on collision. Two distinct
// records may share a tag — one per scope — and VIR's namespace is flat.
func (tm *typeMap) typeName(r *types.Record) string {
	base := r.Name
	if base == "" {
		base = "anon"
	}
	if !isIRIdent(base) {
		base = "anon"
	}
	return tm.uniqueName(base)
}

// uniqueName claims base in VIR's flat type namespace, suffixing on
// collision. The separator is '_' rather than '.' because the result has to
// be a valid VIR identifier.
func (tm *typeMap) uniqueName(base string) string {
	name := base
	for i := 1; tm.used[name]; i++ {
		name = base + "_" + itoa(i)
	}
	tm.used[name] = true
	return name
}

// sig builds the VIR signature of a C function type.
//
// Aggregates cross the boundary by reference and say so: a record argument is
// a pointer carrying byval, and a record return is a leading pointer carrying
// sret. Whether that becomes registers or stack is the classification
// ir/lower/abi does — vcc states that the value is passed, not how.
//
// Narrow integer parameters and results carry zext or sext. The value in the
// register is already i32, so the attribute is not a conversion; it tells the
// ABI which half of the register the callee may believe.
func (tm *typeMap) sig(f *types.Func) *ir.Sig {
	s := ir.NewSig()
	ret := types.Unqualify(f.Ret)

	if r, ok := ret.(*types.Record); ok && !r.Vector {
		s.Param(ir.TypePtr, ir.SRet(tm.record(r)))
	} else if !isVoid(ret) {
		if rt, ok := tm.regType(ret); ok {
			s.Ret(rt, tm.extAttrs(ret)...)
		}
	}
	for _, p := range f.Params {
		pt := types.Unqualify(types.AdjustParam(p.Type))
		if isVoid(pt) {
			continue
		}
		if named, ok := tm.indirect(pt); ok {
			s.Param(ir.TypePtr, ir.ByVal(named))
			continue
		}
		rt, ok := tm.regType(pt)
		if !ok {
			if !tm.u.unsupported128(nil, pt) {
				tm.u.errorf(tm.u.file, "internal: parameter of type %s has no register type", pt)
			}
			rt = ir.TypeI32
		}
		s.Param(rt, tm.extAttrs(pt)...)
	}
	if f.Variadic {
		s.Variadic()
	}
	return s
}

// extAttrs returns zext or sext for an integer type narrower than its
// register, and nothing otherwise.
func (tm *typeMap) extAttrs(t types.Type) []ir.ParamAttr {
	if !types.IsInteger(t) {
		return nil
	}
	bits, signed := tm.u.model.IntBits(t)
	rt, _ := tm.regType(t)
	if rt == ir.TypeI32 && bits >= 32 || rt == ir.TypeI64 && bits >= 64 {
		return nil
	}
	if signed {
		return []ir.ParamAttr{ir.SExt}
	}
	return []ir.ParamAttr{ir.ZExt}
}

// funcType interns a func typedef for an indirect call.
//
// callind names one of these, which is what keeps an indirect call well-typed and
// carries its calling convention. Interning is by the signature's spelling rather
// than by the C type, since two compatible C function types must reach the same
// VIR type.
func (tm *typeMap) funcType(f *types.Func) *ir.Type {
	key := sigKey(f)
	if t, ok := tm.sigs[key]; ok {
		return t
	}
	t := tm.u.mod.FuncType(tm.u.uniq("fn"), tm.sig(f))
	tm.sigs[key] = t
	return t
}

func sigKey(f *types.Func) string {
	var b strings.Builder
	b.WriteString(f.Ret.String())
	b.WriteByte('(')
	for i, p := range f.Params {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(types.AdjustParam(p.Type).String())
	}
	if f.Variadic {
		b.WriteString(",...")
	}
	if !f.Proto {
		b.WriteString("|noproto")
	}
	b.WriteByte(')')
	return b.String()
}

func isVoid(t types.Type) bool {
	b, ok := types.Unqualify(t).(*types.Basic)
	return ok && b.K == types.Void
}

func isIRIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// ---- values that cross a call boundary by pointer -------------------------

// indirect reports whether a value of t is passed and returned by pointer
// rather than in a register, and the IR type describing the storage.
//
// Records always are, in every convention vcc targets: vcc states that the
// value is passed and ir/lower/abi decides whether the pointer's contents
// end up in registers or on the stack.
//
// __m128i is the other case, and only under the Microsoft convention. MSVC
// passes a vector argument by pointer to a copy the caller makes — dumpbin
// on `void take(__m128i)` shows the two LEAs and no XMM — while returning
// one in XMM0. So the parameter is indirect and the result is not, which is
// not a shape any other type in C has and is why this is a separate question
// from regType's. System V classifies the same sixteen bytes as SSE and
// passes them in a vector register, so there is nothing to do there.
func (tm *typeMap) indirect(t types.Type) (*ir.Type, bool) {
	r, ok := types.Unqualify(t).(*types.Record)
	if !ok {
		return nil, false
	}
	if r.Vector {
		if tm.u.layout.ABI != "ms" {
			return nil, false // in a vector register, and nothing to copy
		}
		return tm.record(r), true
	}
	return tm.record(r), true
}
