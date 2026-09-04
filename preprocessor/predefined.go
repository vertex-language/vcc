package preprocessor

import (
	"fmt"
	"time"

	"github.com/vertex-language/vcc/token"
)

// installPredefines defines the standard macros, then applies the caller's
// -D and -U in order.
//
// Everything target-dependent — __CHAR_BIT__, __SIZEOF_LONG__, __INT_MAX__ and
// kin — arrives through Config.Predefines, computed by the vcc package from a
// types.Model. This package never learns what a target is, the same inversion
// that keeps sysroot out of phase 4.
func (p *Preprocessor) installPredefines() {
	std := func(name string, b Builtin) {
		m := &Macro{Name: name, ObjLike: true, Builtin: b}
		p.macros.Define(m)
	}
	std("__FILE__", BuiltinFile)
	std("__LINE__", BuiltinLine)
	std("__DATE__", BuiltinDate)
	std("__TIME__", BuiltinTime)
	// __COUNTER__ is gcc's, and the only way to build a name that is unique
	// per expansion — which is what a macro that declares something needs
	// when it may be used twice in one scope. Every assertion macro that
	// declares a static is written with it.
	std("__COUNTER__", BuiltinCounter)

	hosted := "0"
	if p.cfg.Hosted {
		hosted = "1"
	}
	for _, d := range [][2]string{
		{"__STDC__", "1"},
		{"__STDC_VERSION__", p.cfg.Std.Version()},
		{"__STDC_HOSTED__", hosted},
		{"__VCC__", "1"},

		// §6.10.8.3's conditional feature macros. Each one says vcc does not
		// implement an optional part of the language, and each is defined
		// because that is true today rather than because it is convenient: a
		// program that tests them is entitled to be told, and a program that
		// does not is entitled to have the feature work.
		//
		// Complex arithmetic is not lowered, so <complex.h> is not provided.
		{"__STDC_NO_COMPLEX__", "1"},
		// <threads.h> is not provided. The _Thread_local storage class is
		// a separate feature: §6.10.8.3 says this macro is about the
		// header and the thread API, not about the qualifier. It is
		// implemented where the target has a model for it, which today is
		// Windows; elsewhere a declaration is refused at lowering rather
		// than accepted and placed somewhere nothing can reach.
		{"__STDC_NO_THREADS__", "1"},

		// vcc claims GCC compatibility because the C it has to compile is
		// the C that exists, and that C is written for gcc and clang. A
		// platform's own headers are the first and least avoidable case:
		// Darwin's <sys/cdefs.h> greets a compiler that does not define
		// __GNUC__ with "#warning Unsupported compiler detected", and then
		// <libkern/_OSByteOrder.h> declines to declare the byte swaps that
		// htons expands to, so ordinary networking code does not compile.
		//
		// The version is the one clang reports. It is not a claim to be gcc
		// 4.2 — nothing is — but the number every header's feature test was
		// written against, and raising it only opts into newer extensions.
		{"__GNUC__", "4"},
		{"__GNUC_MINOR__", "2"},
		{"__GNUC_PATCHLEVEL__", "1"},
	} {
		p.definePlain(d[0], d[1])
	}
	// __STDC_NO_VLA__ and __STDC_NO_ATOMICS__ are deliberately absent:
	// variable length arrays lower, and so does the _Atomic qualifier.

	for _, d := range p.cfg.Predefines {
		switch d.Kind {
		case PredefineUndef:
			p.macros.Undef(d.Text)
		default:
			p.defineFromFlag(d.Text)
		}
	}
}

func (p *Preprocessor) definePlain(name, value string) {
	m := &Macro{Name: name, ObjLike: true}
	if value != "" {
		m.Body = []Token{p.gen.Mint(token.INT_LIT, value)}
	}
	p.macros.Define(m)
}

// defineFromFlag runs a -D spelling through the same #define grammar a
// directive uses, so the two cannot drift apart. `-D NAME` defines NAME as 1.
func (p *Preprocessor) defineFromFlag(text string) {
	src := text
	if i := indexByte(text, '='); i >= 0 {
		src = text[:i] + " " + text[i+1:]
	} else {
		src = text + " 1"
	}
	f := token.NewFile("<command-line>", []byte(src+"\n"))
	toks, diags := scanPP(f)
	for _, d := range diags {
		p.fromToken(f, d)
	}
	org := &Origin{File: f}
	line := p.wrap(toks, org)
	p.doDefine(&reader{org: org}, trimEOF(line), Site{Origin: org, Pos: f.Pos(0), End: f.Pos(1)})
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func trimEOF(ts []Token) []Token {
	if n := len(ts); n > 0 && ts[n-1].Kind == token.EOF {
		return ts[:n-1]
	}
	return ts
}

// builtin computes the replacement list for a computed macro. Each returns
// exactly one token, minted into the generated arena, so the result is an
// ordinary span in an ordinary position space.
//
// __DATE__ and __TIME__ read Config.Epoch, never a clock: SOURCE_DATE_EPOCH is
// the same contract arc's output formats hold to, and there is no other mode.
func (p *Preprocessor) builtin(m *Macro, at Token) []Token {
	var t Token
	switch m.Builtin {
	case BuiltinFile:
		t = p.gen.Mint(token.STRING_LIT, fmt.Sprintf("%q", p.currentFileName(at)))
	case BuiltinLine:
		t = p.gen.Mint(token.INT_LIT, fmt.Sprint(p.currentLine(at)))
	case BuiltinDate:
		t = p.gen.Mint(token.STRING_LIT, `"`+p.epochFormat("Jan  2 2006")+`"`)
	case BuiltinTime:
		t = p.gen.Mint(token.STRING_LIT, `"`+p.epochFormat("15:04:05")+`"`)
	case BuiltinCounter:
		t = p.gen.Mint(token.INT_LIT, fmt.Sprint(p.counter))
		p.counter++
	default:
		return nil
	}
	t.Flags = at.Flags & token.FlagAdjacent
	t.Exp = &Expansion{Macro: m.Name, Use: at.Site(), Outer: at.Exp}
	return []Token{t}
}

func (p *Preprocessor) epochFormat(layout string) string {
	now := p.cfg.Now()
	if now.IsZero() {
		now = time.Unix(0, 0).UTC()
	}
	return now.Format(layout)
}

// currentFileName is the path as written, never made absolute: an absolute
// path is the build machine leaking into the output.
func (p *Preprocessor) currentFileName(at Token) string {
	if p.fileName != "" {
		return p.fileName
	}
	s := at.Site()
	if s.Origin != nil {
		return s.Origin.Name()
	}
	return "<unknown>"
}

func (p *Preprocessor) currentLine(at Token) int {
	return p.physicalLine(at.Site()) + p.lineDelta
}

func (p *Preprocessor) physicalLine(s Site) int {
	if s.Origin == nil || s.Origin.File == nil || !s.Pos.IsValid() {
		return 0
	}
	return s.Origin.File.Position(s.Pos).Line
}