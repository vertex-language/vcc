// Package token defines the lexical vocabulary of C11 (ISO/IEC
// 9899:2011; C17 is lexically identical) and the per-translation-unit
// position space every span in the front end resolves through.
//
// Invariants:
//
//  1. Nothing below the parser interprets. Tokens carry no text;
//     literals arrive undecoded and resolve through the File that
//     produced them.
//  2. No cross-file address space. A Pos is per-File.
//  3. Every span is non-empty (End > Pos), including ILLEGAL. The
//     scanner's EOF token is the one zero-width exception.
package token

// Pos is a compact position within one File: byte offset into the
// translated text, plus one, so the zero value NoPos is invalid.
type Pos int32

// NoPos is the invalid position. Fields like a delimiter that was
// never written hold NoPos.
const NoPos Pos = 0

func (p Pos) IsValid() bool { return p > NoPos }

// Flags carry lexical facts the parser ignores but diagnostics and
// formatters need.
type Flags uint8

const (
	// FlagAdjacent: no whitespace or comment separates this token
	// from the previous one.
	FlagAdjacent Flags = 1 << iota
	// FlagNLBefore: a line terminator appeared before this token.
	FlagNLBefore
	// FlagDigraph: this punctuator was spelled as a digraph
	// (<: :> <% %> %: %:%:); Kind holds the canonical punctuator.
	FlagDigraph
)

func (f Flags) Has(g Flags) bool { return f&g != 0 }

// Token is a kind and a span. It holds no text: spelling resolves
// through the File via Slice (translated) or Raw (as typed).
type Token struct {
	Kind  Kind
	Flags Flags
	Pos   Pos // inclusive
	End   Pos // exclusive
}