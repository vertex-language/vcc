package lower

import "github.com/vertex-language/vcc/types"

// place is where one member of a record lives.
//
// A plain member is Off bytes from the record's base. A bit-field member is
// Width bits starting BitOff bits above the base of a Unit-byte storage unit
// that itself starts at Off. Skip marks the unnamed zero-width bit-field,
// which occupies no storage and only closes the unit before it.
type place struct {
	Off    int64
	Bit    bool
	BitOff int64
	Width  int64
	Unit   int64
	Skip   bool
}

// recLayout is a record's storage description: one place per declared member,
// in declaration order, plus the size and alignment sizeof and _Alignof yield.
//
// This duplicates arithmetic types.Model already does for Sizeof, and should
// not: see the note in the package README. Until types exposes offsets, the
// two must be kept in step, and lower asserts agreement on Size below.
type recLayout struct {
	Size, Align int64
	Places      []place
	Flexible    bool // last member is an incomplete array
}

// layout computes, memoizes, and returns a record's storage description.
//
// The result is inserted before the members are walked, so a record reachable
// from its own members through a pointer terminates rather than recurring.
func (tm *typeMap) layout(r *types.Record) *recLayout {
	if l, ok := tm.lay[r]; ok {
		return l
	}
	l := &recLayout{Align: 1, Places: make([]place, len(r.Fields))}
	tm.lay[r] = l
	if r.Union {
		tm.layoutUnion(r, l)
	} else {
		tm.layoutStruct(r, l)
	}
	if want, ok := tm.u.model.Sizeof(r); ok && want != l.Size {
		tm.u.warnf(tm.u.file, "internal: layout of %s is %d bytes, types says %d", r, l.Size, want)
	}
	return l
}

// layoutStruct implements the allocation vcc defines for §6.7.2.1p11: members
// in declaration order, each at the next offset its own alignment admits, and
// bit-fields packed low-order bits first into storage units of their declared
// type, a new unit begun whenever the field would cross a unit boundary.
//
// The standard leaves both the direction and the straddling rule
// implementation-defined. vcc defines them once, here, rather than per target:
// a bit-field layout that varied by host would make a struct's ABI a property
// of who compiled it.
func (tm *typeMap) layoutStruct(r *types.Record, l *recLayout) {
	var cur types.BitCursor // the allocation cursor; types/bitfield.go is the rule
	ms := tm.u.model.MSBitfields

	if r.Align > l.Align {
		l.Align = r.Align
	}
	for i, f := range r.Fields {
		ft := types.Unqualify(f.Type)
		sz, known := tm.u.model.Sizeof(ft)
		al, ok := tm.u.model.Alignof(ft)
		if !ok || al < 1 {
			al = 1
		}
		// __attribute__((packed)) places every member at the next byte and
		// lets none contribute alignment of its own; #pragma pack caps what
		// each may contribute. types.Record.MemberAlign is the rule, and
		// types.Model reads the same one for sizeof.
		natAl := al
		al = r.MemberAlign(al)
		if !known {
			// The only incomplete member C admits is a trailing flexible
			// array, which contributes nothing to the size.
			sz = 0
			if i == len(r.Fields)-1 {
				l.Flexible = true
			}
		}
		// An unnamed bit-field contributes storage and nothing else. It
		// cannot raise the record's alignment: `struct { char c; int :1; }`
		// is three bytes aligned to one, not eight aligned to four, and a
		// zero-width field — which is always unnamed — only closes the unit
		// before it. A named bit-field does contribute, through its declared
		// type, exactly as a plain member does.
		if al > l.Align && (!f.BitField || f.Name != "") {
			l.Align = al
		}

		if !f.BitField {
			off := roundUp(cur.CloseForMember(), al)
			l.Places[i] = place{Off: off}
			cur.Bits = (off + sz) * 8
			continue
		}

		if f.Width == 0 {
			// §6.7.2.1p12: no member, and the next one starts a fresh unit.
			// packed does not change this: the zero-width field exists only
			// to round the cursor, and rounding it by one byte would make
			// it mean nothing.
			cur.ZeroWidth(ms, natAl)
			l.Places[i] = place{Skip: true}
			continue
		}

		unit := sz * 8
		if unit <= 0 {
			unit = 8
			sz = 1
		}
		start := cur.Bits
		if r.Packed {
			// A packed bit-field is placed at the next bit, and its storage
			// unit is the smallest power-of-two byte span that covers it
			// from the byte it starts in.
			uoff := start / 8
			span := (start+f.Width+7)/8 - uoff
			sz = 1
			for sz < span {
				sz *= 2
			}
			if sz > 8 {
				// The field starts mid-byte and is wide enough that no
				// eight-byte window covers it. Reaching it takes two loads
				// and a join, which this does not do.
				tm.u.errorf(tm.u.file,
					"%s: packed bit-field %s is %d bits starting %d bits into a byte, "+
						"which spans %d bytes; vcc reads a bit-field in one load of at most eight",
					r, f.Name, f.Width, start%8, span)
				sz = 8
			}
			l.Places[i] = place{
				Off:    uoff,
				Bit:    true,
				BitOff: start - uoff*8,
				Width:  f.Width,
				Unit:   sz,
			}
			cur.CloseUnit()
			cur.Bits = start + f.Width
			continue
		}
		p := cur.PlaceBitfield(ms, sz, al, f.Width)
		l.Places[i] = place{
			Off:    p.Off,
			Bit:    true,
			BitOff: p.BitOff,
			Width:  f.Width,
			Unit:   p.Unit,
		}
	}

	l.Size = roundUp((cur.End()+7)/8, l.Align)
}

// layoutUnion places every member at zero. The size is the widest member,
// rounded to the strictest alignment.
func (tm *typeMap) layoutUnion(r *types.Record, l *recLayout) {
	if r.Align > l.Align {
		l.Align = r.Align
	}
	for i, f := range r.Fields {
		ft := types.Unqualify(f.Type)
		sz, _ := tm.u.model.Sizeof(ft)
		al, ok := tm.u.model.Alignof(ft)
		if !ok || al < 1 {
			al = 1
		}
		al = r.MemberAlign(al)
		// As in a struct: an unnamed bit-field does not raise the alignment.
		if al > l.Align && (!f.BitField || f.Name != "") {
			l.Align = al
		}
		if f.BitField {
			if f.Width == 0 {
				l.Places[i] = place{Skip: true}
				continue
			}
			if sz < 1 {
				sz = 1
			}
			l.Places[i] = place{Bit: true, BitOff: 0, Width: f.Width, Unit: sz}
			// A bit-field member is as wide as its bits, not as wide as the
			// type it was declared with: `union { long long :40; char c; }`
			// is five bytes.
			if b := (f.Width + 7) / 8; b > l.Size {
				l.Size = b
			}
			continue
		}
		l.Places[i] = place{Off: 0}
		if sz > l.Size {
			l.Size = sz
		}
	}
	l.Size = roundUp(l.Size, l.Align)
}

// memberStep is one hop of a member path: the record being indexed and the
// index of the member within it.
type memberStep struct {
	rec   *types.Record
	index int
}

// member resolves a member name against a record, descending into anonymous
// members (§6.7.2.1p13). It returns the path taken, or nil.
//
// Direct members win over anonymous ones at every level, and the search is
// breadth-first across a level for the same reason: an ambiguity between two
// anonymous members is a constraint violation the analyzer already reported,
// and lower must not resolve it differently than the analyzer did.
func (tm *typeMap) member(r *types.Record, name string) []memberStep {
	for i, f := range r.Fields {
		if f.Name == name {
			return []memberStep{{rec: r, index: i}}
		}
	}
	for i, f := range r.Fields {
		if f.Name != "" {
			continue
		}
		inner, ok := types.Unqualify(f.Type).(*types.Record)
		if !ok {
			continue
		}
		if rest := tm.member(inner, name); rest != nil {
			return append([]memberStep{{rec: r, index: i}}, rest...)
		}
	}
	return nil
}

// offsetOf sums a member path's byte offsets. The final step's place is
// returned separately, since a bit-field member is not addressable and the
// caller needs its unit, not its offset alone.
func (tm *typeMap) offsetOf(path []memberStep) (off int64, last place) {
	for _, s := range path {
		p := tm.layout(s.rec).Places[s.index]
		off += p.Off
		last = p
	}
	return off, last
}

func roundUp(n, a int64) int64 {
	if a <= 1 {
		return n
	}
	return (n + a - 1) / a * a
}
