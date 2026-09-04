package lower

import (
	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// fnState is one function body's emission state.
type fnState struct {
	decl  *ast.FuncDecl
	ft    *types.Func
	fn    *ir.Func
	retTy types.Type

	entry *ir.Block
	cur   *ir.Block
	live  bool

	params    []ir.Value
	sretParam ir.Ptr
	sret      ir.Ptr
	hasSret   bool
	isMain    bool

	// vlaDims holds each VLA dimension's length, evaluated once where the
	// declaration was reached. §6.7.6.2p5 evaluates a size expression exactly
	// there and nowhere else, so a later sizeof reads the value back instead
	// of running the expression again — which for `int a[get_size()]` is the
	// difference between calling get_size once and calling it twice.
	//
	// It hangs off the function because the values are ir.Values: they are
	// only meaningful inside the function that computed them.
	vlaDims map[*types.Array]ir.I64

	labels map[string]*labelBlock

	// addrTaken names the labels &&label was applied to, in first-use order.
	// A computed goto names all of them as its targets, because the address
	// it branches through cannot be traced to one.
	addrTaken map[string]bool
	addrOrder []string
	brk       *ir.Block
	cont      *ir.Block
	sw        *switchState

	statics int
}

type labelBlock struct {
	blk  *ir.Block
	seen bool
}

// blk returns the block instructions go into.
//
// Code after a return, break, or goto is unreachable but still has to be
// lowered — it may declare objects, and it may hold a label that something
// jumps to. It goes into a fresh block that nothing branches to, which the
// verifier's RPO omits and which lowering discards. The alternative, tracking
// reachability through every emitter, would put a nil check on every line of
// this package.
func (u *unit) blk() *ir.Block {
	f := u.fn
	if f == nil {
		u.errorf(u.file, "internal: emission outside a function body")
		return nil
	}
	if !f.live {
		f.cur = f.fn.Block(u.uniq("dead"))
		f.live = true
	}
	return f.cur
}

// enter makes b the current block.
func (u *unit) enter(b *ir.Block) {
	u.fn.cur, u.fn.live = b, true
}

// leave marks the current block terminated.
func (u *unit) leave() { u.fn.live = false }

func (u *unit) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.AsmStmt:
		u.asmStmt(s)

	case *ast.CompoundStmt:
		u.push()
		save := u.saveStack()
		u.stmtList(s)
		u.restoreStack(save)
		u.pop()

	case *ast.DeclStmt:
		u.localDecl(s.D)

	case *ast.ExprStmt:
		u.discard(s.X)

	case *ast.EmptyStmt, *ast.BadStmt:

	case *ast.SEHTryStmt:
		// Emulate a standard block for now, avoiding PE exception handling.
		u.stmt(s.Body)
		
		// The handler block needs to be emitted to resolve any nested labels,
		// but shouldn't execute in a success path.
		resume := u.fn.fn.Block(u.uniq("seh_resume"))
		handler := u.fn.fn.Block(u.uniq("seh_handler"))
		
		u.blk().Br(resume.To())
		u.enter(handler)
		if s.Filter != nil {
			u.discard(s.Filter)
		}
		u.stmt(s.Handler)
		u.blk().Br(resume.To())
		u.enter(resume)

	case *ast.SEHLeaveStmt:
		// Not implemented fully, just a break.
		
	case *ast.IfStmt:
		u.ifStmt(s)

	case *ast.WhileStmt:
		u.whileStmt(s)

	case *ast.DoStmt:
		u.doStmt(s)

	case *ast.ForStmt:
		u.forStmt(s)

	case *ast.SwitchStmt:
		u.switchStmt(s)

	case *ast.CaseStmt:
		u.caseStmt(s)

	case *ast.LabeledStmt:
		u.labeledStmt(s)

	case *ast.GotoStmt:
		u.gotoStmt(s)

	case *ast.BreakStmt:
		if u.fn.brk == nil {
			u.errorf(s, "internal: break outside a loop or switch")
			return
		}
		u.blk().Br(u.fn.brk.To())
		u.leave()

	case *ast.ContinueStmt:
		if u.fn.cont == nil {
			u.errorf(s, "internal: continue outside a loop")
			return
		}
		u.blk().Br(u.fn.cont.To())
		u.leave()

	case *ast.ReturnStmt:
		u.returnStmt(s)

	default:
		u.errorf(s, "internal: %T is not lowered", s)
	}
}

func (u *unit) stmtList(c *ast.CompoundStmt) {
	if c == nil {
		return
	}
	for _, it := range c.Items {
		u.stmt(it)
	}
}

func (u *unit) ifStmt(s *ast.IfStmt) {
	f := u.fn
	thenB := f.fn.Block(u.uniq("if_then"))
	end := f.fn.Block(u.uniq("if_end"))
	elseB := end
	if s.Else != nil {
		elseB = f.fn.Block(u.uniq("if_else"))
	}

	c := u.truth(u.expr(s.Cond), s)
	u.blk().BrIf(c, thenB.To(), elseB.To())

	u.enter(thenB)
	u.stmt(s.Then)
	if f.live {
		u.blk().Br(end.To())
	}
	if s.Else != nil {
		u.enter(elseB)
		u.stmt(s.Else)
		if f.live {
			u.blk().Br(end.To())
		}
	}
	u.enter(end)
}

func (u *unit) whileStmt(s *ast.WhileStmt) {
	f := u.fn
	cond := f.fn.Block(u.uniq("while_cond"))
	body := f.fn.Block(u.uniq("while_body"))
	end := f.fn.Block(u.uniq("while_end"))

	u.blk().Br(cond.To())
	u.enter(cond)
	c := u.truth(u.expr(s.Cond), s)
	u.blk().BrIf(c, body.To(), end.To())

	u.enter(body)
	u.loop(cond, end, func() { u.stmt(s.Body) })
	if f.live {
		u.blk().Br(cond.To())
	}
	u.enter(end)
}

func (u *unit) doStmt(s *ast.DoStmt) {
	f := u.fn
	body := f.fn.Block(u.uniq("do_body"))
	cond := f.fn.Block(u.uniq("do_cond"))
	end := f.fn.Block(u.uniq("do_end"))

	u.blk().Br(body.To())
	u.enter(body)
	u.loop(cond, end, func() { u.stmt(s.Body) })
	if f.live {
		u.blk().Br(cond.To())
	}
	u.enter(cond)
	c := u.truth(u.expr(s.Cond), s)
	u.blk().BrIf(c, body.To(), end.To())
	u.enter(end)
}

// forStmt lowers both for forms. The declaration form gets its own scope, so
// that the loop variable does not outlive the loop.
func (u *unit) forStmt(s *ast.ForStmt) {
	f := u.fn
	u.push()
	save := u.saveStack()

	switch init := s.Init.(type) {
	case nil:
	case *ast.GenDecl:
		u.localDecl(init)
	case ast.Expr:
		u.discard(init)
	}

	cond := f.fn.Block(u.uniq("for_cond"))
	body := f.fn.Block(u.uniq("for_body"))
	post := f.fn.Block(u.uniq("for_post"))
	end := f.fn.Block(u.uniq("for_end"))

	u.blk().Br(cond.To())
	u.enter(cond)
	if s.Cond != nil {
		c := u.truth(u.expr(s.Cond), s)
		u.blk().BrIf(c, body.To(), end.To())
	} else {
		u.blk().Br(body.To())
	}

	u.enter(body)
	// continue goes to the post expression, not to the condition: §6.8.6.2p2.
	u.loop(post, end, func() { u.stmt(s.Body) })
	if f.live {
		u.blk().Br(post.To())
	}

	u.enter(post)
	u.discard(s.Post)
	u.blk().Br(cond.To())

	u.enter(end)
	u.restoreStack(save)
	u.pop()
}

// loop runs body with break and continue bound, restoring them after.
func (u *unit) loop(cont, brk *ir.Block, body func()) {
	f := u.fn
	oc, ob := f.cont, f.brk
	f.cont, f.brk = cont, brk
	body()
	f.cont, f.brk = oc, ob
}

// ---- switch --------------------------------------------------------------

type switchCase struct {
	val int64
	blk *ir.Block
}

type switchState struct {
	node   *ast.SwitchStmt
	cases  []switchCase
	byNode map[*ast.CaseStmt]*ir.Block
	dflt   *ir.Block
	end    *ir.Block
	typ    types.Type
}

// switchStmt lowers a switch as a dispatch followed by the body.
//
// The case labels are found by a pre-scan rather than by structure, because C
// admits them anywhere inside the body — inside an if, inside a loop, halfway
// through a do-while (Duff's device). So every case gets a block up front,
// the dispatch is emitted before the body, and lowering the body simply
// branches into the block when it walks past the label.
func (u *unit) switchStmt(s *ast.SwitchStmt) {
	f := u.fn
	ctrl := u.expr(s.Cond)
	ct := u.promote(ctrl.t)
	sel := u.convert(ctrl, ct, s)

	sw := &switchState{
		node:   s,
		byNode: map[*ast.CaseStmt]*ir.Block{},
		end:    f.fn.Block(u.uniq("sw_end")),
		typ:    ct,
	}
	for _, c := range findCases(s.Body) {
		if c.Kind == token.DEFAULT {
			sw.dflt = f.fn.Block(u.uniq("sw_default"))
			sw.byNode[c] = sw.dflt
			continue
		}
		v, ok := u.constOf(c.Value)
		if !ok {
			u.errorf(c, "internal: case value was not recorded as a constant")
			continue
		}
		blk := f.fn.Block(u.uniq("sw_case"))
		sw.byNode[c] = blk
		hi := v
		if c.High != nil {
			// gcc's `case lo ... hi:`. The dispatch is a table of values, so
			// the range is expanded into one entry per value — which is what
			// it means, and what a jump table would hold anyway. A range
			// wide enough to be worth a comparison instead is refused rather
			// than silently expanded into a million entries.
			n, ok := u.constOf(c.High)
			if !ok {
				u.errorf(c, "internal: the upper bound of a case range was not recorded as a constant")
				continue
			}
			if n < v {
				u.warnf(c, "case range %d ... %d is empty", v, n)
				continue
			}
			if n-v >= 4096 {
				u.errorf(c, "case range %d ... %d covers %d values; vcc expands a range into one label per value",
					v, n, n-v+1)
				continue
			}
			hi = n
		}
		for x := v; x <= hi; x++ {
			sw.cases = append(sw.cases, switchCase{val: x, blk: blk})
		}
	}
	dflt := sw.dflt
	if dflt == nil {
		dflt = sw.end
	}

	u.dispatch(sel, sw, dflt, s)

	u.leave() // the dispatch terminated its block; the body starts unreachable
	osw, obrk := f.sw, f.brk
	f.sw, f.brk = sw, sw.end
	u.stmt(s.Body)
	f.sw, f.brk = osw, obrk
	if f.live {
		u.blk().Br(sw.end.To())
	}
	u.enter(sw.end)
}

// dispatch emits the selector test.
//
// A dense set of cases becomes a subtract, an unsigned range check, and a
// br_table — which is exactly the shape §G2 describes, and the range check is
// the work the frontend was going to do to find the default edge anyway. A
// sparse set becomes a comparison chain, because a table spanning the gaps
// would be mostly default edges.
func (u *unit) dispatch(sel value, sw *switchState, dflt *ir.Block, at ast.Node) {
	if len(sw.cases) == 0 {
		u.blk().Br(dflt.To())
		return
	}
	lo, hi := sw.cases[0].val, sw.cases[0].val
	for _, c := range sw.cases {
		if c.val < lo {
			lo = c.val
		}
		if c.val > hi {
			hi = c.val
		}
	}
	span := hi - lo + 1
	dense := span <= 4096 && span <= 4*int64(len(sw.cases)) && len(sw.cases) >= 4

	if !dense {
		for _, c := range sw.cases {
			b := u.blk()
			k := u.convert(u.intConst(c.val, types.Typ(types.LongLong)), sw.typ, at)
			cond := u.emitCompare(b, sel.v, k.v, sw.typ, token.EQL, at)
			next := u.fn.fn.Block(u.uniq("sw_test"))
			b.BrIf(cond, c.blk.To(), next.To())
			u.enter(next)
		}
		u.blk().Br(dflt.To())
		return
	}

	b := u.blk()
	base := u.convert(u.intConst(lo, types.Typ(types.LongLong)), sw.typ, at)
	off := value{u.emitSub(b, sel.v, base.v, sw.typ, at), sw.typ}
	limit := u.convert(u.intConst(span, types.Typ(types.LongLong)), sw.typ, at)

	// The range check is unsigned on the wrapped difference, which catches
	// both ends with one comparison.
	var in ir.I1
	switch x := off.v.(type) {
	case ir.I32:
		in = b.I32.ULt(x, u.i32(limit.v, at))
	case ir.I64:
		in = b.I64.ULt(x, u.i64(limit.v, at))
	default:
		u.errorf(at, "internal: switch selector is not an integer")
		return
	}
	table := u.fn.fn.Block(u.uniq("sw_table"))
	b.BrIf(in, table.To(), dflt.To())

	u.enter(table)
	tb := u.blk()
	idx := off.v
	if x, ok := idx.(ir.I64); ok {
		idx = tb.I32.WrapI64(x)
	}
	targets := make([]ir.BlockTarget, span)
	for i := range targets {
		targets[i] = dflt.To()
	}
	for _, c := range sw.cases {
		targets[c.val-lo] = c.blk.To()
	}
	tb.BrTable(u.i32(idx, at), targets, dflt.To())
	u.leave()
}

// findCases collects the case and default labels a switch owns: every one in
// the body except those belonging to a nested switch.
func findCases(body ast.Stmt) []*ast.CaseStmt {
	var out []*ast.CaseStmt
	var walk func(ast.Node) bool
	walk = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SwitchStmt:
			if n != body {
				return false
			}
		case *ast.CaseStmt:
			out = append(out, n)
		}
		return true
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if s, ok := n.(*ast.SwitchStmt); ok && ast.Node(s) != ast.Node(body) {
			return false // a nested switch owns its own labels
		}
		return walk(n)
	})
	return out
}

// caseStmt is reached while lowering the body: fall into the block the
// dispatch already branches to, then continue with the labelled statement.
func (u *unit) caseStmt(s *ast.CaseStmt) {
	f := u.fn
	if f.sw == nil {
		u.errorf(s, "internal: case label outside a switch")
		u.stmt(s.Stmt)
		return
	}
	blk := f.sw.byNode[s]
	if blk == nil {
		u.stmt(s.Stmt)
		return
	}
	if f.live {
		u.blk().Br(blk.To())
	}
	u.enter(blk)
	u.stmt(s.Stmt)
}

// ---- labels and goto -----------------------------------------------------

// prescanLabels gives every label in the function a block before the body is
// walked, so that a forward goto has a target.
func (u *unit) prescanLabels(body *ast.CompoundStmt) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.LabeledStmt:
			name := u.name(n.Label)
			if _, dup := u.fn.labels[name]; !dup {
				u.fn.labels[name] = &labelBlock{blk: u.fn.fn.Block("L_" + sanitize(name))}
			}
		case *ast.LabelAddrExpr:
			// The labels a computed goto may reach, collected here rather
			// than as &&label is lowered: a `goto *p` can precede every
			// &&label in the function, and §19.6 wants the target list at
			// the branch.
			if n.Label == nil {
				return true
			}
			name := u.name(n.Label)
			if u.fn.addrTaken == nil {
				u.fn.addrTaken = map[string]bool{}
			}
			if !u.fn.addrTaken[name] {
				u.fn.addrTaken[name] = true
				u.fn.addrOrder = append(u.fn.addrOrder, name)
			}
		}
		return true
	})
}

func (u *unit) labeledStmt(s *ast.LabeledStmt) {
	lb := u.fn.labels[u.name(s.Label)]
	if lb == nil {
		u.stmt(s.Stmt)
		return
	}
	if u.fn.live {
		u.blk().Br(lb.blk.To())
	}
	lb.seen = true
	u.enter(lb.blk)
	u.stmt(s.Stmt)
}

// gotoStmt is a plain branch.
//
// It is a plain branch only because every object lives in the frame: no block
// takes a parameter, so there is nothing to supply across an edge that skips
// arbitrary initialization. A frontend that built SSA here would owe this
// statement a merge.
func (u *unit) gotoStmt(s *ast.GotoStmt) {
	if s.Target != nil {
		u.computedGoto(s)
		return
	}
	lb := u.fn.labels[u.name(s.Label)]
	if lb == nil {
		u.errorf(s, "internal: no block for label %s", u.name(s.Label))
		return
	}
	u.blk().Br(lb.blk.To())
	u.leave()
}

// computedGoto lowers gcc's `goto *expr`.
//
// §19.6 requires every block a brind can reach to be named as one of its
// targets, and the address came from a &&label the compiler cannot trace back
// to a particular label. So the target list is every label in the function
// whose address was taken — which is exactly the set a &&label can produce,
// and no larger.
func (u *unit) computedGoto(s *ast.GotoStmt) {
	p := u.ptr(u.convert(u.expr(s.Target), &types.Pointer{Elem: types.Typ(types.Void)}, s).v, s)
	if p.IsZero() {
		return
	}
	targets := make([]*ir.Block, 0, len(u.fn.addrTaken))
	for _, name := range u.fn.addrOrder {
		if lb := u.fn.labels[name]; lb != nil {
			targets = append(targets, lb.blk)
		}
	}
	if len(targets) == 0 {
		u.errorf(s, "a computed goto needs at least one label whose address was taken with &&")
		return
	}
	u.blk().BrInd(p, targets...)
	u.leave()
}

// labelAddr lowers &&label.
func (u *unit) labelAddr(e *ast.LabelAddrExpr) value {
	vt := &types.Pointer{Elem: types.Typ(types.Void)}
	if u.fn == nil || e.Label == nil {
		return u.poison(vt)
	}
	name := u.name(e.Label)
	lb := u.fn.labels[name]
	if lb == nil {
		u.errorf(e, "internal: no block for label %s", name)
		return u.poison(vt)
	}
	return value{u.blk().Ptr.BlockAddr(lb.blk), vt}
}

func sanitize(s string) string {
	b := []byte(s)
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			b[i] = '_'
		}
	}
	return string(b)
}

// ---- return --------------------------------------------------------------

func (u *unit) returnStmt(s *ast.ReturnStmt) {
	f := u.fn
	b := u.blk()

	if s.Result == nil {
		if f.hasSret {
			b.Return()
		} else {
			b.Return()
		}
		u.leave()
		return
	}
	// The block is read again after the operand: lowering it can move the
	// cursor — a conditional, a ||, a statement expression all end the block
	// they started in — and the copy and the return belong wherever the
	// operand left off.
	v := u.expr(s.Result)
	b = u.blk()
	if f.hasSret {
		// The result is written into the caller's storage; the return itself
		// carries nothing, since the signature declares no ret-item.
		r, _ := asRecord(types.Unqualify(f.retTy))
		b.MemCpy(f.sret, u.ptr(v.v, s), b.I64.Const(u.types.layout(r).Size))
		b = u.blk()
		b.Return()
		u.leave()
		return
	}
	if isVoid(types.Unqualify(f.retTy)) {
		u.discard(s.Result)
		b = u.blk()
		b.Return()
		u.leave()
		return
	}
	cv := u.convert(v, f.retTy, s)
	b = u.blk()
	if cv.v == nil {
		b.Trap()
	} else {
		b.Return(cv.v)
	}
	u.leave()
}

// ---- block-scope declarations -------------------------------------------

func (u *unit) localDecl(d ast.Decl) {
	switch d := d.(type) {
	case *ast.GenDecl:
		u.declareEnums(d.Specs)
		sto, _ := specStorage(d.Specs)
		if sto == typedefStorage {
			return
		}
		for _, id := range d.List {
			u.localVar(d, id, sto)
		}
	case *ast.StaticAssertDecl, *ast.EmptyDecl, *ast.BadDecl:
	default:
		u.errorf(d, "internal: %T is not lowered", d)
	}
}

func (u *unit) localVar(d *ast.GenDecl, id *ast.InitDeclarator, sto storage) {
	nameID := id.Decl.DeclName()
	if nameID == nil {
		return
	}
	name := u.name(nameID)
	t := u.typeOf(id)

	if ft, ok := asFunc(t); ok {
		// A block-scope function declaration names a file-scope symbol.
		o := u.top.objs[name]
		if o == nil {
			o = u.emitFuncSymbol(name, ft, externalLinkage, id, u.definesFunc(name))
		}
		u.bind(o)
		return
	}

	switch sto {
	case externStorage:
		o := u.top.objs[name]
		if o == nil {
			g := u.mod.ImportGlobal(u.sym(name), u.types.ftype(t))
			o = &object{name: name, typ: t, decl: id, sto: externStorage,
				link: externalLinkage, sym: g}
			u.top.objs[name] = o
		}
		u.bind(o)
		return

	case staticStorage:
		t = u.completeArray(t, id.Init)
		// Static duration, no linkage: a module symbol with a name nothing
		// outside this function can spell, which is why it is mangled.
		u.fn.statics++
		sym := u.name(u.fn.decl.Name) + "." + name + "." + itoa(u.fn.statics)
		g := u.mod.Global(u.sym(sanitize(sym)), ir.RW, u.types.ftype(t)).Internal()
		if id.Init != nil {
			g.Init(u.staticInit(t, id.Init))
		} else {
			g.Init(ir.ZeroInit)
		}
		if a := u.declAlign(d.Specs, id.Attrs, id); a > 0 {
			g.Align(uint64(a))
		}
		u.bind(&object{name: name, typ: t, decl: id, sto: staticStorage, sym: g})
		return
	}

	u.recordVLAExprs(t, id)
	u.evalVLADims(t, id)
	// Automatic duration.
	if a, ok := asArray(t); ok && (a.Form == types.VLA || a.Form == types.StarArray) {
		u.declareVLA(name, t, id)
		return
	}
	t = u.completeArray(t, id.Init)
	// The declaration's own alignment, where it asked for one stricter
	// than the type's. An aligned vector load reads a frame slot the same
	// way it reads any other address, so this is what keeps one from
	// faulting.
	addr := u.allocaAligned(t, u.declAlign(d.Specs, id.Attrs, id), name, id)
	o := &object{name: name, typ: t, decl: id, sto: sto, addr: addr}
	u.bind(o)
	if id.Init != nil {
		u.initObject(addr, t, id.Init, id)
	}
}

// alloca reserves frame storage for an object of type t.
//
// ptr.alloc is admitted in the entry block only (§19.6), so every automatic
// object's storage is reserved there regardless of which block declares it.
// C's block scoping is a naming rule, not a storage one — the only construct
// whose storage really is scoped is the VLA, which uses ptr.alloca and lives
// in vla.go.
func (u *unit) alloca(t types.Type, name string, at ast.Node) ir.Ptr {
	size := u.sizeof(t, at)
	align := u.alignof(t, at)
	if size == 0 {
		size = 1 // a zero-size object still needs a distinct address
	}
	e := u.fn.entry
	p := e.Ptr.Alloc(uint64(size), uint64(align))
	e.Name(p, sanitize(name))
	return p
}

func (u *unit) allocaBytes(size, align int64, name string) ir.Ptr {
	e := u.fn.entry
	p := e.Ptr.Alloc(uint64(size), uint64(align))
	e.Name(p, sanitize(name))
	return p
}

// allocaAligned reserves storage for an automatic object whose declaration
// asked for an alignment of its own. A want of zero means it asked for none
// and the type's alignment stands; anything weaker than the type's is
// ignored, since §6.7.5 makes _Alignas a floor and not a ceiling.
func (u *unit) allocaAligned(t types.Type, want int64, name string, at ast.Node) ir.Ptr {
	align := u.alignof(t, at)
	if want > align {
		align = want
	}
	size := u.sizeof(t, at)
	if size == 0 {
		size = 1
	}
	return u.allocaBytes(size, align, name)
}
