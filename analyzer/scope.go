package analyzer

import (
	"github.com/vertex-language/vcc/ast"
	"github.com/vertex-language/vcc/types"
)

// C's namespaces (§6.2.3): ordinary identifiers, tags, labels.
// Members live on their Record; labels live on the checker per
// function. This file owns the first two.

type symKind uint8

const (
	symObject symKind = iota
	symFunc
	symTypedef
	symEnumConst
)

func (k symKind) String() string {
	switch k {
	case symFunc:
		return "function"
	case symTypedef:
		return "typedef"
	case symEnumConst:
		return "enumeration constant"
	}
	return "object"
}

type symbol struct {
	kind   symKind
	typ    types.Type
	node   ast.Node
	extern bool  // file scope or declared extern: relinkable
	value  int64 // enum constants
}

type tagsym struct {
	typ  types.Type // *types.Record or *types.Enum
	node ast.Node
}

type scope struct {
	ordinary map[string]*symbol
	tags     map[string]*tagsym
}

func (c *checker) push() {
	c.scopes = append(c.scopes, &scope{
		ordinary: map[string]*symbol{},
		tags:     map[string]*tagsym{},
	})
}

func (c *checker) pop() { c.scopes = c.scopes[:len(c.scopes)-1] }

func (c *checker) fileScope() bool { return len(c.scopes) == 1 }

func (c *checker) lookup(name string) *symbol {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if s, ok := c.scopes[i].ordinary[name]; ok {
			return s
		}
	}
	return nil
}

// declare enters a name in the innermost scope. Same-scope
// redeclaration is permitted only where C permits it: declarations
// with linkage (file scope, or extern), and typedefs redeclaring
// typedefs (C11 allows compatible redefinition; compatibility is
// deferred with the rest of type compatibility).
func (c *checker) declare(id *ast.Ident, s *symbol) {
	name := c.name(id)
	cur := c.scopes[len(c.scopes)-1]
	if prev, ok := cur.ordinary[name]; ok {
		switch {
		case prev.kind != s.kind:
			c.report(id, "'"+name+"' redeclared as a different kind of symbol (was "+prev.kind.String()+")")
			return
		case prev.kind == symEnumConst:
			c.report(id, "enumeration constant '"+name+"' redeclared")
			return
		case prev.kind == symTypedef, prev.extern && s.extern:
			// permitted; keep the first
			return
		default:
			c.report(id, "'"+name+"' redeclared in the same scope")
			return
		}
	}
	cur.ordinary[name] = s
}

// lookupTag searches all scopes; declareTag enters in the innermost.
func (c *checker) lookupTag(name string) *tagsym {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if t, ok := c.scopes[i].tags[name]; ok {
			return t
		}
	}
	return nil
}

func (c *checker) currentTag(name string) *tagsym {
	t, _ := c.scopes[len(c.scopes)-1].tags[name]
	return t
}

func (c *checker) declareTag(name string, t *tagsym) {
	c.scopes[len(c.scopes)-1].tags[name] = t
}
