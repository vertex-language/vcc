package parser

import (
	"testing"

	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
)

// asmIn parses one function body and returns the asm statement in it. The
// bodies here declare the objects their operands name, so the statement is
// rarely the only item.
func asmIn(t *testing.T, body string) (*token.File, *ast.AsmStmt) {
	t.Helper()

	unit, f, diags := parse(t, "void f(void) { "+body+" }", 0)
	mustClean(t, diags)
	for _, it := range f.Decls[0].(*ast.FuncDecl).Body.Items {
		if a, ok := it.(*ast.AsmStmt); ok {
			return unit, a
		}
	}
	t.Fatal("no asm statement in the body")
	return nil, nil
}

// text is a string literal's spelling, quotes and all, which is enough to
// tell the tests which literal they got.
func text(unit *token.File, s *ast.StringLit) string {
	if s == nil {
		return "<nil>"
	}
	return string(unit.Slice(s.Lo, s.Hi))
}

// Basic asm: no colon anywhere, so the whole construct is the template and
// the operand lists do not exist rather than being empty.
func TestParseBasicAsm(t *testing.T) {
	unit, a := asmIn(t, `__asm__("nop");`)

	if a.Extended {
		t.Error("Extended is true; a form with no colon is basic asm")
	}
	if got := text(unit, a.Template); got != `"nop"` {
		t.Errorf("Template = %s, want \"nop\"", got)
	}
	if a.Volatile || a.Goto || a.Inline {
		t.Error("no qualifier was written, and one is set")
	}
}

// The qualifiers, in gcc's decorated spellings and in any order.
func TestParseAsmQualifiers(t *testing.T) {
	for _, src := range []string{
		`asm volatile ("nop");`,
		`__asm__ __volatile__ ("nop");`,
		`asm __volatile__ ("nop");`,
	} {
		t.Run(src, func(t *testing.T) {
			_, a := asmIn(t, src)
			if !a.Volatile {
				t.Error("Volatile is false")
			}
		})
	}

	_, a := asmIn(t, `asm goto ("jmp %l[done]" : : : : done); done: ;`)
	if !a.Goto {
		t.Error("Goto is false")
	}
}

// The extended form, with every list occupied. This is the shape a kernel
// header writes and the one the old parser threw away.
func TestParseExtendedAsm(t *testing.T) {
	unit, a := asmIn(t, `int x, y;
		asm volatile ("addl %1, %0"
		    : "=r" (x)
		    : "r" (y), "i" (3)
		    : "cc", "memory");`)

	if !a.Extended || !a.Volatile {
		t.Fatalf("Extended = %v, Volatile = %v; want both true", a.Extended, a.Volatile)
	}
	if got := text(unit, a.Template); got != `"addl %1, %0"` {
		t.Errorf("Template = %s", got)
	}

	if len(a.Outputs) != 1 {
		t.Fatalf("Outputs has %d entries, want one", len(a.Outputs))
	}
	if got := text(unit, a.Outputs[0].Constraint); got != `"=r"` {
		t.Errorf("output constraint = %s, want \"=r\"", got)
	}
	if _, ok := a.Outputs[0].X.(*ast.Ident); !ok {
		t.Errorf("output expression is %T, want an identifier", a.Outputs[0].X)
	}

	if len(a.Inputs) != 2 {
		t.Fatalf("Inputs has %d entries, want two", len(a.Inputs))
	}
	if got := text(unit, a.Inputs[1].Constraint); got != `"i"` {
		t.Errorf("second input constraint = %s, want \"i\"", got)
	}
	if len(a.Clobbers) != 2 {
		t.Fatalf("Clobbers has %d entries, want two", len(a.Clobbers))
	}
	if got := text(unit, a.Clobbers[1]); got != `"memory"` {
		t.Errorf("second clobber = %s, want \"memory\"", got)
	}
}

// The empty lists an omitted section leaves. `::: "memory"` is the barrier
// every header writes, and its three colons are three empty lists and not
// a typo.
func TestParseAsmEmptySections(t *testing.T) {
	unit, a := asmIn(t, `asm volatile ("" ::: "memory");`)

	if !a.Extended {
		t.Error("Extended is false; the colons make it extended even when the lists are empty")
	}
	if len(a.Outputs) != 0 || len(a.Inputs) != 0 {
		t.Errorf("Outputs = %v, Inputs = %v; want both empty", a.Outputs, a.Inputs)
	}
	if len(a.Clobbers) != 1 || text(unit, a.Clobbers[0]) != `"memory"` {
		t.Errorf("Clobbers = %d entries, want the one", len(a.Clobbers))
	}
}

// Symbolic operand names, which are an alias for a position rather than a
// second way to declare one.
func TestParseAsmSymbolicNames(t *testing.T) {
	unit, a := asmIn(t, `int x, y;
		asm ("mov %[src], %[dst]" : [dst] "=r" (x) : [src] "r" (y));`)

	if len(a.Outputs) != 1 || a.Outputs[0].Name == nil {
		t.Fatal("the output has no symbolic name")
	}
	if got := string(unit.Slice(a.Outputs[0].Name.Lo, a.Outputs[0].Name.Hi)); got != "dst" {
		t.Errorf("output name = %q, want dst", got)
	}
	if len(a.Inputs) != 1 || a.Inputs[0].Name == nil {
		t.Fatal("the input has no symbolic name")
	}
}

// asm goto, whose fifth list is labels and not objects.
func TestParseAsmGoto(t *testing.T) {
	unit, a := asmIn(t, `int x;
		asm goto ("test %0, %0\n\tjnz %l[nz]"
		    :
		    : "r" (x)
		    : "cc"
		    : nz, zero);
		nz: zero: ;`)

	if !a.Goto {
		t.Fatal("Goto is false")
	}
	if len(a.Outputs) != 0 {
		t.Errorf("Outputs has %d entries; asm goto's second section is empty here", len(a.Outputs))
	}
	if len(a.Inputs) != 1 {
		t.Errorf("Inputs has %d entries, want one", len(a.Inputs))
	}
	if len(a.Labels) != 2 {
		t.Fatalf("Labels has %d entries, want two", len(a.Labels))
	}
	if got := string(unit.Slice(a.Labels[0].Lo, a.Labels[0].Hi)); got != "nz" {
		t.Errorf("first label = %q, want nz", got)
	}
}

// An operand's expression is an expression, not just a name.
func TestParseAsmOperandExpressions(t *testing.T) {
	_, a := asmIn(t, `int v[4]; int *p;
		asm ("" : "=m" (v[2]) : "r" (p + 1));`)

	if _, ok := a.Outputs[0].X.(*ast.IndexExpr); !ok {
		t.Errorf("output expression is %T, want an index", a.Outputs[0].X)
	}
	if _, ok := a.Inputs[0].X.(*ast.BinaryExpr); !ok {
		t.Errorf("input expression is %T, want a binary expression", a.Inputs[0].X)
	}
}

// Adjacent string literals concatenate in the template, which is how a
// multi-instruction template is written across source lines.
func TestParseAsmTemplateRun(t *testing.T) {
	_, a := asmIn(t, `asm ("push %rax\n\t"
		             "pop %rax");`)

	if a.Template == nil || len(a.Template.Segs) != 2 {
		t.Fatalf("Template has %v segments, want two", a.Template)
	}
}

// A malformed asm is one diagnostic and not a cascade, and the statement
// still has an extent.
func TestParseAsmMalformed(t *testing.T) {
	_, f, diags := parse(t, `void f(void) { asm ("nop" : "=r" 1); }`, 0)
	if errCount(diags) == 0 {
		t.Fatal("no diagnostic for an operand with no parenthesized expression")
	}
	if errCount(diags) > 1 {
		t.Errorf("%d diagnostics, want one: %v", errCount(diags), diags)
	}
	items := f.Decls[0].(*ast.FuncDecl).Body.Items
	if len(items) != 1 {
		t.Fatalf("body has %d items, want the one recovered statement", len(items))
	}
}

// gcc's file-scope assembly, which is a declaration only in the sense of
// appearing where one does.
func TestParseFileScopeAsm(t *testing.T) {
	unit, f, diags := parse(t, `__asm__(".globl x\nx: .quad 1"); int after;`, 0)
	mustClean(t, diags)

	if len(f.Decls) != 2 {
		t.Fatalf("file has %d declarations, want two", len(f.Decls))
	}
	a, ok := f.Decls[0].(*ast.AsmDecl)
	if !ok {
		t.Fatalf("first declaration is %T, want *ast.AsmDecl", f.Decls[0])
	}
	if got := text(unit, a.Template); got != `".globl x\nx: .quad 1"` {
		t.Errorf("Template = %s", got)
	}
	if _, ok := f.Decls[1].(*ast.GenDecl); !ok {
		t.Errorf("the declaration after it is %T, want a GenDecl", f.Decls[1])
	}
}

// The assembler label, which renames a symbol and is the one tolerated
// group whose contents are kept.
func TestParseAsmLabel(t *testing.T) {
	unit, f, diags := parse(t, `
		int counter __asm__("renamed") = 3;
		extern int f(void) __asm__("real_f");
		int g(void) __asm__("real_g") { return 0; }
		int plain;
	`, 0)
	mustClean(t, diags)

	g0 := f.Decls[0].(*ast.GenDecl)
	if got := text(unit, g0.List[0].AsmLabel); got != `"renamed"` {
		t.Errorf("object label = %s, want \"renamed\"", got)
	}
	g1 := f.Decls[1].(*ast.GenDecl)
	if got := text(unit, g1.List[0].AsmLabel); got != `"real_f"` {
		t.Errorf("declaration label = %s, want \"real_f\"", got)
	}
	fn := f.Decls[2].(*ast.FuncDecl)
	if got := text(unit, fn.AsmLabel); got != `"real_g"` {
		t.Errorf("definition label = %s, want \"real_g\"", got)
	}
	if l := f.Decls[3].(*ast.GenDecl).List[0].AsmLabel; l != nil {
		t.Errorf("a declaration with no label has %s; the previous one leaked", text(unit, l))
	}
}
