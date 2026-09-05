package ast

import (
	"fmt"
	"io"
	"reflect"

	"github.com/vertex-language/vcc/token"
)

// Fdump prints the tree rooted at n, one node per line, indented by
// depth. Identifiers and literals appear with their resolved text,
// and every node with its position as raw line:column — so dumps
// line up with what the user typed, even through trigraphs and
// splices.
func Fdump(w io.Writer, f *token.File, n Node) error {
	d := &dumper{w: w, f: f}
	d.dump(n, 0)
	return d.err
}

type dumper struct {
	w   io.Writer
	f   *token.File
	err error
}

func (d *dumper) printf(format string, args ...any) {
	if d.err == nil {
		_, d.err = fmt.Fprintf(d.w, format, args...)
	}
}

func (d *dumper) dump(n Node, depth int) {
	if d.err != nil || n == nil || isNil(n) {
		return
	}
	for i := 0; i < depth; i++ {
		d.printf("  ")
	}
	p := d.f.Position(n.Pos())
	d.printf("%s %d:%d", nodeName(n), p.Line, p.Column)

	switch n := n.(type) {
	case *Ident:
		d.printf(" %s", n.Name(d.f))
	case *BasicLit:
		d.printf(" %s %s", n.Kind, d.f.Slice(n.Lo, n.Hi))
	case *StringLit:
		for _, seg := range n.Segs {
			d.printf(" %s", d.f.Slice(seg.Lo, seg.Hi))
		}
	case *KeywordSpec:
		d.printf(" %s", n.Kind)
	case *BinaryExpr:
		d.printf(" %s", n.Op)
	case *AssignExpr:
		d.printf(" %s", n.Op)
	case *UnaryExpr:
		d.printf(" %s", n.Op)
	case *IncDecExpr:
		d.printf(" %s (postfix)", n.Op)
	case *SelectorExpr:
		d.printf(" %s", n.Op)
	case *StructType:
		d.printf(" %s", n.Kind)
	case *CaseStmt:
		d.printf(" %s", n.Kind)
	}
	d.printf("\n")

	for _, c := range children(n) {
		d.dump(c, depth+1)
	}
}

func nodeName(n Node) string {
	t := reflect.TypeOf(n)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}
