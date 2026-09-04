package types

// Where a bit-field goes, which is two rules and not one.
//
// §6.7.2.1p11 leaves the allocation of a bit-field into storage
// implementation-defined, and the two answers in circulation disagree about
// ordinary structs rather than about corners:
//
//	struct { char a; int b : 3; };
//
// is four bytes under gcc and clang, with b in the byte after a, and eight
// under MSVC, with b at offset four. Neither is more correct; what matters
// is agreeing with everything else on the platform, because the layout is
// what a struct means to the kernel, to a library, and to the next object
// file. So the rule is the target's — Model.MSBitfields — and this file is
// both of them, in one place, because the size types.Model reports and the
// offsets lower emits are computed separately and have to come out the same.

// BitCursor is the state a struct's layout carries while it places members:
// where the next bit goes, and what allocation unit is open.
//
// Under the gcc rule no unit is ever "open" — a bit-field is placed at the
// cursor and moved to the next boundary of its declared type only if it
// would straddle one — so only the MS rule uses the last three fields.
type BitCursor struct {
	Bits int64 // the cursor, in bits from the record's base

	// The open allocation unit, under the MS rule: where it starts, how
	// wide it is, and the declared size in bytes of the type that opened
	// it. A size of zero means no unit is open.
	unitStart int64
	unitBits  int64
	unitSize  int64
}

// BitPlace is where one bit-field landed: the unit's byte offset, the bit
// within it, and the unit's width in bytes.
type BitPlace struct {
	Off    int64 // the unit's byte offset from the record's base
	BitOff int64 // the field's first bit within the unit
	Unit   int64 // the unit's width in bytes
}

// CloseUnit ends the open unit, moving the cursor past all of it.
//
// A unit is closed by anything that is not a bit-field continuing it: an
// ordinary member, a bit-field of another size, one that does not fit, and
// the end of the record. The bits left in it are padding, which is the
// whole difference from the gcc rule.
func (c *BitCursor) CloseUnit() {
	if c.unitSize != 0 {
		if end := c.unitStart + c.unitBits; end > c.Bits {
			c.Bits = end
		}
		c.unitSize = 0
	}
}

// PlaceBitfield places a bit-field of width w, whose declared type is size
// bytes wide and is placed at align-byte alignment, and returns where it
// went. ms selects the rule.
func (c *BitCursor) PlaceBitfield(ms bool, size, align, w int64) BitPlace {
	unit := size * 8
	if unit <= 0 {
		unit, size = 8, 1
	}
	if !ms {
		// gcc's: at the cursor, unless the field would cross a boundary of
		// its own declared type, in which case at the next one.
		start := c.Bits
		if start/unit != (start+w-1)/unit {
			start = roundUp(start, unit)
		}
		uoff := (start / unit) * size
		c.Bits = start + w
		return BitPlace{Off: uoff, BitOff: start - uoff*8, Unit: size}
	}

	// MSVC's: continue the open unit when it was opened by the same
	// declared size and has room, and otherwise open a new one at the next
	// offset the type's alignment admits.
	if c.unitSize != size || c.Bits+w > c.unitStart+c.unitBits {
		c.CloseUnit()
		c.unitStart = roundUp(c.Bits, align*8)
		c.unitBits = unit
		c.unitSize = size
		c.Bits = c.unitStart
	}
	off := c.unitStart / 8
	place := BitPlace{Off: off, BitOff: c.Bits - c.unitStart, Unit: size}
	c.Bits += w
	return place
}

// CloseForMember ends any open unit before an ordinary member is placed,
// and returns the byte offset the cursor has reached.
func (c *BitCursor) CloseForMember() int64 {
	c.CloseUnit()
	return (c.Bits + 7) / 8
}

// ZeroWidth is `int : 0`, which places no member and only says that the
// next bit-field starts a fresh unit (§6.7.2.1p12).
//
// Under the gcc rule that means rounding the cursor to a boundary of the
// declared type, by its natural alignment rather than a packed one: the
// field exists to round, and rounding it by a byte would leave it meaning
// nothing. Under MSVC's, closing the open unit is already that.
func (c *BitCursor) ZeroWidth(ms bool, natAlign int64) {
	if ms {
		c.CloseUnit()
		return
	}
	c.Bits = roundUp(c.Bits, natAlign*8)
}

// end is the record's size in bits, once every member is placed.
func (c *BitCursor) End() int64 {
	c.CloseUnit()
	return c.Bits
}
