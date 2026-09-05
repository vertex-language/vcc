package ast

import "reflect"

// Visitor's Visit method is invoked for each node encountered by
// Walk; if it returns a non-nil Visitor, Walk visits each child with
// that visitor.
type Visitor interface {
	Visit(Node) Visitor
}

// Walk traverses the tree rooted at n in depth-first order. Children
// are discovered by reflection over exported fields in source order,
// so new fields traverse without this file changing. Typed nils are
// skipped, as are Span fields and fields tagged `ast:"-"`.
//
// This is the reference implementation; if it shows up in a profile,
// generate the switch and keep this as the oracle.
func Walk(v Visitor, n Node) {
	if n == nil || isNil(n) {
		return
	}
	if v = v.Visit(n); v == nil {
		return
	}
	for _, c := range children(n) {
		Walk(v, c)
	}
}

// Inspect traverses in Walk order, calling f for each node; if f
// returns false the subtree is skipped.
func Inspect(n Node, f func(Node) bool) {
	Walk(inspector(f), n)
}

type inspector func(Node) bool

func (f inspector) Visit(n Node) Visitor {
	if f(n) {
		return f
	}
	return nil
}

var (
	spanType = reflect.TypeOf(Span{})
	nodeType = reflect.TypeOf((*Node)(nil)).Elem()
)

func isNil(n Node) bool {
	v := reflect.ValueOf(n)
	return v.Kind() == reflect.Ptr && v.IsNil()
}

// children collects n's child nodes: exported fields in declaration
// order, slices flattened, Span and `ast:"-"` fields skipped.
func children(n Node) []Node {
	v := reflect.ValueOf(n)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	var out []Node
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() || f.Type == spanType || f.Tag.Get("ast") == "-" {
			continue
		}
		collect(v.Field(i), &out)
	}
	return out
}

func collect(v reflect.Value, out *[]Node) {
	switch v.Kind() {
	case reflect.Slice:
		if v.Type().Elem() == spanType {
			return // StringLit.Segs: segments are extents, not children
		}
		for i := 0; i < v.Len(); i++ {
			collect(v.Index(i), out)
		}
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		collect(v.Elem(), out)
	case reflect.Ptr:
		if v.IsNil() || !v.Type().Implements(nodeType) {
			return // skips File.Unit (*token.File) among others
		}
		*out = append(*out, v.Interface().(Node))
	}
}
