package lower

// gcc's inline assembly, onto §8b's.
//
// The two models are close but not the same, and the difference is entirely
// in the operand lists. gcc gives one operand both roles — `"+r"` is read and
// written, `"=m"` names an object the text stores through — while §8b gives
// an output and an input separate entries and lets an input match an output
// by number. So the C operand list is not the IR operand list, and the whole
// of this file is the translation between them:
//
//   - A read-write output becomes an IR output and an IR input tied to it,
//     which is the same register in both roles and is how §8b spells `+`.
//   - A memory operand becomes an IR input carrying the object's *address*,
//     for both directions. §8b has no output that is a place rather than a
//     value, and it does not need one: the text stores through the address,
//     and the backend prints the operand as a dereference.
//   - Everything else is an IR output or an IR input at the same position.
//
// Because the two numberings differ, the template cannot be handed on as
// written. `%0` in the C source names the first *C* operand; the IR reads it
// as the first *IR* operand, and after a `"=m"` output moved to the input
// list those are not the same thing. So the template is rewritten, and a
// matching constraint's digits are rewritten with it — they are an operand
// reference in constraint spelling. Symbolic names, `%[dst]`, resolve
// through the same map, which is why they cost nothing extra.

import (
	"strconv"
	"strings"

	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/types"
)

// asmOperand is one entry of the C operand lists, and where it landed in the
// IR's. Exactly one of irOut and irIn is set for an operand that survived
// classification; a read-write register operand sets both.
type asmOperand struct {
	node  *ast.AsmOperand
	con   string // the constraint, decoded
	base  string // the constraint without its =+&% modifiers
	isOut bool

	mem bool // the operand is an address the text dereferences
	rw  bool // `+`, so it is read as well as written

	lv lval // outputs, and read-write inputs
	ty types.Type

	irOut int // index into the IR output list, or -1
	irIn  int // index into the IR input list, or -1
}

// ref is the operand's number in the IR's numbering, which runs over the
// outputs and then the inputs.
func (o *asmOperand) ref(nouts int) int {
	if o.irOut >= 0 {
		return o.irOut
	}
	return nouts + o.irIn
}

// asmDecl emits gcc's file-scope assembly as §3b's module-level block.
//
// It is emitted in pass one, among the declarations, because the order
// between a block and the items around it is the only thing the order means:
// it decides where the block's bytes fall relative to theirs, and nothing
// else. Whatever the text defines it defines to the linker — the IR is
// explicit that a name it emits is not in the module's value namespace — so
// there is nothing here to declare.
func (u *unit) asmDecl(d *ast.AsmDecl) {
	if d.Template == nil {
		return // the parser reported it
	}
	u.mod.Asm(u.asmString(d.Template))
}

func (u *unit) asmStmt(s *ast.AsmStmt) {
	if s.Template == nil {
		return // the parser reported it and consumed the construct
	}
	tmpl := u.asmString(s.Template)

	// The compiler barrier every header writes as
	// `__asm__ volatile ("" ::: "memory")`. It orders this thread's accesses
	// against itself and emits no instruction, which is §8's single-thread
	// fence — a statement the IR can make directly, and a better one than an
	// empty template with a clobber list.
	if isBlankAsm(tmpl) && len(s.Outputs) == 0 && len(s.Inputs) == 0 {
		if asmClobbersMemory(u, s) {
			u.blk().Fence(ir.SeqCst, ir.SingleThread)
		}
		return
	}

	if !s.Extended {
		// Basic asm has no operands and, gcc says, no promise about the
		// registers it leaves alone. It is always volatile.
		u.blk().Asm(tmpl).Volatile().Emit()
		return
	}
	u.extendedAsm(s, tmpl)
}

func (u *unit) extendedAsm(s *ast.AsmStmt, tmpl string) {
	ops := make([]*asmOperand, 0, len(s.Outputs)+len(s.Inputs))
	for _, n := range s.Outputs {
		ops = append(ops, &asmOperand{node: n, isOut: true, irOut: -1, irIn: -1})
	}
	for _, n := range s.Inputs {
		ops = append(ops, &asmOperand{node: n, isOut: false, irOut: -1, irIn: -1})
	}

	if !u.classifyAsmOperands(s, ops) {
		return
	}

	// The IR's own numbering, decided before anything is emitted because the
	// template is rewritten against it and the template is what the builder
	// is opened with.
	nouts := 0
	for _, o := range ops {
		if o.isOut && !o.mem {
			o.irOut = nouts
			nouts++
		}
	}
	nins := 0
	assign := func(o *asmOperand) {
		o.irIn = nins
		nins++
	}
	for _, o := range ops {
		switch {
		case o.mem, !o.isOut:
			assign(o)
		case o.rw:
			assign(o) // the hidden input a `+` output is read through
		}
	}

	text, ok := u.rewriteAsmTemplate(s, tmpl, ops, nouts)
	if !ok {
		return
	}

	// Now the operands are evaluated, in the order they were written. An
	// output's designation comes first because a `+` reads it, and a
	// designation is not a read.
	for _, o := range ops {
		if o.isOut {
			o.lv = u.lvalue(o.node.X)
			o.ty = o.lv.t
		}
	}

	outs := make([]*asmOperand, nouts)
	ins := make([]ir.Value, nins)
	for _, o := range ops {
		if o.irOut >= 0 {
			outs[o.irOut] = o
		}
		if o.irIn < 0 {
			continue
		}
		switch {
		case o.mem && o.isOut:
			ins[o.irIn] = o.lv.addr
		case o.mem:
			l := u.lvalue(o.node.X)
			ins[o.irIn] = l.addr
			o.ty = l.t
		case o.isOut: // the read half of a `+`
			ins[o.irIn] = u.load(o.lv, o.node).v
		default:
			v := u.expr(o.node.X)
			ins[o.irIn] = v.v
			o.ty = v.t
		}
	}

	if s.Goto {
		u.asmGoto(s, text, ops, outs, ins)
		return
	}

	st := u.blk().Asm(text)
	if s.Volatile || len(s.Outputs) == 0 {
		// gcc treats an extended asm with no outputs as volatile, on the
		// reasoning that its only purpose can be an effect the compiler
		// cannot see.
		st = st.Volatile()
	}
	for _, o := range outs {
		rt, ok := u.types.regType(types.Unqualify(o.ty))
		if !ok {
			u.errorf(o.node, "an asm output of type %s does not fit a register; constrain it %q so the text stores through its address instead", o.ty, "=m")
			return
		}
		st = st.Out(rt, ir.CStr(o.con))
	}
	for _, o := range ops {
		if o.irIn >= 0 {
			st = st.In(ins[o.irIn], asmInConstraint(o))
		}
	}
	st = st.Clobber(u.asmClobbers(s)...)

	res := st.Emit()
	if res.Len() != nouts {
		return // the builder refused it and said so
	}
	for i, o := range outs {
		u.storeLval(value{res.Value(i), o.ty}, o.lv, o.node)
	}
}

// asmGoto emits the terminator form and the block the fallthrough lands in.
//
// The labels are the blocks the assembled text branches to. They are already
// blocks — prescanLabels gave every label in the function one before the body
// was walked — which is what lets an asm goto name a label written after it.
func (u *unit) asmGoto(s *ast.AsmStmt, text string, ops, outs []*asmOperand, ins []ir.Value) {
	labels := make([]*ir.Block, 0, len(s.Labels))
	for _, id := range s.Labels {
		lb := u.fn.labels[u.name(id)]
		if lb == nil {
			u.errorf(id, "%s is not a label in this function", u.name(id))
			return
		}
		labels = append(labels, lb.blk)
	}
	if len(labels) == 0 {
		u.errorf(s, "`asm goto` with no labels; the goto form exists for the labels")
		return
	}

	st := u.blk().AsmGoto(text)
	rts := make([]ir.RegType, len(outs))
	for i, o := range outs {
		rt, ok := u.types.regType(types.Unqualify(o.ty))
		if !ok {
			u.errorf(o.node, "an asm output of type %s does not fit a register; constrain it %q so the text stores through its address instead", o.ty, "=m")
			return
		}
		rts[i] = rt
		st = st.Out(rt, ir.CStr(o.con))
	}
	for _, o := range ops {
		if o.irIn >= 0 {
			st = st.In(ins[o.irIn], asmInConstraint(o))
		}
	}
	st = st.Clobber(u.asmClobbers(s)...)

	// The outputs are the fallthrough block's trailing parameters, which is
	// where §14 puts them and where gcc's rule puts them too: valid on the
	// path the text fell through, and on no other. The labels take none —
	// there is no edge for the compiler to write.
	fall := u.fn.fn.Block(u.uniq("asm_fall"))
	params := make([]ir.Value, len(outs))
	for i := range outs {
		params[i] = fall.Param(rts[i], u.uniq("asmout"))
	}
	st.To(fall.To(), labels...)

	u.enter(fall)
	for i, o := range outs {
		u.storeLval(value{params[i], o.ty}, o.lv, o.node)
	}
}

// classifyAsmOperands decides each operand's kind from its constraint, and
// reports the ones this compiler will not guess about.
func (u *unit) classifyAsmOperands(s *ast.AsmStmt, ops []*asmOperand) bool {
	ok := true
	for _, o := range ops {
		if o.node.Constraint == nil || o.node.X == nil {
			return false // the parser reported it
		}
		o.con = u.asmString(o.node.Constraint)
		o.base = strings.TrimLeft(o.con, "=+&%")
		o.rw = strings.ContainsRune(o.con, '+')
		o.mem = u.asmIsMemConstraint(o.base)

		switch {
		case o.isOut && !strings.ContainsAny(o.con, "=+"):
			u.errorf(o.node, "an asm output's constraint must begin with = or +, and %q does not", o.con)
			ok = false
		case !o.isOut && strings.ContainsAny(o.con, "=+"):
			u.errorf(o.node, "an asm input's constraint may not be written %q; = and + belong to an output", o.con)
			ok = false
		case o.base == "":
			u.errorf(o.node, "an asm operand's constraint is %q, which names no register class", o.con)
			ok = false
		case !o.mem && u.asmIsImmediateConstraint(o.base):
			// An immediate operand is a literal in the text rather than a
			// register, and §8b's operand model has no place to put one:
			// every operand it carries is a register the allocator assigned.
			u.errorf(o.node, "the constraint %q asks for an immediate operand, which is not lowered; write the constant into the template, or constrain it %q", o.con, "r")
			ok = false
		}
	}
	return ok
}

// asmInConstraint is the constraint the IR sees for one input.
//
// The hidden input of a read-write output is a matching constraint naming
// that output — which is the one place this compiler writes a constraint the
// C source did not. A tie the source wrote is renumbered by rewriteAsmTemplate
// along with the template's references.
func asmInConstraint(o *asmOperand) ir.Constraint {
	if o.isOut && o.rw && !o.mem {
		return ir.CStr(strconv.Itoa(o.irOut))
	}
	return ir.CStr(o.con)
}

// asmClobbers decodes the clobber list. The two pseudo-registers pass
// through as themselves; a register name is the target's to recognise, and
// the backend says so if it does not.
func (u *unit) asmClobbers(s *ast.AsmStmt) []string {
	out := make([]string, 0, len(s.Clobbers))
	for _, c := range s.Clobbers {
		if name := u.asmString(c); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func asmClobbersMemory(u *unit, s *ast.AsmStmt) bool {
	for _, c := range s.Clobbers {
		if u.asmString(c) == "memory" {
			return true
		}
	}
	return false
}

// asmIsMemConstraint reports whether a constraint names an address rather
// than a value.
//
// The letters are the target's, and one of them collides: `Q` is a memory
// operand on AArch64 and a register class on x86. The test is on the whole
// constraint rather than on any letter in it, so a multi-alternative
// constraint like `"rm"`, which offers the compiler a choice, takes the
// register alternative — the one this IR can always satisfy.
func (u *unit) asmIsMemConstraint(base string) bool {
	switch base {
	case "m", "o", "V", "mem":
		return true
	case "Q":
		return strings.HasPrefix(u.target.Use(), "aarch64")
	}
	return false
}

// asmIsImmediateConstraint reports whether every alternative the constraint
// offers is a constant. gcc's `i` and `n` are the portable two; the
// upper-case letters are per-target constant ranges, and `s` is a symbol.
func (u *unit) asmIsImmediateConstraint(base string) bool {
	if base == "imm" {
		return true
	}
	for i := 0; i < len(base); i++ {
		switch c := base[i]; {
		case c == 'i' || c == 'n' || c == 's' || c == 'E' || c == 'F':
			continue
		case c >= 'I' && c <= 'P':
			continue
		default:
			return false
		}
	}
	return base != ""
}

// asmString decodes one string literal to the bytes it holds.
func (u *unit) asmString(s *ast.StringLit) string {
	if s == nil {
		return ""
	}
	sv := u.decodeString(s)
	var b strings.Builder
	for _, r := range sv.Data[:max(len(sv.Data)-1, 0)] {
		b.WriteByte(byte(r))
	}
	return b.String()
}

// isBlankAsm reports whether a template holds no instruction.
func isBlankAsm(text string) bool {
	return strings.TrimSpace(text) == ""
}

// —— the template ——

// rewriteAsmTemplate renumbers a gcc template into §8b's numbering.
//
// Both spellings say the same thing in different words: `%0` and `%[dst]`
// name an operand, and the IR wants the operand's position in *its* lists.
// The rewrite is where the two models are actually reconciled, and it is why
// a `"=m"` output can move to the input list without the text noticing.
//
// A label reference keeps gcc's `%l` spelling and gains the block's name,
// because the IR names an asm goto's labels rather than numbering them —
// there is no number to renumber. `%%` passes through as `%%`: the assembler
// is the one that reads it, and the IR hands it the template unread.
func (u *unit) rewriteAsmTemplate(s *ast.AsmStmt, tmpl string, ops []*asmOperand, nouts int) (string, bool) {
	byName := make(map[string]*asmOperand, len(ops))
	for _, o := range ops {
		if o.node.Name != nil {
			byName[u.name(o.node.Name)] = o
		}
	}
	labelByName := make(map[string]string, len(s.Labels))
	labels := make([]string, 0, len(s.Labels))
	for _, id := range s.Labels {
		lb := u.fn.labels[u.name(id)]
		if lb == nil {
			continue // asmGoto reports it
		}
		labelByName[u.name(id)] = lb.blk.Label()
		labels = append(labels, lb.blk.Label())
	}

	var b strings.Builder
	for i := 0; i < len(tmpl); {
		if tmpl[i] != '%' {
			b.WriteByte(tmpl[i])
			i++
			continue
		}
		i++
		if i >= len(tmpl) {
			u.errorf(s, "the asm template ends in a %% with nothing after it")
			return "", false
		}

		switch tmpl[i] {
		case '%':
			b.WriteString("%%")
			i++
			continue
		case '=':
			// gcc's per-expansion unique number, written into a local label
			// so two expansions of one template do not collide. The IR
			// solves the same problem by prefixing each expansion's numeric
			// local labels, so `1:` and `1f` already work and this does not
			// have to.
			u.errorf(s, "%%= is not lowered; write the label as a numeric local label, `1:` and `1b`, which is kept distinct per expansion")
			return "", false
		case '{', '|', '}':
			u.errorf(s, "the %%{...|...%%} dialect alternatives are not lowered; write the one dialect this compiler assembles")
			return "", false
		case 'l':
			i++
			name, next, ok := u.asmLabelRef(s, tmpl, i, ops, labels, labelByName)
			if !ok {
				return "", false
			}
			b.WriteString("%l[" + name + "]")
			i = next
			continue
		}

		// An operand, with at most one modifier letter before it.
		mod := ""
		if tmpl[i] != '[' && !isASCIIDigit(tmpl[i]) {
			mod = string(tmpl[i])
			i++
			if i >= len(tmpl) {
				u.errorf(s, "the asm template ends in an operand modifier with no operand after it")
				return "", false
			}
		}

		o, next, ok := u.asmOperandRef(s, tmpl, i, ops, byName)
		if !ok {
			return "", false
		}
		b.WriteString("%" + mod + strconv.Itoa(o.ref(nouts)))
		i = next
	}
	return b.String(), true
}

// asmOperandRef reads the `0` of `%0` or the `[dst]` of `%[dst]`, and
// returns the operand it names.
func (u *unit) asmOperandRef(s *ast.AsmStmt, tmpl string, i int,
	ops []*asmOperand, byName map[string]*asmOperand) (*asmOperand, int, bool) {

	if tmpl[i] == '[' {
		name, next, ok := asmBracketed(tmpl, i)
		if !ok {
			u.errorf(s, "a symbolic operand reference in the asm template has no closing ']'")
			return nil, 0, false
		}
		o := byName[name]
		if o == nil {
			u.errorf(s, "the asm template names the operand [%s], which no operand declares", name)
			return nil, 0, false
		}
		return o, next, true
	}

	j := i
	for j < len(tmpl) && isASCIIDigit(tmpl[j]) {
		j++
	}
	if j == i {
		u.errorf(s, "%%%c is not an operand reference; write %%%% for a literal percent sign", tmpl[i])
		return nil, 0, false
	}
	n, err := strconv.Atoi(tmpl[i:j])
	if err != nil || n >= len(ops) {
		u.errorf(s, "the asm template names operand %%%s and there %s", tmpl[i:j], asmOperandCount(len(ops)))
		return nil, 0, false
	}
	return ops[n], j, true
}

// asmLabelRef reads the label of `%l[done]` or `%l2`.
//
// gcc numbers labels after the operands, so `%l2` in a statement with two
// operands is the first label. The bracketed spelling needs no such counting
// and is the one anything written this decade uses.
func (u *unit) asmLabelRef(s *ast.AsmStmt, tmpl string, i int, ops []*asmOperand,
	labels []string, byName map[string]string) (string, int, bool) {

	if i < len(tmpl) && tmpl[i] == '[' {
		name, next, ok := asmBracketed(tmpl, i)
		if !ok {
			u.errorf(s, "a label reference in the asm template has no closing ']'")
			return "", 0, false
		}
		blk, ok := byName[name]
		if !ok {
			u.errorf(s, "the asm template names the label %%l[%s], which is not in the statement's label list", name)
			return "", 0, false
		}
		return blk, next, true
	}

	j := i
	for j < len(tmpl) && isASCIIDigit(tmpl[j]) {
		j++
	}
	if j == i {
		u.errorf(s, "%%l must be followed by a label name in brackets or by its number")
		return "", 0, false
	}
	n, err := strconv.Atoi(tmpl[i:j])
	if err != nil || n < len(ops) || n-len(ops) >= len(labels) {
		u.errorf(s, "the asm template names label %%l%s, and the labels of this statement are numbered from %d", tmpl[i:j], len(ops))
		return "", 0, false
	}
	return labels[n-len(ops)], j, true
}

// asmBracketed reads a `[name]` beginning at i, and returns the name and the
// index just past the closing bracket.
func asmBracketed(tmpl string, i int) (string, int, bool) {
	end := strings.IndexByte(tmpl[i:], ']')
	if end < 0 {
		return "", 0, false
	}
	return tmpl[i+1 : i+end], i + end + 1, true
}

func asmOperandCount(n int) string {
	switch n {
	case 0:
		return "are no operands"
	case 1:
		return "is one operand"
	}
	return "are " + strconv.Itoa(n) + " operands"
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }

// —— assembler labels ——

// collectAsmLabels reads the `__asm__("name")` written after a declarator.
//
// It renames the symbol and changes nothing else: the object keeps its C
// name, its type and its linkage, and only the name the linker sees moves.
// A libc is the reason it exists — it is how one declaration of `fopen`
// reaches a definition called `fopen64` with no macro in between — and a
// compiler that reads libc headers has to honour it or link against the
// wrong symbol.
//
// The labels are collected before anything is declared, in one pass, because
// the rename has to be in place the first time a name reaches sym: a call
// emitted in the first function must already use the label of a function
// declared further down the file.
func (u *unit) collectAsmLabels() {
	note := func(id *ast.Ident, lit *ast.StringLit) {
		if id == nil || lit == nil {
			return
		}
		label := u.asmString(lit)
		if label == "" {
			return
		}
		if u.asmNames == nil {
			u.asmNames = map[string]string{}
		}
		u.asmNames[u.name(id)] = label
	}

	for _, d := range u.file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			note(d.Name, d.AsmLabel)
		case *ast.GenDecl:
			for _, id := range d.List {
				note(id.Decl.DeclName(), id.AsmLabel)
			}
		}
	}
}
