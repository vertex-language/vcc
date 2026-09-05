package lower

import (
	"strconv"
	"strings"

	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/analyzer"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/types"
)

// String literals become read-only globals with internal linkage, pooled by
// content.
//
// Pooling is by decoded content rather than by spelling, so that "a\tb" and
// "a\011b" share one object. §6.4.5p7 leaves it unspecified whether identical
// literals are distinct objects, which is the licence to pool at all; vcc
// pools always, because a program that can tell has already relied on the
// unspecified case.

// stringLit emits a string literal as the address of its object.
func (u *unit) stringLit(s *ast.StringLit) value {
	sv := u.decodeString(s)
	sym := u.stringSymbol(sv)
	t := u.stringType(s)
	if u.fn == nil {
		return value{nil, t}
	}
	return value{u.blk().Ptr.GetAddr(sym), t}
}

// stringType is the literal's C type: an array of its element type, one
// longer than the characters written, since Data includes the terminator.
func (u *unit) stringType(s *ast.StringLit) types.Type {
	sv := u.decodeString(s)
	return &types.Array{
		Elem: types.Qualify(sv.Elem, types.QConst),
		Form: types.FixedArray,
		Len:  int64(len(sv.Data)),
	}
}

func (u *unit) decodeString(s *ast.StringLit) analyzer.StringValue {
	if sv, ok := u.strCache[s]; ok {
		return sv
	}
	sv := analyzer.DecodeString(u.src, s, u.model,
		func(msg string) { u.errorf(s, "%s", msg) })
	u.strCache[s] = sv
	return sv
}

// stringSymbol interns one decoded run as a module global.
func (u *unit) stringSymbol(sv analyzer.StringValue) ir.Symbol {
	key := u.stringKey(sv)
	if s, ok := u.strs[key]; ok {
		return s
	}
	esz, _ := u.model.Sizeof(sv.Elem)
	if esz < 1 {
		esz = 1
	}
	st, ok := u.types.storeType(sv.Elem)
	if !ok {
		st = ir.StoreI8
	}
	ft := ir.Array(uint64(len(sv.Data)), st.FType())

	g := u.mod.Global(u.sym(u.uniq("str")), ir.RO, ft).Internal()
	if esz == 1 {
		var b strings.Builder
		for _, c := range sv.Data {
			b.WriteByte(byte(c))
		}
		g.Init(ir.Str(b.String()))
	} else {
		items := make([]ir.Init, len(sv.Data))
		for i, c := range sv.Data {
			items[i] = ir.Lit(ir.Uint(uint64(c)))
		}
		g.Init(ir.List(items...))
	}
	u.strs[key] = g
	return g
}

func (u *unit) stringKey(sv analyzer.StringValue) string {
	var b strings.Builder
	b.WriteString(sv.Elem.String())
	b.WriteByte(':')
	for _, c := range sv.Data {
		b.WriteString(strconv.FormatUint(uint64(c), 16))
		b.WriteByte(',')
	}
	return b.String()
}

// bindFuncName gives the body §6.4.2.2's __func__, and gcc's two spellings of
// it, as an object rather than as a macro: the standard says "as if the
// declaration `static const char __func__[] = "function-name";` appeared",
// and a program may take its address or pass it on.
//
// All three names are one object. They are pooled with the string literals,
// so a function whose name also appears as a literal contributes one copy.
func (u *unit) bindFuncName(name string) {
	sv := analyzer.StringValue{Elem: types.Typ(types.Char)}
	for i := 0; i < len(name); i++ {
		sv.Data = append(sv.Data, uint32(name[i]))
	}
	sv.Data = append(sv.Data, 0)
	sym := u.stringSymbol(sv)
	t := types.Qualify(&types.Array{
		Elem: types.Typ(types.Char),
		Form: types.FixedArray,
		Len:  int64(len(sv.Data)),
	}, types.QConst)
	for _, spelling := range [...]string{"__func__", "__FUNCTION__", "__PRETTY_FUNCTION__"} {
		u.bind(&object{name: spelling, typ: t, sto: staticStorage, link: internalLinkage, sym: sym})
	}
}
