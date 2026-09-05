package token

// Kind identifies the lexical class of a token.
type Kind uint8

const (
	ILLEGAL Kind = iota
	EOF
	COMMENT

	IDENT

	literal_beg
	INT_LIT
	FLOAT_LIT
	CHAR_LIT
	STRING_LIT
	literal_end

	punct_beg
	LBRACK // [
	RBRACK // ]
	LPAREN // (
	RPAREN // )
	LBRACE // {
	RBRACE // }

	PERIOD // .
	ARROW  // ->
	INC    // ++
	DEC    // --

	AND   // &
	MUL   // *
	ADD   // +
	SUB   // -
	TILDE // ~
	NOT   // !

	QUO  // /
	REM  // %
	SHL  //
	SHR  // >>
	LSS  //
	GTR  // >
	LEQ  // <=
	GEQ  // >=
	EQL  // ==
	NEQ  // !=
	XOR  // ^
	OR   // |
	LAND // &&
	LOR  // ||

	QUESTION // ?
	COLON    // :
	SEMI     // ;
	ELLIPSIS // ...

	ASSIGN     // =
	MUL_ASSIGN // *=
	QUO_ASSIGN // /=
	REM_ASSIGN // %=
	ADD_ASSIGN // +=
	SUB_ASSIGN // -=
	SHL_ASSIGN // <<=
	SHR_ASSIGN // >>=
	AND_ASSIGN // &=
	XOR_ASSIGN // ^=
	OR_ASSIGN  // |=

	COMMA    // ,
	HASH     // #
	HASHHASH // ##
	punct_end

	// The 44 keywords of §6.4.1. _Imaginary is reserved even in
	// implementations without Annex G.
	keyword_beg
	AUTO
	BREAK
	CASE
	CHAR
	CONST
	CONTINUE
	DEFAULT
	DO
	DOUBLE
	ELSE
	ENUM
	EXTERN
	FLOAT
	FOR
	GOTO
	IF
	INLINE
	INT
	LONG
	REGISTER
	RESTRICT
	RETURN
	SHORT
	SIGNED
	SIZEOF
	STATIC
	STRUCT
	SWITCH
	TYPEDEF
	UNION
	UNSIGNED
	VOID
	VOLATILE
	WHILE
	ALIGNAS       // _Alignas
	ALIGNOF       // _Alignof
	ATOMIC        // _Atomic
	BOOL          // _Bool
	COMPLEX       // _Complex
	GENERIC       // _Generic
	IMAGINARY     // _Imaginary
	NORETURN      // _Noreturn
	STATIC_ASSERT // _Static_assert
	THREAD_LOCAL  // _Thread_local
	std_keyword_end

	// Extension keywords. These are not in §6.4.1, but they are spelled
	// in the reserved __ namespace, so recognizing them takes nothing
	// away from a conforming program.
	//
	// __int128 is a type specifier and not a typedef name, which is the
	// distinction that matters: a specifier combines with signed and
	// unsigned (`unsigned __int128`) and refuses int, and a typedef name
	// could do neither. The __int128_t and __uint128_t spellings really
	// are typedefs and stay predeclared in the analyzer.
	//
	// __auto_type is a specifier whose type is not written: it is the
	// initializer's, so unlike every other one here it cannot be resolved
	// from the specifier list alone. See Spec.Auto.
	INT128    // __int128
	INT64     // __int64
	INT32     // __int32
	INT16     // __int16
	INT8      // __int8
	AUTO_TYPE // __auto_type
	TRY       // __try
	EXCEPT    // __except
	FINALLY   // __finally
	LEAVE     // __leave
	keyword_end
)

var names = [...]string{
	ILLEGAL: "ILLEGAL",
	EOF:     "EOF",
	COMMENT: "COMMENT",

	IDENT:      "IDENT",
	INT_LIT:    "INT_LIT",
	FLOAT_LIT:  "FLOAT_LIT",
	CHAR_LIT:   "CHAR_LIT",
	STRING_LIT: "STRING_LIT",

	LBRACK: "[",
	RBRACK: "]",
	LPAREN: "(",
	RPAREN: ")",
	LBRACE: "{",
	RBRACE: "}",

	PERIOD: ".",
	ARROW:  "->",
	INC:    "++",
	DEC:    "--",

	AND:   "&",
	MUL:   "*",
	ADD:   "+",
	SUB:   "-",
	TILDE: "~",
	NOT:   "!",

	QUO:  "/",
	REM:  "%",
	SHL:  "<<",
	SHR:  ">>",
	LSS:  "<",
	GTR:  ">",
	LEQ:  "<=",
	GEQ:  ">=",
	EQL:  "==",
	NEQ:  "!=",
	XOR:  "^",
	OR:   "|",
	LAND: "&&",
	LOR:  "||",

	QUESTION: "?",
	COLON:    ":",
	SEMI:     ";",
	ELLIPSIS: "...",

	ASSIGN:     "=",
	MUL_ASSIGN: "*=",
	QUO_ASSIGN: "/=",
	REM_ASSIGN: "%=",
	ADD_ASSIGN: "+=",
	SUB_ASSIGN: "-=",
	SHL_ASSIGN: "<<=",
	SHR_ASSIGN: ">>=",
	AND_ASSIGN: "&=",
	XOR_ASSIGN: "^=",
	OR_ASSIGN:  "|=",

	COMMA:    ",",
	HASH:     "#",
	HASHHASH: "##",

	AUTO:          "auto",
	BREAK:         "break",
	CASE:          "case",
	CHAR:          "char",
	CONST:         "const",
	CONTINUE:      "continue",
	DEFAULT:       "default",
	DO:            "do",
	DOUBLE:        "double",
	ELSE:          "else",
	ENUM:          "enum",
	EXTERN:        "extern",
	FLOAT:         "float",
	FOR:           "for",
	GOTO:          "goto",
	IF:            "if",
	INLINE:        "inline",
	INT:           "int",
	LONG:          "long",
	REGISTER:      "register",
	RESTRICT:      "restrict",
	RETURN:        "return",
	SHORT:         "short",
	SIGNED:        "signed",
	SIZEOF:        "sizeof",
	STATIC:        "static",
	STRUCT:        "struct",
	SWITCH:        "switch",
	TYPEDEF:       "typedef",
	UNION:         "union",
	UNSIGNED:      "unsigned",
	VOID:          "void",
	VOLATILE:      "volatile",
	WHILE:         "while",
	ALIGNAS:       "_Alignas",
	ALIGNOF:       "_Alignof",
	ATOMIC:        "_Atomic",
	BOOL:          "_Bool",
	COMPLEX:       "_Complex",
	GENERIC:       "_Generic",
	IMAGINARY:     "_Imaginary",
	NORETURN:      "_Noreturn",
	STATIC_ASSERT: "_Static_assert",
	THREAD_LOCAL:  "_Thread_local",
	INT128:        "__int128",
	INT64:         "__int64",
	INT32:         "__int32",
	INT16:         "__int16",
	INT8:          "__int8",
	AUTO_TYPE:     "__auto_type",
	TRY:           "__try",
	EXCEPT:        "__except",
	FINALLY:       "__finally",
	LEAVE:         "__leave",
}

// String returns the keyword or punctuator spelling, or the class name
// for kinds with no fixed spelling (IDENT, INT_LIT, …).
func (k Kind) String() string {
	if int(k) < len(names) && names[k] != "" {
		return names[k]
	}
	return "Kind(" + itoa(int(k)) + ")"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

var keywords = func() map[string]Kind {
	m := make(map[string]Kind, keyword_end-keyword_beg-2)
	for k := keyword_beg + 1; k < keyword_end; k++ {
		if k == std_keyword_end { // the marker between the two blocks, not a keyword
			continue
		}
		m[names[k]] = k
	}
	return m
}()

// aliases are extension spellings of a keyword already in the table: the
// same operator under another name, in the reserved __ namespace, so
// recognizing one takes nothing away from a conforming program.
//
// They are not the tolerated spellings of parser/decl.go, which are parsed
// and discarded. An alias means what the keyword means — __alignof(T) is
// _Alignof(T) — so it resolves here, at the one place a spelling becomes a
// kind, rather than at each place the kind is handled. Like that table,
// this one grows on evidence from real system headers.
var aliases = map[string]Kind{
	// MSVC's name for _Alignof, and gcc's. The Windows SDK's winioctl.h
	// rounds a DSM range up to __alignof(DEVICE_DSM_RANGE), reached from
	// <windows.h>. The operand is a type name, as _Alignof's is.
	"__alignof":   ALIGNOF,
	"__alignof__": ALIGNOF,

	// gcc's name for _Thread_local, which predates it and which every
	// header that wants the storage class from a gcc still writes. vcc says
	// __GNUC__ 4, and stb_image.h reads that as "older than 5, use
	// __thread" — so the spelling is the one such code reaches for, and the
	// storage class behind it is the one vcc already implements.
	"__thread": THREAD_LOCAL,
}

// Lookup maps an identifier spelling to its keyword kind, or IDENT.
// Typedef-ness is the parser's call, not Lookup's.
func Lookup(name string) Kind {
	if k, ok := keywords[name]; ok {
		return k
	}
	if k, ok := aliases[name]; ok {
		return k
	}
	return IDENT
}

func (k Kind) IsLiteral() bool { return literal_beg < k && k < literal_end }
func (k Kind) IsPunct() bool   { return punct_beg < k && k < punct_end }
func (k Kind) IsKeyword() bool { return keyword_beg < k && k < keyword_end }

// Operator precedence for the ten binary levels of §6.5, plus COMMA
// below them. Assignment and ?: are right-associative and not driven
// by this table.
const (
	LowestPrec  = 0 // non-binary operators
	HighestPrec = 11
)

func (k Kind) Precedence() int {
	switch k {
	case COMMA:
		return 1
	case LOR:
		return 2
	case LAND:
		return 3
	case OR:
		return 4
	case XOR:
		return 5
	case AND:
		return 6
	case EQL, NEQ:
		return 7
	case LSS, GTR, LEQ, GEQ:
		return 8
	case SHL, SHR:
		return 9
	case ADD, SUB:
		return 10
	case MUL, QUO, REM:
		return 11
	}
	return LowestPrec
}
