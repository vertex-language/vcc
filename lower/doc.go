// Package lower turns a checked C translation unit into a Vertex IR module.
//
// It is phase 7's second half: the analyzer decided what is legal, types
// decided what each declarator denotes, and lower decides what runs. Input is
// an *ast.File that parsed, the *analyzer.Info produced for it, and a
// types.Model; output is an *ir.Module and the diagnostics only code
// generation can raise.
//
// What lower owns, because nothing below it does:
//
//   - Expression typing. analyzer.Info records the types of declaring nodes
//     and the values of required constant expressions; the type of `a + b` is
//     computed here, alongside the conversions §6.3 requires.
//   - Ordinary-identifier resolution. Info has no use-to-declaration map, so
//     lower rebuilds object scopes in source order as it walks. Tags need no
//     scope here: a tag in type position was already resolved into the
//     types.Type that Info holds.
//   - Record layout. Byte offsets, bit-field placement, and padding, computed
//     from the same types.Model that answers sizeof.
//   - Initializer shape. Brace elision, designators, and the flattening of a
//     static initializer into an ir.Init tree.
//
// What lower does not own: anything about a machine. No register is physical,
// no address is known, and the only target facts consulted are the ones in
// types.Model and in the module's ir.Layout. Instruction selection, register
// allocation, and the meaning of a calling convention are ir/lower's, one
// repository down.
//
// Two passes, because C admits forward reference at file scope and Go does
// not admit patching an ir.Symbol into existence twice. Pass one declares
// every external object and function, so that a body may name a symbol
// defined below it. Pass two emits bodies and initializers.
//
// Errors are reported, never panicked: a translation unit that reached lower
// parsed and checked, so a fault here is a fault in lower. The module is
// always non-nil and always safe to hand to verify.Module, which is the
// authority on whether it is sound.
package lower
