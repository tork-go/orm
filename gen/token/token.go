package token

import "fmt"

// Pos is a position in a schema file. Offset is a 0 based byte offset
// into the file. Line and Col are 1 based and counted in bytes, the same
// scheme go/token uses, so a position pastes straight into an editor's
// go-to-line box. Byte columns and display columns only diverge on
// non-ASCII source, and the language server converts at its own boundary,
// so nothing else needs to care about encodings.
type Pos struct {
	Offset int
	Line   int
	Col    int
}

// String renders the position as "line:col", the fragment every
// diagnostic embeds right after the file name.
func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Span is a half open source range: Start points at the first byte of a
// construct and End at the first byte after it. A span is what an editor
// underlines, so every token, syntax node, and diagnostic carries one.
type Span struct {
	Start Pos
	End   Pos
}

// Kind classifies a token. Keywords such as "model" and "datasource" are
// deliberately not kinds of their own: they lex as KindIdent and the
// parser matches their spelling, which keeps every keyword contextual.
// A model may therefore have a field named "model" without the lexer
// needing to know where it is.
type Kind int

const (
	// KindEOF terminates every token stream, even an empty one, so a
	// parser never has to bounds check its lookahead.
	KindEOF Kind = iota

	// KindNewline is significant in .tork: a field declaration ends at
	// the end of its line unless the line breaks inside parentheses.
	// The lexer therefore reports newlines instead of swallowing them.
	KindNewline

	KindComment // // line comment; Lit holds the text after the slashes
	KindIdent   // model names, field names, keywords, attribute words
	KindString  // "double quoted"; Lit holds the decoded value
	KindInt     // 42, -7
	KindFloat   // 1.5, -0.25

	KindAt       // @
	KindAtAt     // @@
	KindDot      // .
	KindComma    // ,
	KindColon    // :
	KindQuestion // ?
	KindAssign   // =
	KindLBrace   // {
	KindRBrace   // }
	KindLParen   // (
	KindRParen   // )
	KindLBracket // [
	KindRBracket // ]

	// KindIllegal marks a character the language has no use for. The
	// lexer emits it alongside a diagnostic rather than stopping, so
	// one stray byte never hides the rest of the file from the parser.
	KindIllegal
)

// kindNames is indexed by Kind. The spellings are what test failures and
// debug output print, so they favor the source syntax over Go
// identifiers where the token has a fixed spelling.
var kindNames = [...]string{
	KindEOF:      "EOF",
	KindNewline:  "newline",
	KindComment:  "comment",
	KindIdent:    "ident",
	KindString:   "string",
	KindInt:      "int",
	KindFloat:    "float",
	KindAt:       "@",
	KindAtAt:     "@@",
	KindDot:      ".",
	KindComma:    ",",
	KindColon:    ":",
	KindQuestion: "?",
	KindAssign:   "=",
	KindLBrace:   "{",
	KindRBrace:   "}",
	KindLParen:   "(",
	KindRParen:   ")",
	KindLBracket: "[",
	KindRBracket: "]",
	KindIllegal:  "illegal",
}

// String names the kind for debug output and test failure messages.
func (k Kind) String() string {
	if k < 0 || int(k) >= len(kindNames) {
		return fmt.Sprintf("Kind(%d)", int(k))
	}
	return kindNames[k]
}

// Token is one lexical unit of a schema file. Lit is the decoded literal:
// for a string token it is the value with escapes resolved, for a comment
// the text after the two slashes, and for everything else the raw source
// spelling. The raw bytes are always recoverable through Span, which is
// why the decoded form can be stored without losing information.
type Token struct {
	Kind Kind
	Lit  string
	Span Span
}
