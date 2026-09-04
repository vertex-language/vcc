package lower

import (
	"strings"

	"github.com/vertex-language/ir"
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/token"
	"github.com/vertex-language/vcc/types"
)

// storage is the storage class as written, after the defaults §6.2.2 supplies.
type storage uint8

const (
	autoStorage storage = iota
	registerStorage
	staticStorage
	externStorage
	typedefStorage
)

// linkage is §6.2.2's linkage, which decides whether a name is a module-scope
// ir symbol and whether that symbol is exported.
type linkage uint8

const (
	noLinkage linkage = iota
	internalLinkage
	externalLinkage
)

// object is one binding in the ordinary-identifier namespace.
//
// Exactly one home is set. A static-duration object lives at sym and is
// reached by ptr.getaddr; an automatic one lives at addr, the pointer its
// alloca returned; an enumeration constant has no home at all, only val. The
// distinction is the whole reason lower keeps scopes: an identifier in
// expression position must know which of the three it is before it knows its
// type.
type object struct {
	name string
	typ  types.Type
	decl ast.Node
	sto  storage
	link linkage

	sym    ir.Symbol // static duration: global, global import, function, alias
	call   ir.Callee // set alongside sym for a function or function import
	addr   ir.Ptr    // automatic duration: the alloca holding the object
	val    int64     // enumeration constant
	isEnum bool

	// pending marks a name this unit imports but has not yet created a
	// module item for. A declaration is not a reference: <stdio.h> declares
	// several hundred functions and a unit that calls two of them must not
	// carry the rest into its symbol table. Emitting them all makes every
	// object file an undefined reference to the whole C library, and the
	// link then fails on the first name the platform does not actually
	// export — _alloca, which is a compiler builtin and lives in no library.
	//
	// So the import is created at the first use, by imported, and a
	// declaration that is never used creates nothing. align and tls are the
	// attributes that would otherwise have been applied at creation.
	pending bool
	align   int64
	tls     bool

	// sig is the ir signature of a function this unit defines, built where
	// the name is declared rather than where the body is walked. An ir.Func's
	// arity is whatever its Param calls have added so far, so a call emitted
	// before the callee's body has been walked sees a signature with no
	// parameters at all — which is every forward-declared static and every
	// pair of mutually recursive functions. Building it in pass one makes the
	// shape a property of the declaration, which is what C says it is.
	sig *funcSig

	// vlaSize is the runtime element count of a variably modified type,
	// evaluated once at the declaration and kept for sizeof and for pointer
	// arithmetic. Zero for everything else.
	vlaSize ir.I64
}

func (o *object) isStatic() bool { return o.sym != nil || o.pending }

// imported returns the module item for a static-duration object, creating a
// pending import the first time one is asked for. It returns nil for an
// object with no static home at all, so callers keep testing the result
// rather than the field.
func (u *unit) imported(o *object) ir.Symbol {
	if o == nil {
		return nil
	}
	if o.sym == nil && o.pending {
		o.pending = false
		if ft, ok := types.Unqualify(o.typ).(*types.Func); ok {
			sig := u.types.sig(ft)
			if u.msSetjmp(o.name) {
				sig.Param(ir.TypePtr)
			}
			imp := u.mod.ImportFunc(u.sym(o.name), sig)
			o.sym, o.call = imp, imp
		} else {
			g := u.mod.ImportGlobal(u.sym(o.name), u.types.ftype(o.typ))
			if o.align > 0 {
				g.Align(uint64(o.align))
			}
			if o.tls {
				g.TLSModel(ir.GlobalDynamic)
			}
			o.sym = g
		}
	}
	return o.sym
}

// callable returns the callee for a function name, creating a pending import
// on the way, and nil for anything that is not directly callable.
func (u *unit) callable(o *object) ir.Callee {
	if o == nil {
		return nil
	}
	u.imported(o)
	return o.call
}

// funcSig is a function's ir shape: the parameter values its body will bind,
// and the hidden pointer an aggregate return travels through.
type funcSig struct {
	params    []ir.Value
	sretParam ir.Ptr
	hasSret   bool
}

type scope struct {
	parent *scope
	objs   map[string]*object
}

func newScope(parent *scope) *scope {
	return &scope{parent: parent, objs: map[string]*object{}}
}

func (u *unit) push() *scope {
	u.scope = newScope(u.scope)
	return u.scope
}

func (u *unit) pop() {
	if u.scope.parent != nil {
		u.scope = u.scope.parent
	}
}

// bind enters o in the innermost scope.
//
// A redeclaration at the same scope is not reported here: §6.7p3 and §6.9p5
// are the analyzer's, and by the time lower runs the only redeclarations left
// are the legal ones — a tentative definition completed, an extern
// redeclared. The later binding wins, which is what "completed" means.
func (u *unit) bind(o *object) {
	u.scope.objs[o.name] = o
}

// lookup resolves an ordinary identifier outward from the innermost scope.
func (u *unit) lookup(name string) *object {
	for s := u.scope; s != nil; s = s.parent {
		if o, ok := s.objs[name]; ok {
			return o
		}
	}
	return nil
}

// resolve binds an identifier appearing in expression position.
//
// A miss is possible on input the analyzer accepted only in the sense that
// error recovery produced an Ident where no declaration exists; it reports and
// yields nil, and the caller emits a poison value rather than guessing.
func (u *unit) resolve(id *ast.Ident) *object {
	name := u.name(id)
	if o := u.lookup(name); o != nil {
		return o
	}
	u.errorf(id, "internal: %s was not bound by any declaration lower walked", name)
	return nil
}

// specStorage reads the storage class off a specifier list.
//
// This is syntax, not analysis: the analyzer already enforced §6.7.1's
// one-storage-class rule, so the first keyword found is the only one. Reading
// it here rather than through types.BuildSpecs avoids running construction —
// and its diagnostics — a second time over a list that was already checked.
func specStorage(specs ast.DeclSpecs) (storage, bool) {
	for _, s := range specs {
		k, ok := s.(*ast.KeywordSpec)
		if !ok {
			continue
		}
		switch k.Kind {
		case token.TYPEDEF:
			return typedefStorage, true
		case token.EXTERN:
			return externStorage, true
		case token.STATIC:
			return staticStorage, true
		case token.AUTO:
			return autoStorage, true
		case token.REGISTER:
			return registerStorage, true
		}
	}
	return autoStorage, false
}

// specHas reports whether a bare keyword appears in a specifier list.
func specHas(specs ast.DeclSpecs, k token.Kind) bool {
	for _, s := range specs {
		if ks, ok := s.(*ast.KeywordSpec); ok && ks.Kind == k {
			return true
		}
	}
	return false
}

// fileLinkage applies §6.2.2p3–5 at file scope.
func fileLinkage(sto storage, explicit bool) linkage {
	if sto == staticStorage {
		return internalLinkage
	}
	return externalLinkage
}

// blockLinkage applies §6.2.2p6 at block scope: only extern gives linkage, and
// a static block-scope object has static duration with no linkage — a module
// symbol nothing outside the function may name.
func blockLinkage(sto storage) linkage {
	if sto == externStorage {
		return externalLinkage
	}
	return noLinkage
}

// declaresTLS reports whether a declaration says thread-local, in either
// spelling.
//
// C11 writes _Thread_local, which is a keyword and a storage-class
// specifier. MSVC writes __declspec(thread), which reaches here as an
// attribute because that is what a __declspec is parsed into — so it has to
// be looked for in the other list, and a compiler that looks in one place
// gives the second spelling an ordinary global. That is the worst kind of
// wrong answer: the program compiles, runs, and shares between threads a
// variable whose whole point was not to be shared.
func (u *unit) declaresTLS(specs ast.DeclSpecs) bool {
	if specHas(specs, token.THREAD_LOCAL) {
		return true
	}
	for _, s := range specs {
		as, ok := s.(*ast.AttrSpec)
		if !ok {
			continue
		}
		for _, a := range as.Attrs {
			if a.Name != nil && strings.Trim(u.name(a.Name), "_") == "thread" {
				return true
			}
		}
	}
	return false
}
