package vcc

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/vertex-language/vcc/preprocessor"
	"github.com/vertex-language/vcc/token"
)

// An Input is one thing to compile or to link.
//
// A path is only the common case. Data lets a caller compile source it already has
// in memory — a test, a playground, a generator — without writing a temporary
// file, and lets an object this process just produced reach the linker without
// touching the disk at all.
//
// What an Input is for is decided by its name: .c and .i are compiled,
// everything else is handed to the linker. That is the same rule the command
// line has always used, and it is what lets one ordered list hold sources and
// objects together — which a static link requires, because its order is the
// caller's and reordering it would be vcc deciding something the caller said.
type Input struct {
	// Name is the path this input is known by. It is what diagnostics print,
	// what decides whether the input is source, and what the linker records.
	// "", "-" and "<stdin>" all name standard input.
	Name string

	// Data is the bytes. Nil means read Name from disk.
	Data []byte

	// FS is where a quoted #include beside this input resolves. Nil uses the
	// directory Name sits in — and nothing at all when the input has no
	// directory, which is what standard input has always searched.
	FS fs.FS
}

// File is an input read from disk.
func File(path string) Input { return Input{Name: path} }

// Text is source already in memory.
func Text(name string, data []byte) Input { return Input{Name: name, Data: data} }

// ObjectBytes is an object file already in memory — from Object, from a build
// cache, from another compiler. It is Text under the name that says what the
// caller meant; the extension in name is what decides how it is treated.
func ObjectBytes(name string, data []byte) Input { return Input{Name: name, Data: data} }

// name is what a diagnostic calls this input.
func (in Input) name() string {
	if in.Name == "" || in.Name == "-" {
		return "<stdin>"
	}
	return in.Name
}

// isSource reports whether vcc compiles this input rather than handing it to
// the linker. Standard input is source; so is anything vcc's own front end
// reads. Everything else is the linker's business.
func (in Input) isSource() bool {
	switch strings.ToLower(filepath.Ext(in.Name)) {
	case ".c", ".i":
		return true
	}
	return in.name() == "<stdin>"
}

// preprocessed reports whether this input goes through phase 4 under pp. A .c
// file does, a .i file does not, and an unnamed input does not unless asked:
// a pipe has no extension, which is the whole reason the override exists.
func (in Input) preprocessed(pp Tristate) bool {
	switch pp {
	case PPAlways:
		return true
	case PPNever:
		return false
	}
	return filepath.Ext(in.Name) == ".c"
}

// moduleName is the module the IR opens with: the file's stem, so a module
// built from foo.c is named foo. Standard input has no stem, so lower's own
// default applies there.
func (in Input) moduleName() string {
	if in.name() == "<stdin>" {
		return ""
	}
	base := filepath.Base(in.Name)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// bytes is the input's contents, read from disk if it carries none.
func (in Input) bytes() ([]byte, error) {
	if in.Data != nil {
		return in.Data, nil
	}
	if in.name() == "<stdin>" {
		return nil, &fs.PathError{Op: "open", Path: "<stdin>", Err: fs.ErrInvalid}
	}
	return os.ReadFile(in.Name)
}

// load reads the input into the position space its diagnostics resolve
// through.
func (in Input) load() (*token.File, error) {
	src, err := in.bytes()
	if err != nil {
		return nil, err
	}
	return token.NewFile(in.name(), src), nil
}

// mount is the directory an input was read from, as the mount a quoted
// #include in it searches first (§6.10.2p3). An input with no directory gets
// the zero mount, which searches nothing extra.
func (in Input) mount() preprocessor.Mount {
	if in.FS != nil {
		return preprocessor.Mount{Name: ".", FS: in.FS}
	}
	if in.name() == "<stdin>" {
		return preprocessor.Mount{}
	}
	dir := filepath.Dir(in.Name)
	if dir == "" {
		dir = "."
	}
	return preprocessor.Mount{Name: dir, FS: os.DirFS(dir)}
}
