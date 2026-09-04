# C Language Grammar (C11 Standard)

This document presents the grammar for the C programming language
(ISO/IEC 9899:2011), expressed using the notation of the Java Language
Specification (JLS) — a BNF variant chosen for being shorter and easier
to read than other common styles.

**Scope:** This covers the C language only, as defined by C11 (with C17
being grammatically identical). C++ is entirely out of scope, as are
compiler-specific extensions (e.g. GNU C) and later standard revisions
such as C23. The preprocessing grammar (Annex A.3) is also omitted; this
document describes the post-preprocessing language.

## Notation

```text
Nonterminal:
    alternative one
    alternative two
```

* **CamelCase** words are nonterminals. Everything else — lowercase keywords, punctuation, operators — is a terminal, to appear exactly as written.
* `'{'` / `'['` — When braces or brackets are enclosed in single quotes, they represent literal C punctuator terminals, overriding the JLS meta-syntax.
* `{x}` — zero or more occurrences of `x`.
* `[x]` — zero or one occurrence of `x`.
* `(one of)` — each symbol on the following lines is a separate alternative.
* `but not` — excludes the named expansions.
* A phrase in *(italics-by-parenthesis)* defines a nonterminal narratively where enumeration is impractical.

---

## Lexical Structure

### Tokens

```text
Token:
    Keyword
    Identifier
    Constant
    StringLiteral
    Punctuator
```

### Keywords

```text
Keyword: (one of)
    auto break case char const continue default do double else enum extern float for goto if inline int long register restrict return short signed sizeof static struct switch typedef union unsigned void volatile while _Alignas _Alignof _Atomic _Bool _Complex _Generic _Imaginary _Noreturn _Static_assert _Thread_local
```

> Note: `_Imaginary` is a reserved keyword but is not a type specifier in
> the core language; it is only used as a type specifier by
> implementations supporting Annex G (IEC 60559 complex arithmetic).

### Identifiers

```text
Identifier:
    IdentifierNondigit
    Identifier IdentifierNondigit
    Identifier Digit

IdentifierNondigit:
    Nondigit
    UniversalCharacterName
    (other implementation-defined characters)

Nondigit: (one of)
    _ a b c d e f g h i j k l m n o p q r s t u v w x y z
    A B C D E F G H I J K L M N O P Q R S T U V W X Y Z

Digit: (one of)
    0 1 2 3 4 5 6 7 8 9
```

### Constants

```text
Constant:
    IntegerConstant
    FloatingConstant
    EnumerationConstant
    CharacterConstant

IntegerConstant:
    DecimalConstant [IntegerSuffix]
    OctalConstant [IntegerSuffix]
    HexadecimalConstant [IntegerSuffix]

DecimalConstant:
    NonzeroDigit {Digit}

OctalConstant:
    0 {OctalDigit}

HexadecimalConstant:
    HexPrefix HexadecimalDigit {HexadecimalDigit}

HexPrefix: (one of)
    0x 0X

NonzeroDigit: (one of)
    1 2 3 4 5 6 7 8 9

OctalDigit: (one of)
    0 1 2 3 4 5 6 7

HexadecimalDigit: (one of)
    0 1 2 3 4 5 6 7 8 9 a b c d e f A B C D E F

IntegerSuffix:
    UnsignedSuffix [LongSuffix]
    UnsignedSuffix LongLongSuffix
    LongSuffix [UnsignedSuffix]
    LongLongSuffix [UnsignedSuffix]

UnsignedSuffix: (one of)
    u U

LongSuffix: (one of)
    l L

LongLongSuffix: (one of)
    ll LL

FloatingConstant:
    DecimalFloatingConstant
    HexadecimalFloatingConstant

DecimalFloatingConstant:
    FractionalConstant [ExponentPart] [FloatingSuffix]
    DigitSequence ExponentPart [FloatingSuffix]

HexadecimalFloatingConstant:
    HexPrefix HexFractionalConstant BinaryExponentPart [FloatingSuffix]
    HexPrefix HexDigitSequence BinaryExponentPart [FloatingSuffix]

FractionalConstant:
    [DigitSequence] . DigitSequence
    DigitSequence .

ExponentPart:
    e [Sign] DigitSequence
    E [Sign] DigitSequence

Sign: (one of)
    + -

DigitSequence:
    Digit {Digit}

HexFractionalConstant:
    [HexDigitSequence] . HexDigitSequence
    HexDigitSequence .

BinaryExponentPart:
    p [Sign] DigitSequence
    P [Sign] DigitSequence

HexDigitSequence:
    HexadecimalDigit {HexadecimalDigit}

FloatingSuffix: (one of)
    f l F L

EnumerationConstant:
    Identifier

CharacterConstant:
    ' CChar {CChar} '
    L ' CChar {CChar} '
    u ' CChar {CChar} '
    U ' CChar {CChar} '

CChar:
    (any member of the source character set except the single-quote ', backslash \, or new-line character)
    EscapeSequence

EscapeSequence:
    SimpleEscapeSequence
    OctalEscapeSequence
    HexadecimalEscapeSequence
    UniversalCharacterName

SimpleEscapeSequence: (one of)
    \' \" \? \\ \a \b \f \n \r \t \v

OctalEscapeSequence:
    \ OctalDigit [OctalDigit] [OctalDigit]

HexadecimalEscapeSequence:
    \x HexadecimalDigit {HexadecimalDigit}
```

> Note: a character constant requires at least one `CChar` — the empty
> character constant `''` is not valid C. String literals, by contrast,
> may be empty.

### String Literals

```text
StringLiteral:
    " {SChar} "
    u8 " {SChar} "
    u " {SChar} "
    U " {SChar} "
    L " {SChar} "

SChar:
    (any member of the source character set except the double-quote ", backslash \, or new-line character)
    EscapeSequence

UniversalCharacterName:
    \u HexDigit HexDigit HexDigit HexDigit
    \U HexDigit HexDigit HexDigit HexDigit HexDigit HexDigit HexDigit HexDigit

HexDigit:
    HexadecimalDigit
```

### Punctuators

```text
Punctuator: (one of)
    [ ] ( ) { } . ->
    ++ -- & * + - ~ !
    / % << >> < > <= >= == != ^ | && ||
    ? : ; ...
    = *= /= %= += -= <<= >>= &= ^= |=
    , # ##
    <: :> <% %> %: %:%:
```

---

## Expressions

```text
PrimaryExpression:
    Identifier
    Constant
    StringLiteral
    ( Expression )
    GenericSelection

GenericSelection:
    _Generic ( AssignmentExpression , GenericAssocList )

GenericAssocList:
    GenericAssociation {, GenericAssociation}

GenericAssociation:
    TypeName : AssignmentExpression
    default : AssignmentExpression

PostfixExpression:
    PrimaryExpression
    PostfixExpression '[' Expression ']'
    PostfixExpression ( [ArgumentExpressionList] )
    PostfixExpression . Identifier
    PostfixExpression -> Identifier
    PostfixExpression ++
    PostfixExpression --
    ( TypeName ) '{' InitializerList [,] '}'

ArgumentExpressionList:
    AssignmentExpression {, AssignmentExpression}

UnaryExpression:
    PostfixExpression
    ++ UnaryExpression
    -- UnaryExpression
    UnaryOperator CastExpression
    sizeof UnaryExpression
    sizeof ( TypeName )
    _Alignof ( TypeName )

UnaryOperator: (one of)
    & * + - ~ !

CastExpression:
    UnaryExpression
    ( TypeName ) CastExpression

MultiplicativeExpression:
    CastExpression
    MultiplicativeExpression * CastExpression
    MultiplicativeExpression / CastExpression
    MultiplicativeExpression % CastExpression

AdditiveExpression:
    MultiplicativeExpression
    AdditiveExpression + MultiplicativeExpression
    AdditiveExpression - MultiplicativeExpression

ShiftExpression:
    AdditiveExpression
    ShiftExpression << AdditiveExpression
    ShiftExpression >> AdditiveExpression

RelationalExpression:
    ShiftExpression
    RelationalExpression < ShiftExpression
    RelationalExpression > ShiftExpression
    RelationalExpression <= ShiftExpression
    RelationalExpression >= ShiftExpression

EqualityExpression:
    RelationalExpression
    EqualityExpression == RelationalExpression
    EqualityExpression != RelationalExpression

AndExpression:
    EqualityExpression
    AndExpression & EqualityExpression

ExclusiveOrExpression:
    AndExpression
    ExclusiveOrExpression ^ AndExpression

InclusiveOrExpression:
    ExclusiveOrExpression
    InclusiveOrExpression | ExclusiveOrExpression

LogicalAndExpression:
    InclusiveOrExpression
    LogicalAndExpression && InclusiveOrExpression

LogicalOrExpression:
    LogicalAndExpression
    LogicalOrExpression || LogicalAndExpression

ConditionalExpression:
    LogicalOrExpression
    LogicalOrExpression ? Expression : ConditionalExpression

AssignmentExpression:
    ConditionalExpression
    UnaryExpression AssignmentOperator AssignmentExpression

AssignmentOperator: (one of)
    = *= /= %= += -= <<= >>= &= ^= |=

Expression:
    AssignmentExpression {, AssignmentExpression}

ConstantExpression:
    ConditionalExpression
```

---

## Declarations

```text
Declaration:
    DeclarationSpecifiers [InitDeclaratorList] ;
    StaticAssertDeclaration

DeclarationSpecifiers:
    DeclarationSpecifier {DeclarationSpecifier}

DeclarationSpecifier:
    StorageClassSpecifier
    TypeSpecifier
    TypeQualifier
    FunctionSpecifier
    AlignmentSpecifier

InitDeclaratorList:
    InitDeclarator {, InitDeclarator}

InitDeclarator:
    Declarator [= Initializer]

StorageClassSpecifier: (one of)
    typedef extern static _Thread_local auto register

TypeSpecifier:
    void
    char
    short
    int
    long
    float
    double
    signed
    unsigned
    _Bool
    _Complex
    AtomicTypeSpecifier
    StructOrUnionSpecifier
    EnumSpecifier
    TypedefName
```

> Note: `_Imaginary` is additionally a `TypeSpecifier` only in
> implementations supporting Annex G; it is not part of the core C11
> grammar.

```text
StructOrUnionSpecifier:
    StructOrUnion [Identifier] '{' StructDeclarationList '}'
    StructOrUnion Identifier

StructOrUnion: (one of)
    struct union

StructDeclarationList:
    StructDeclaration {StructDeclaration}

StructDeclaration:
    SpecifierQualifierList [StructDeclaratorList] ;
    StaticAssertDeclaration

SpecifierQualifierList:
    (TypeSpecifier | TypeQualifier) {(TypeSpecifier | TypeQualifier)}

StructDeclaratorList:
    StructDeclarator {, StructDeclarator}

StructDeclarator:
    Declarator
    [Declarator] : ConstantExpression

EnumSpecifier:
    enum [Identifier] '{' EnumeratorList [,] '}'
    enum Identifier

EnumeratorList:
    Enumerator {, Enumerator}

Enumerator:
    EnumerationConstant [= ConstantExpression]

AtomicTypeSpecifier:
    _Atomic ( TypeName )

TypeQualifier: (one of)
    const restrict volatile _Atomic

FunctionSpecifier: (one of)
    inline _Noreturn

AlignmentSpecifier:
    _Alignas ( TypeName )
    _Alignas ( ConstantExpression )

Declarator:
    [Pointer] DirectDeclarator

DirectDeclarator:
    Identifier
    ( Declarator )
    DirectDeclarator '[' [TypeQualifierList] [AssignmentExpression] ']'
    DirectDeclarator '[' static [TypeQualifierList] AssignmentExpression ']'
    DirectDeclarator '[' TypeQualifierList static AssignmentExpression ']'
    DirectDeclarator '[' [TypeQualifierList] * ']'
    DirectDeclarator ( ParameterTypeList )
    DirectDeclarator ( [IdentifierList] )

Pointer:
    * [TypeQualifierList] [Pointer]

TypeQualifierList:
    TypeQualifier {TypeQualifier}

ParameterTypeList:
    ParameterList [, ...]

ParameterList:
    ParameterDeclaration {, ParameterDeclaration}

ParameterDeclaration:
    DeclarationSpecifiers Declarator
    DeclarationSpecifiers [AbstractDeclarator]

IdentifierList:
    Identifier {, Identifier}

TypeName:
    SpecifierQualifierList [AbstractDeclarator]

AbstractDeclarator:
    Pointer
    [Pointer] DirectAbstractDeclarator

DirectAbstractDeclarator:
    ( AbstractDeclarator )
    [DirectAbstractDeclarator] '[' [TypeQualifierList] [AssignmentExpression] ']'
    [DirectAbstractDeclarator] '[' static [TypeQualifierList] AssignmentExpression ']'
    [DirectAbstractDeclarator] '[' TypeQualifierList static AssignmentExpression ']'
    [DirectAbstractDeclarator] '[' * ']'
    [DirectAbstractDeclarator] ( [ParameterTypeList] )

TypedefName:
    Identifier

Initializer:
    AssignmentExpression
    '{' InitializerList [,] '}'

InitializerList:
    [Designation] Initializer {, [Designation] Initializer}

Designation:
    DesignatorList =

DesignatorList:
    Designator {Designator}

Designator:
    '[' ConstantExpression ']'
    . Identifier

StaticAssertDeclaration:
    _Static_assert ( ConstantExpression , StringLiteral ) ;
```

---

## Statements

```text
Statement:
    LabeledStatement
    CompoundStatement
    ExpressionStatement
    SelectionStatement
    IterationStatement
    JumpStatement
    AsmStatement

LabeledStatement:
    Identifier : Statement
    case ConstantExpression : Statement
    default : Statement

CompoundStatement:
    '{' [BlockItemList] '}'

BlockItemList:
    BlockItem {BlockItem}

BlockItem:
    Declaration
    Statement

ExpressionStatement:
    [Expression] ;

SelectionStatement:
    if ( Expression ) Statement
    if ( Expression ) Statement else Statement
    switch ( Expression ) Statement

IterationStatement:
    while ( Expression ) Statement
    do Statement while ( Expression ) ;
    for ( [Expression] ; [Expression] ; [Expression] ) Statement
    for ( Declaration [Expression] ; [Expression] ) Statement

JumpStatement:
    goto Identifier ;
    continue ;
    break ;
    return [Expression] ;

AsmStatement:                          # gcc's, not C11's
    asm-keyword {AsmQualifier} ( AsmBody ) ;

asm-keyword:
    asm | __asm | __asm__

AsmQualifier:
    volatile | __volatile__ | inline | __inline__ | goto

AsmBody:                               # basic asm is the first line alone
    StringLiteral
    StringLiteral : [AsmOperandList]
                  [: [AsmOperandList] [: [StringList] [: IdentifierList]]]

AsmOperandList:
    AsmOperand {, AsmOperand}

AsmOperand:
    [ '[' Identifier ']' ] StringLiteral ( Expression )
```

**The asm statement is gcc's and is in the tree for what a header writes.**
A compiler that reads system headers reads inline assembly, so the statement
is parsed rather than skipped: the operand expressions are expressions, the
constraints and clobbers are string literals, and the last list belongs to
`asm goto` alone and names labels rather than objects. The template is not
read at this layer — what `%0` means depends on an operand list, and what a
constraint letter means depends on the target.

---

## External Definitions

```text
TranslationUnit:
    ExternalDeclaration {ExternalDeclaration}

ExternalDeclaration:
    FunctionDefinition
    Declaration

FunctionDefinition:
    DeclarationSpecifiers Declarator [DeclarationList] CompoundStatement

DeclarationList:
    Declaration {Declaration}
```