// Package types represents C types and constructs them from
// declaration specifiers and declarators.
//
// Types are trees; struct, union, and enum types additionally have
// identity — two *Record values are the same type iff they are the
// same pointer, which is what makes a tag namespace meaningful.
// Constraint checking that the parser deliberately deferred
// (specifier multisets, qualifier placement, static and * in array
// declarators, function/array derivation rules) happens here, during
// construction, reported through the Resolver.
package types

import (
	"fmt"
	"strings"
)

// Kind identifies a type's shape.
type Kind uint8

const (
	Invalid Kind = iota

	Void
	Bool
	Char // plain char: distinct from SChar and UChar (§6.2.5p15)
	SChar
	UChar
	Short
	UShort
	Int
	UInt
	Long
	ULong
	LongLong
	ULongLong

	// Int128 and UInt128 are gcc's __int128. vcc knows their width and
	// alignment, which is what a struct containing one needs — Darwin's
	// arm/_mcontext.h has them, reached from <stdio.h> — and refuses
	// arithmetic on them, because the IR has no 128-bit register and
	// software arithmetic for one is not written. Knowing the size is the
	// half that matters: a member of the wrong width moves every member
	// after it.
	Int128
	UInt128

	Float
	Double
	LongDouble
	ComplexFloat
	ComplexDouble
	ComplexLongDouble

	PointerKind
	ArrayKind
	FuncKind
	StructKind
	UnionKind
	EnumKind
)

// Type is the interface all C types implement.
type Type interface {
	Kind() Kind
	String() string
}

// Basic is a builtin arithmetic type or void. Use Typ for the
// canonical singleton of each kind.
type Basic struct{ K Kind }

var basics [ComplexLongDouble + 1]Basic

func init() {
	for k := range basics {
		basics[k].K = Kind(k)
	}
}

// Typ returns the canonical *Basic for a basic kind.
func Typ(k Kind) *Basic { return &basics[k] }

func (b *Basic) Kind() Kind { return b.K }

// IsComplex reports whether t is one of §6.2.5's complex types.
func IsComplex(t Type) bool {
	switch Unqualify(t).Kind() {
	case ComplexFloat, ComplexDouble, ComplexLongDouble:
		return true
	}
	return false
}

// Qual is a qualifier set.
type Qual uint8

const (
	QConst Qual = 1 << iota
	QVolatile
	QRestrict
	QAtomic
)

// Qualified wraps a type with qualifiers. Qualify never nests them.
type Qualified struct {
	Q Qual
	T Type
}

func (q *Qualified) Kind() Kind { return q.T.Kind() }

// Qualify applies qualifiers, merging with any already present.
// Qualifying with nothing is the identity.
func Qualify(t Type, q Qual) Type {
	if q == 0 {
		return t
	}
	if in, ok := t.(*Qualified); ok {
		return &Qualified{Q: in.Q | q, T: in.T}
	}
	return &Qualified{Q: q, T: t}
}

// Unqualify strips the qualifier wrapper, if any.
func Unqualify(t Type) Type {
	if q, ok := t.(*Qualified); ok {
		return q.T
	}
	return t
}

// QualsOf returns a type's qualifiers.
func QualsOf(t Type) Qual {
	if q, ok := t.(*Qualified); ok {
		return q.Q
	}
	return 0
}

// Pointer is pointer-to-Elem.
type Pointer struct{ Elem Type }

func (*Pointer) Kind() Kind { return PointerKind }

// ArrayForm distinguishes §6.7.6.2's four bracket shapes.
type ArrayForm uint8

const (
	FixedArray      ArrayForm = iota // [N], N a constant expression
	IncompleteArray                  // []
	VLA                              // [expr], expr not constant
	StarArray                        // [*]
)

// Array is array-of-Elem. Len is meaningful only for FixedArray.
// Static records a parameter's [static …].
type Array struct {
	Elem   Type
	Form   ArrayForm
	Len    int64
	Static bool
}

func (*Array) Kind() Kind { return ArrayKind }

// Param is one function parameter.
type Param struct {
	Name string // "" for unnamed
	Type Type
}

// Func is a function type. Proto is false for f() and K&R
// identifier-list declarators, where the parameters are unspecified.
type Func struct {
	Ret      Type
	Params   []Param
	Variadic bool
	Proto    bool
}

func (*Func) Kind() Kind { return FuncKind }

// Field is one struct/union member.
type Field struct {
	Name     string // "" for an unnamed bit-field or anonymous record
	Type     Type
	BitField bool
	Width    int64 // meaningful when BitField
}

// Record is a struct or union type. Identity is the tag: two Records
// are the same type iff they are the same pointer. Complete flips to
// true when a definition supplies the member list.
type Record struct {
	Union    bool
	Name     string // "" for anonymous
	Fields   []Field
	Complete bool

	// Packed drops the padding between members: every one is placed at the
	// next byte rather than at the next offset its own alignment admits, and
	// the record itself aligns to one. It is __attribute__((packed)), which
	// is not in the standard and is in every protocol header written for
	// gcc — a wire format is a struct whose layout the protocol chose.
	Packed bool

	// Align, when non-zero, is the alignment __attribute__((aligned(n)))
	// asked for, in bytes. It raises the record's alignment and therefore
	// its size, and it applies whether or not the record is packed: the two
	// attributes answer different questions, one about the members and one
	// about the whole.
	Align int64

	// Pack, when non-zero, is the ceiling #pragma pack put on each member's
	// alignment, in bytes. A member whose type wants less keeps what it
	// wants; one that wants more is placed at Pack instead, and the record
	// aligns no more strictly than its members now do.
	//
	// It is not Packed, which is a floor of one and admits no padding at
	// all: `#pragma pack(2)` still aligns an int to two. Packed wins where
	// both are set, being the stricter of the two.
	//
	// The Windows SDK is written in it. wingdi.h puts BITMAPFILEHEADER
	// inside pshpack2.h, which makes it fourteen bytes rather than sixteen,
	// and a compiler that ignores the pragma writes .bmp files no other
	// program can read — silently, since nothing about the code says so.
	Pack int64

	// Vector marks a record that names a vector register rather than a
	// place in memory: __m128i, and its siblings when they arrive. It is
	// MSVC's __declspec(intrin_type), which is what <emmintrin.h> writes on
	// each of them, and it is a claim about the compiler and not about the
	// layout — the members are still there, still at their offsets, and
	// still readable, which is why this is a flag on a record and not a
	// separate kind. What it changes is that a value of the type is loaded
	// into a register rather than carried by address, so the intrinsics can
	// be instructions.
	Vector bool
}

// MemberAlign is the alignment a member of natural alignment n is actually
// placed at in this record: capped by #pragma pack, and flattened to one by
// __attribute__((packed)).
//
// It is a method rather than a rule spelled at each layout, because there
// are two layouts — types.Model's, which answers sizeof, and lower's, which
// places the members — and they have to agree.
func (r *Record) MemberAlign(n int64) int64 {
	switch {
	case r.Packed:
		return 1
	case r.Pack > 0 && n > r.Pack:
		return r.Pack
	}
	return n
}

func (r *Record) Kind() Kind {
	if r.Union {
		return UnionKind
	}
	return StructKind
}

// Enum is an enumerated type; like Record, it has identity.
//
// Under is the integer type the enumeration is compatible with. C17
// §6.7.2.2p4 leaves the choice to the implementation but §6.7.2.2p2
// constrains the enumerators to int, and this one answers int wherever
// that holds — which is every enumeration a strictly conforming program
// can write, so Under is Invalid there and Underlying says int.
//
// It widens only for the enumerators C17 does not allow, which every
// compiler on this list accepts and which the Windows SDK's wingdi.h
// writes thirty-four of (DISPLAYCONFIG_OUTPUT_TECHNOLOGY_INTERNAL is
// 0x80000000). The analyzer picks the type; see enumType.
type Enum struct {
	Name     string
	Complete bool
	Under    Kind
}

func (*Enum) Kind() Kind { return EnumKind }

// Underlying is the integer kind the enumeration is compatible with: int
// unless an enumerator did not fit in one.
func (e *Enum) Underlying() Kind {
	if e.Under == Invalid {
		return Int
	}
	return e.Under
}

// ConstType is the type an enumeration constant of e has.
//
// §6.4.4.3p2 says int, and this answers int wherever that is a type the
// value fits — which keeps _Generic, a variadic argument and a
// __builtin_types_compatible_p over an enumerator saying what the standard
// says they say. Where the enumeration widened there is no such option: an
// enumerator that does not fit in an int cannot have the type int, so it
// has the enumeration's, which is what gcc and clang give it.
func (e *Enum) ConstType() Type {
	if e.Under == Invalid {
		return Typ(Int)
	}
	return e
}

// IsInteger reports whether t (unqualified) is an integer type;
// enums count (§6.2.5p17).
func IsInteger(t Type) bool {
	switch Unqualify(t).Kind() {
	case Bool, Char, SChar, UChar, Short, UShort, Int, UInt,
		Long, ULong, LongLong, ULongLong, Int128, UInt128, EnumKind:
		return true
	}
	return false
}

// IsSigned reports whether an integer type is signed. Plain char's
// signedness is the Model's business, not the type's; it reports
// false here and callers that care ask the Model.
func IsSigned(t Type) bool {
	u := Unqualify(t)
	if e, ok := u.(*Enum); ok {
		// An enumeration is as signed as the type it is compatible with,
		// which is int until an enumerator too large for one widened it.
		return IsSigned(Typ(e.Underlying()))
	}
	switch u.Kind() {
	case SChar, Short, Int, Long, LongLong, Int128:
		return true
	}
	return false
}

// ---- printing, C-ish and compact, for diagnostics ----

func (b *Basic) String() string {
	switch b.K {
	case Void:
		return "void"
	case Bool:
		return "_Bool"
	case Char:
		return "char"
	case SChar:
		return "signed char"
	case UChar:
		return "unsigned char"
	case Short:
		return "short"
	case UShort:
		return "unsigned short"
	case Int:
		return "int"
	case UInt:
		return "unsigned int"
	case Long:
		return "long"
	case ULong:
		return "unsigned long"
	case LongLong:
		return "long long"
	case ULongLong:
		return "unsigned long long"
	case Float:
		return "float"
	case Double:
		return "double"
	case LongDouble:
		return "long double"
	case Int128:
		return "__int128"
	case UInt128:
		return "unsigned __int128"
	case ComplexFloat:
		return "float _Complex"
	case ComplexDouble:
		return "double _Complex"
	case ComplexLongDouble:
		return "long double _Complex"
	}
	return "invalid"
}

func (q *Qualified) String() string {
	var b strings.Builder
	for _, e := range [...]struct {
		q Qual
		s string
	}{{QConst, "const "}, {QVolatile, "volatile "}, {QRestrict, "restrict "}, {QAtomic, "_Atomic "}} {
		if q.Q&e.q != 0 {
			b.WriteString(e.s)
		}
	}
	b.WriteString(q.T.String())
	return b.String()
}

func (p *Pointer) String() string { return p.Elem.String() + "*" }

func (a *Array) String() string {
	switch a.Form {
	case FixedArray:
		return fmt.Sprintf("%s[%d]", a.Elem, a.Len)
	case VLA:
		return a.Elem.String() + "[<vla>]"
	case StarArray:
		return a.Elem.String() + "[*]"
	}
	return a.Elem.String() + "[]"
}

func (f *Func) String() string {
	var b strings.Builder
	b.WriteString(f.Ret.String())
	b.WriteByte('(')
	if !f.Proto {
		b.WriteByte(')')
		return b.String()
	}
	if len(f.Params) == 0 {
		b.WriteString("void")
	}
	for i, p := range f.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.Type.String())
	}
	if f.Variadic {
		b.WriteString(", ...")
	}
	b.WriteByte(')')
	return b.String()
}

func (r *Record) String() string {
	kw := "struct"
	if r.Union {
		kw = "union"
	}
	if r.Name == "" {
		return kw + " <anonymous>"
	}
	return kw + " " + r.Name
}

func (e *Enum) String() string {
	if e.Name == "" {
		return "enum <anonymous>"
	}
	return "enum " + e.Name
}

// IsVector reports whether t is a vector type: __m128i today, and whatever
// joins it. A vector is not arithmetic — C's operators do not apply to one,
// and the operations on it are the intrinsics — and it is not a record, so
// the rules that name those two categories do not reach it and the ones that
// concern it say so.
func IsVector(t Type) bool {
	r, ok := Unqualify(t).(*Record)
	return ok && r.Vector
}
