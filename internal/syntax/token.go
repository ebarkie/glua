// Package syntax turns Lua 5.4 source text into an abstract syntax tree. It knows nothing
// about the VM: the compiler in the glua package walks the tree it produces.
package syntax

import "fmt"

// Kind is the kind of a token.
type Kind uint8

const (
	EOF Kind = iota

	// literals and names
	Name
	String
	Int
	Float

	// reserved words
	And
	Break
	Do
	Else
	Elseif
	End
	False
	For
	Function
	Goto
	If
	In
	Local
	Nil
	Not
	Or
	Repeat
	Return
	Then
	True
	Until
	While

	// operators and punctuation
	Plus     // +
	Minus    // -
	Star     // *
	Slash    // /
	DSlash   // //
	Percent  // %
	Caret    // ^
	Hash     // #
	Amp      // &
	Tilde    // ~
	Pipe     // |
	Shl      // <<
	Shr      // >>
	Concat   // ..
	Dots     // ...
	Eq       // ==
	Ne       // ~=
	Le       // <=
	Ge       // >=
	Lt       // <
	Gt       // >
	Assign   // =
	LParen   // (
	RParen   // )
	LBrace   // {
	RBrace   // }
	LBracket // [
	RBracket // ]
	DColon   // ::
	Semi     // ;
	Colon    // :
	Comma    // ,
	Dot      // .
)

var kindText = [...]string{
	EOF: "<eof>", Name: "<name>", String: "<string>", Int: "<integer>", Float: "<number>",
	And: "and", Break: "break", Do: "do", Else: "else", Elseif: "elseif", End: "end",
	False: "false", For: "for", Function: "function", Goto: "goto", If: "if", In: "in",
	Local: "local", Nil: "nil", Not: "not", Or: "or", Repeat: "repeat", Return: "return",
	Then: "then", True: "true", Until: "until", While: "while",
	Plus: "+", Minus: "-", Star: "*", Slash: "/", DSlash: "//", Percent: "%", Caret: "^",
	Hash: "#", Amp: "&", Tilde: "~", Pipe: "|", Shl: "<<", Shr: ">>", Concat: "..",
	Dots: "...", Eq: "==", Ne: "~=", Le: "<=", Ge: ">=", Lt: "<", Gt: ">", Assign: "=",
	LParen: "(", RParen: ")", LBrace: "{", RBrace: "}", LBracket: "[", RBracket: "]",
	DColon: "::", Semi: ";", Colon: ":", Comma: ",", Dot: ".",
}

func (k Kind) String() string {
	if int(k) < len(kindText) {
		return kindText[k]
	}
	return fmt.Sprintf("Kind(%d)", k)
}

// quoted is how C Lua's luaX_token2str names a token kind in messages: symbols and reserved
// words in quotes, the others as <eof>, <name> and so on.
func (k Kind) quoted() string {
	if k >= And {
		return "'" + kindText[k] + "'"
	}
	return kindText[k]
}

var keywords = map[string]Kind{
	"and": And, "break": Break, "do": Do, "else": Else, "elseif": Elseif, "end": End,
	"false": False, "for": For, "function": Function, "goto": Goto, "if": If, "in": In,
	"local": Local, "nil": Nil, "not": Not, "or": Or, "repeat": Repeat, "return": Return,
	"then": Then, "true": True, "until": Until, "while": While,
}

// IsReserved reports whether s is a reserved word of Lua.
func IsReserved(s string) bool {
	_, ok := keywords[s]
	return ok
}

// Token is one lexical token.
type Token struct {
	Kind Kind
	Line int     // 1-based
	Col  int     // 1-based byte column of the first character
	Str  string  // the name, or the string's contents after escapes
	Int  int64   // the value of an Int token
	Num  float64 // the value of a Float token
	raw  string  // the text C Lua shows after "near" for names, strings and numerals
}

// Error is a syntax error. Its message has C Lua's form, such as "unexpected symbol near 'x'".
type Error struct {
	Source    string
	Line, Col int
	Msg       string
}

func (e *Error) Error() string { return fmt.Sprintf("%s:%d: %s", e.Source, e.Line, e.Msg) }
