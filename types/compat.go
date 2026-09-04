package types

// Type classification, the conversions §6.3 defines over it, and §6.2.7's
// compatibility.
//
// These live here rather than in one of the packages that needs them because
// both do: the analyzer decides whether an assignment is a constraint
// violation, and lower decides what conversion to emit, and the two must agree
// about what the types are. Where they disagreed, the analyzer would accept a
// program lower could not emit, or reject one it could.

// IsFloat reports whether t is one of §6.2.5's real floating types. The
// complex types are not included: vcc does not implement them, and a
// predicate that admitted them would let a later phase believe it could.
func IsFloat(t Type) bool {
	switch Unqualify(t).Kind() {
	case Float, Double, LongDouble:
		return true
	}
	return false
}

// IsArithmetic is §6.2.5p18: an integer or floating type.
func IsArithmetic(t Type) bool { return IsInteger(t) || IsFloat(t) }

// IsScalar is §6.2.5p21: an arithmetic type or a pointer.
func IsScalar(t Type) bool { return IsArithmetic(t) || IsPointer(t) }

// IsPointer reports whether t is a pointer type. An array is not one until it
// has decayed; see Decay.
func IsPointer(t Type) bool { return Unqualify(t).Kind() == PointerKind }

// IsVoid reports whether t is void.
func IsVoid(t Type) bool { return Unqualify(t).Kind() == Void }

// IsBool reports whether t is _Bool.
func IsBool(t Type) bool { return Unqualify(t).Kind() == Bool }

// IsRecord reports whether t is a struct or a union.
func IsRecord(t Type) bool {
	k := Unqualify(t).Kind()
	return k == StructKind || k == UnionKind
}

// IsFunc reports whether t is a function type.
func IsFunc(t Type) bool { return Unqualify(t).Kind() == FuncKind }

// IsArray reports whether t is an array type.
func IsArray(t Type) bool { return Unqualify(t).Kind() == ArrayKind }

// AsPointer returns t's pointer type, or nil.
func AsPointer(t Type) *Pointer {
	p, _ := Unqualify(t).(*Pointer)
	return p
}

// AsArray returns t's array type, or nil.
func AsArray(t Type) *Array {
	a, _ := Unqualify(t).(*Array)
	return a
}

// AsFunc returns t's function type, or nil.
func AsFunc(t Type) *Func {
	f, _ := Unqualify(t).(*Func)
	return f
}

// AsRecord returns t's struct or union type, or nil.
func AsRecord(t Type) *Record {
	r, _ := Unqualify(t).(*Record)
	return r
}

// Decay applies §6.3.2.1p3-4: an array becomes a pointer to its first
// element, a function becomes a pointer to itself. Everything else is
// unchanged.
//
// The element's qualifiers travel with it — `const char[8]` decays to
// `const char *`, which is what makes assigning it to `char *` a violation.
func Decay(t Type) Type {
	switch u := Unqualify(t).(type) {
	case *Array:
		return &Pointer{Elem: u.Elem}
	case *Func:
		return &Pointer{Elem: u}
	}
	return t
}

// Promote applies §6.3.1.1p2's integer promotions: a type of integer rank
// below int becomes int, or unsigned int where int cannot represent every
// value of the original. Everything else is unchanged.
func (m Model) Promote(t Type) Type {
	t = Unqualify(t)
	if !IsInteger(t) {
		return t
	}
	if e, isEnum := t.(*Enum); isEnum {
		// An enumerated type promotes to the type it is compatible with:
		// int, unless an enumerator too large for one widened it.
		return Typ(e.Underlying())
	}
	intBits, _ := m.IntBits(Typ(Int))
	bits, signed := m.IntBits(t)
	if bits > intBits || (bits == intBits && !signed && !isSmallInt(t)) {
		return t
	}
	if bits < intBits || signed {
		return Typ(Int)
	}
	return Typ(UInt)
}

func isSmallInt(t Type) bool {
	b, ok := Unqualify(t).(*Basic)
	if !ok {
		return false
	}
	switch b.K {
	case Bool, Char, SChar, UChar, Short, UShort:
		return true
	}
	return false
}

// Usual applies §6.3.1.8's usual arithmetic conversions and returns the common
// type both operands convert to.
func (m Model) Usual(a, b Type) Type {
	a, b = Unqualify(a), Unqualify(b)
	if IsFloat(a) || IsFloat(b) {
		return widerFloat(a, b)
	}
	a, b = m.Promote(a), m.Promote(b)
	if a == b {
		return a
	}
	ab, as := m.IntBits(a)
	bb, bs := m.IntBits(b)
	switch {
	case as == bs:
		if ab >= bb {
			return a
		}
		return b
	case !as && ab >= bb:
		return a
	case !bs && bb >= ab:
		return b
	case as && ab > bb:
		return a
	case bs && bb > ab:
		return b
	}
	// Equal width, mixed signedness: the unsigned counterpart of the signed
	// type is the common one.
	if as {
		return UnsignedOf(a)
	}
	return UnsignedOf(b)
}

func widerFloat(a, b Type) Type {
	rank := func(t Type) int {
		bt, ok := Unqualify(t).(*Basic)
		if !ok {
			return 0
		}
		switch bt.K {
		case LongDouble:
			return 3
		case Double:
			return 2
		case Float:
			return 1
		}
		return 0
	}
	if rank(a) >= rank(b) {
		return a
	}
	return b
}

// UnsignedOf returns the unsigned type of the same rank.
func UnsignedOf(t Type) Type {
	b, ok := Unqualify(t).(*Basic)
	if !ok {
		return t
	}
	switch b.K {
	case SChar, Char:
		return Typ(UChar)
	case Short:
		return Typ(UShort)
	case Int, EnumKind:
		return Typ(UInt)
	case Long:
		return Typ(ULong)
	case LongLong:
		return Typ(ULongLong)
	}
	return t
}

// SizeType is size_t: the unsigned integer of pointer width. There is no Kind
// for it, so it is named by width rather than by spelling — which is also how
// <stddef.h>'s typedef is generated, so the two agree.
func (m Model) SizeType() Type {
	switch m.SizePtr {
	case m.SizeLong:
		return Typ(ULong)
	case m.SizeInt:
		return Typ(UInt)
	default:
		return Typ(ULongLong)
	}
}

// PtrDiffType is ptrdiff_t: the signed integer of pointer width.
func (m Model) PtrDiffType() Type {
	switch m.SizePtr {
	case m.SizeLong:
		return Typ(Long)
	case m.SizeInt:
		return Typ(Int)
	default:
		return Typ(LongLong)
	}
}

// Compatible is §6.2.7p1, within one translation unit.
//
// Identity is what a tag gets: two Records or Enums are the same type exactly
// when they are the same pointer, which is what the analyzer's tag scope
// already guarantees. Everything else is structural.
//
// The one place this is deliberately loose is the unprototyped function.
// §6.7.6.3p15's rules for combining `int f()` with a definition are more than
// a compatibility test can express, and refusing to match it would reject a
// declaration style the standard still admits — so an unprototyped function
// type is compatible with any function type of a compatible return type.
func Compatible(a, b Type) bool {
	if a == nil || b == nil {
		return false
	}
	if QualsOf(a) != QualsOf(b) {
		return false
	}
	return compatUnqual(Unqualify(a), Unqualify(b))
}

// CompatibleIgnoringQuals is Compatible with the outermost qualifiers
// dropped, which is what an assignment's pointee test asks for: §6.5.16.1
// wants the pointed-to types compatible, and checks the qualifiers
// separately because the direction matters.
func CompatibleIgnoringQuals(a, b Type) bool {
	if a == nil || b == nil {
		return false
	}
	return compatUnqual(Unqualify(a), Unqualify(b))
}

func compatUnqual(a, b Type) bool {
	if a == b {
		return true
	}
	// An enumerated type is compatible with its implementation type
	// (§6.7.2.2p4), which this implementation chooses as int unless an
	// enumerator did not fit in one.
	if e, ok := a.(*Enum); ok {
		return b.Kind() == e.Underlying() || b.Kind() == EnumKind && a == b
	}
	if e, ok := b.(*Enum); ok {
		return a.Kind() == e.Underlying()
	}
	if a.Kind() != b.Kind() {
		return false
	}
	switch x := a.(type) {
	case *Basic:
		return x.K == b.(*Basic).K
	case *Pointer:
		return Compatible(x.Elem, b.(*Pointer).Elem)
	case *Array:
		y := b.(*Array)
		if !Compatible(x.Elem, y.Elem) {
			return false
		}
		// A size is compared only where both are known; §6.2.7p1 leaves an
		// incomplete array compatible with a completed one.
		if x.Form == FixedArray && y.Form == FixedArray {
			return x.Len == y.Len
		}
		return true
	case *Func:
		y := b.(*Func)
		if !Compatible(x.Ret, y.Ret) {
			return false
		}
		if !x.Proto || !y.Proto {
			return true // see the note on Compatible
		}
		if x.Variadic != y.Variadic || len(x.Params) != len(y.Params) {
			return false
		}
		for i := range x.Params {
			if !Compatible(AdjustParam(x.Params[i].Type), AdjustParam(y.Params[i].Type)) {
				return false
			}
		}
		return true
	}
	// Records reach here only when they are different pointers, which under
	// tag identity means different types.
	return false
}

// AssignKind classifies the result of an assignment-compatibility test.
type AssignKind uint8

const (
	// AssignOK: §6.5.16.1's constraints are met.
	AssignOK AssignKind = iota
	// AssignBad: the two types have no assignment relation at all.
	AssignBad
	// AssignPointerMismatch: both are pointers, to types that are not
	// compatible. gcc and clang make this a warning by default; §6.5.16.1
	// makes it a constraint violation.
	AssignPointerMismatch
	// AssignDiscardsQuals: the pointee types are compatible, but the target
	// drops a qualifier the source has.
	AssignDiscardsQuals
	// AssignIntPointer: one side is a pointer and the other an integer that
	// is not a null pointer constant.
	AssignIntPointer
)

// Assignable is §6.5.16.1's constraint list, plus the same rules §6.5.2.2
// applies to an argument and §6.8.6.4 to a return value — the standard defines
// all three by reference to simple assignment.
//
// nullConst says the right operand is a null pointer constant, which the
// caller decides: it is a property of the expression, not of its type.
func Assignable(dst, src Type, nullConst bool) AssignKind {
	l, r := Unqualify(dst), Unqualify(Decay(src))

	switch {
	case IsArithmetic(l) && IsArithmetic(r):
		return AssignOK

	// A vector assigns to and from itself and to nothing else. It is not
	// arithmetic — C's operators do not apply to one — and it is not a
	// record, so neither arm above reaches it, and the sixteen bytes have to
	// be able to move between two objects of the type or the type is useless.
	case IsVector(l) || IsVector(r):
		if IsVector(l) && IsVector(r) && l.Kind() == r.Kind() {
			return AssignOK
		}
		return AssignBad

	case IsRecord(l):
		if CompatibleIgnoringQuals(l, r) {
			return AssignOK
		}
		return AssignBad

	case IsBool(l) && IsPointer(r):
		// §6.5.16.1p1's last bullet: a pointer assigns to _Bool.
		return AssignOK

	case IsPointer(l) && nullConst:
		return AssignOK

	case IsPointer(l) && IsPointer(r):
		lp, rp := AsPointer(l), AsPointer(r)
		le, re := lp.Elem, rp.Elem
		// A pointer to void converts to and from a pointer to any object
		// type, but not to a pointer to a function.
		voidEither := (IsVoid(le) && !IsFunc(re)) || (IsVoid(re) && !IsFunc(le))
		if !voidEither && !CompatibleIgnoringQuals(le, re) {
			return AssignPointerMismatch
		}
		// The target must carry every qualifier the source's pointee has.
		if QualsOf(re)&^QualsOf(le) != 0 {
			return AssignDiscardsQuals
		}
		return AssignOK

	case IsPointer(l) && IsInteger(r), IsInteger(l) && IsPointer(r):
		return AssignIntPointer
	}
	return AssignBad
}
