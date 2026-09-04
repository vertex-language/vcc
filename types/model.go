package types

// Model is a target's type model: sizes in bytes, char signedness,
// wchar_t's identity. Layout (Sizeof, Alignof, field offsets) is a
// pure function of the Model — nothing here probes a host.
type Model struct {
	CharSigned bool
	WCharKind  Kind // the basic kind wchar_t aliases

	SizeShort, SizeInt, SizeLong, SizeLongLong int64
	SizePtr                                    int64
	SizeFloat, SizeDouble, SizeLongDouble      int64
	AlignLongDouble                            int64

	// MSBitfields selects the Microsoft rule for where a bit-field goes.
	//
	// §6.7.2.1p11 leaves it implementation-defined, and the two answers in
	// circulation disagree about most structs rather than about a corner.
	// Under the rule gcc and clang use, a bit-field is placed at the
	// current bit as long as it does not cross a boundary of its declared
	// type: `struct { char a; int b : 3; }` puts b in the byte after a and
	// is four bytes. Under MSVC's, a bit-field opens a new allocation unit
	// of its declared type unless the member before it was a bit-field of
	// the same size with room left — so the same struct is eight bytes,
	// with b at offset four.
	//
	// It is an ABI, not a preference: it decides what a struct means to
	// everything else on the platform, so it belongs to the target and not
	// to the compiler. Windows sets it; nothing else does.
	MSBitfields bool
}

// LP64 is the model of x86-64 Linux and friends.
func LP64() Model {
	return Model{
		CharSigned: true,
		WCharKind:  Int,
		SizeShort:  2, SizeInt: 4, SizeLong: 8, SizeLongLong: 8,
		SizePtr:   8,
		SizeFloat: 4, SizeDouble: 8, SizeLongDouble: 16,
		AlignLongDouble: 16,
	}
}

// Sizeof returns a type's size in bytes. ok is false for incomplete
// types, function types, and VLAs — sizes sizeof cannot know.
func (m Model) Sizeof(t Type) (int64, bool) {
	switch t := Unqualify(t).(type) {
	case *Basic:
		return m.basicSize(t.K)
	case *Pointer:
		return m.SizePtr, true
	case *Enum:
		sz, _ := m.basicSize(t.Underlying())
		return sz, t.Complete
	case *Array:
		if t.Form != FixedArray {
			return 0, false
		}
		e, ok := m.Sizeof(t.Elem)
		return e * t.Len, ok
	case *Record:
		if !t.Complete {
			return 0, false
		}
		size, _, ok := m.layout(t, nil)
		return size, ok
	}
	return 0, false
}

func (m Model) basicSize(k Kind) (int64, bool) {
	if k == Int128 || k == UInt128 {
		return 16, true
	}
	switch k {
	case Void:
		return 0, false
	case Bool, Char, SChar, UChar:
		return 1, true
	case Short, UShort:
		return m.SizeShort, true
	case Int, UInt:
		return m.SizeInt, true
	case Long, ULong:
		return m.SizeLong, true
	case LongLong, ULongLong:
		return m.SizeLongLong, true
	case Float:
		return m.SizeFloat, true
	case Double:
		return m.SizeDouble, true
	case LongDouble:
		return m.SizeLongDouble, true
	case ComplexFloat:
		return 2 * m.SizeFloat, true
	case ComplexDouble:
		return 2 * m.SizeDouble, true
	case ComplexLongDouble:
		return 2 * m.SizeLongDouble, true
	}
	return 0, false
}

// Alignof returns a type's alignment requirement.
func (m Model) Alignof(t Type) (int64, bool) {
	switch t := Unqualify(t).(type) {
	case *Basic:
		if t.K == LongDouble || t.K == ComplexLongDouble {
			return m.AlignLongDouble, true
		}
		if t.K == ComplexFloat {
			return m.SizeFloat, true
		}
		if t.K == ComplexDouble {
			return m.SizeDouble, true
		}
		return m.basicSize(t.K)
	case *Pointer:
		return m.SizePtr, true
	case *Enum:
		sz, _ := m.basicSize(t.Underlying())
		return sz, t.Complete
	case *Array:
		return m.Alignof(t.Elem)
	case *Record:
		if !t.Complete {
			return 0, false
		}
		_, align, ok := m.layout(t, nil)
		return align, ok
	}
	return 0, false
}

// Offsetof is the byte offset of a member from the base of a record: what
// offsetof(t, name) yields, and what the address constant &((t *)0)->name
// evaluates to.
//
// The member may be reached through an anonymous struct or union, whose
// members belong to the record that contains it (§6.7.2.1p13); the offsets
// of each step add. A named member is searched for before the anonymous
// ones are descended into, which is the rule that makes an outer member
// shadow an inner one of the same name.
//
// It reports false for an incomplete record, a name no member carries, and
// a bit-field — §6.7.2.1p13 gives a bit-field no address, so it has no
// offset in bytes to give either.
func (m Model) Offsetof(t Type, name string) (int64, bool) {
	r, ok := Unqualify(t).(*Record)
	if !ok || !r.Complete {
		return 0, false
	}
	offs := make([]int64, len(r.Fields))
	if _, _, ok := m.layout(r, offs); !ok {
		return 0, false
	}
	for i, f := range r.Fields {
		if f.Name == name {
			if f.BitField {
				return 0, false
			}
			return offs[i], true
		}
	}
	for i, f := range r.Fields {
		if f.Name != "" {
			continue
		}
		if _, ok := Unqualify(f.Type).(*Record); !ok {
			continue
		}
		if n, ok := m.Offsetof(f.Type, name); ok {
			return offs[i] + n, true
		}
	}
	return 0, false
}

// layout computes a record's size and alignment, and — when offs is not nil
// — writes each member's byte offset into it, in declaration order.
//
// Bit-fields pack into consecutive allocation units of their declared type,
// System V–style: a field that would cross a unit boundary starts a new
// unit, and a zero-width field pads to the next one.
//
// The offsets are Offsetof's, and a bit-field member's is left at zero:
// Offsetof refuses a bit-field before it reads one, so nothing depends on
// the value.
func (m Model) layout(r *Record, offs []int64) (size, align int64, ok bool) {
	align = 1
	var cur BitCursor // the placement cursor; see bitfield.go
	for i, f := range r.Fields {
		fs, ok1 := m.Sizeof(f.Type)
		if !ok1 {
			// The flexible array member contributes no size. It is the last
			// member of a struct, and any member of a union — where there
			// is no order for it to be last in.
			if a, ok := f.Type.(*Array); ok && a.Form == IncompleteArray &&
				(r.Union || i == len(r.Fields)-1) {
				ok1 = true
				fs = 0
			}
		}
		fa, ok2 := m.Alignof(f.Type)
		if !ok1 || !ok2 {
			return 0, 0, false
		}
		// __attribute__((packed)) flattens every member's alignment to one;
		// #pragma pack caps it. Record.MemberAlign is the rule, stated once
		// so that this layout and lower's cannot drift.
		natAl := fa
		fa = r.MemberAlign(fa)
		// An unnamed bit-field carries storage but no alignment: it cannot
		// raise the record's. lower/layout.go states the same rule and the
		// two must agree, or layout warns about itself.
		if fa > align && (!f.BitField || f.Name != "") {
			align = fa
		}
		if r.Union {
			// Every member of a union starts at its base.
			if offs != nil {
				offs[i] = 0
			}
			// A bit-field member is as wide as its bits, not as wide as its
			// declared type.
			w := fs * 8
			if f.BitField {
				w = roundUp(f.Width, 8)
			}
			if w > cur.Bits {
				cur.Bits = w
			}
			continue
		}
		if f.BitField {
			switch {
			case f.Width == 0:
				cur.ZeroWidth(m.MSBitfields, natAl)
			case r.Packed:
				// __attribute__((packed)) admits no padding at all, so the
				// field goes at the next bit whatever its declared type is.
				// It is the one case neither allocation rule describes.
				cur.CloseUnit()
				cur.Bits += f.Width
			default:
				cur.PlaceBitfield(m.MSBitfields, fs, fa, f.Width)
			}
			continue
		}
		off := cur.CloseForMember()
		off = roundUp(off, fa)
		if offs != nil {
			offs[i] = off
		}
		cur.Bits = (off + fs) * 8
	}
	bits := cur.End()
	// __attribute__((aligned(n))) raises the record's alignment, and with it
	// the size, whether or not the record is packed: the two attributes
	// answer different questions.
	if r.Align > align {
		align = r.Align
	}
	size = roundUp(bits, align*8) / 8
	if size == 0 {
		size = 0 // an empty struct is a constraint violation reported elsewhere
	}
	return size, align, true
}

func roundUp(n, to int64) int64 {
	if to == 0 {
		return n
	}
	return (n + to - 1) / to * to
}

// IntBits returns the width in bits of an integer type, and whether
// values of it are signed — plain char resolved by the model.
func (m Model) IntBits(t Type) (bits int64, signed bool) {
	u := Unqualify(t)
	k := u.Kind()
	if e, ok := u.(*Enum); ok {
		k = e.Underlying()
	}
	sz, _ := m.basicSize(k)
	signed = IsSigned(Unqualify(t))
	if k == Char {
		signed = m.CharSigned
	}
	return sz * 8, signed
}

// IntMax returns the largest value an integer type can hold, as a
// uint64.
func (m Model) IntMax(t Type) uint64 {
	bits, signed := m.IntBits(t)
	if signed {
		return 1<<(bits-1) - 1
	}
	if bits >= 64 {
		return ^uint64(0)
	}
	return 1<<bits - 1
}
