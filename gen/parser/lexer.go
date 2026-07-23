package parser

import (
	"unicode/utf8"

	"github.com/tork-go/orm/gen/diag"
	"github.com/tork-go/orm/gen/token"
)

// Scan tokenizes one .tork source file. It never fails: malformed input
// produces KindIllegal tokens and diagnostics rather than an error, and
// the stream always ends with a KindEOF token, so a consumer can run to
// completion over whatever the user has typed so far. That tolerance is
// the point of the design; the language server lexes on every keystroke,
// when the source is almost never well formed.
//
// The file name is only recorded into diagnostics, never opened; callers
// own all file reading so that the language server can lex unsaved
// editor buffers.
func Scan(file string, src []byte) ([]token.Token, []diag.Diagnostic) {
	l := &lexer{file: file, src: src, line: 1, col: 1}
	l.run()
	return l.toks, l.diags
}

// lexer is a single pass, byte at a time scanner. Columns count bytes,
// so advancing never needs to decode UTF-8 except at the two places a
// whole rune matters: an unexpected character (for a readable %q in the
// diagnostic) and an unknown string escape.
type lexer struct {
	file  string
	src   []byte
	off   int
	line  int
	col   int
	toks  []token.Token
	diags []diag.Diagnostic
}

func (l *lexer) pos() token.Pos {
	return token.Pos{Offset: l.off, Line: l.line, Col: l.col}
}

// peek returns the current byte, or 0 at end of file. The zero byte is a
// safe sentinel because it matches no character class the scanner tests
// for.
func (l *lexer) peek() byte {
	if l.off < len(l.src) {
		return l.src[l.off]
	}
	return 0
}

// peekAt returns the byte i positions ahead, or 0 past end of file.
func (l *lexer) peekAt(i int) byte {
	if l.off+i < len(l.src) {
		return l.src[l.off+i]
	}
	return 0
}

// bump consumes one byte, keeping line and column in step. A newline can
// never sit inside a multi byte rune, so byte wise bookkeeping stays
// correct for any UTF-8 input.
func (l *lexer) bump() byte {
	b := l.src[l.off]
	l.off++
	if b == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return b
}

// bumpRune consumes one whole rune and returns it, for the two spots
// where a diagnostic should name a character rather than a byte.
func (l *lexer) bumpRune() rune {
	r, size := utf8.DecodeRune(l.src[l.off:])
	for range size {
		l.bump()
	}
	return r
}

func (l *lexer) emit(kind token.Kind, lit string, start token.Pos) {
	l.toks = append(l.toks, token.Token{
		Kind: kind,
		Lit:  lit,
		Span: token.Span{Start: start, End: l.pos()},
	})
}

func (l *lexer) errorf(start token.Pos, format string, args ...any) {
	l.diags = append(l.diags, diag.Errorf(l.file, token.Span{Start: start, End: l.pos()}, format, args...))
}

func (l *lexer) run() {
	for l.off < len(l.src) {
		start := l.pos()
		switch b := l.peek(); {
		case b == ' ' || b == '\t':
			l.bump()
		case b == '\n':
			l.bump()
			l.emit(token.KindNewline, "\n", start)
		case b == '\r':
			// A CRLF pair is one line break; a lone CR is treated as
			// one too, so files from any editor lex identically. Only
			// the LF byte moves the line counter inside bump, so the
			// lone CR advances it here.
			l.bump()
			if l.peek() == '\n' {
				l.bump()
			} else {
				l.line++
				l.col = 1
			}
			l.emit(token.KindNewline, "\n", start)
		case b == '/':
			l.scanComment(start)
		case b == '"':
			l.scanString(start)
		case b == '@':
			l.bump()
			if l.peek() == '@' {
				l.bump()
				l.emit(token.KindAtAt, "@@", start)
			} else {
				l.emit(token.KindAt, "@", start)
			}
		case isIdentStart(b):
			l.scanIdent(start)
		case isDigit(b):
			l.scanNumber(start)
		case b == '-' && isDigit(l.peekAt(1)):
			l.bump()
			l.scanNumber(start)
		default:
			l.scanPunct(start)
		}
	}
	end := l.pos()
	l.toks = append(l.toks, token.Token{Kind: token.KindEOF, Span: token.Span{Start: end, End: end}})
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || isDigit(b)
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func (l *lexer) scanIdent(start token.Pos) {
	for isIdentPart(l.peek()) {
		l.bump()
	}
	l.emit(token.KindIdent, string(l.src[start.Offset:l.off]), start)
}

// scanNumber is entered on a digit, or after a leading minus sign has
// already been consumed. The dot is claimed only when a digit follows,
// so "1.x" lexes as int, dot, ident rather than a broken float.
func (l *lexer) scanNumber(start token.Pos) {
	for isDigit(l.peek()) {
		l.bump()
	}
	kind := token.KindInt
	if l.peek() == '.' && isDigit(l.peekAt(1)) {
		kind = token.KindFloat
		l.bump()
		for isDigit(l.peek()) {
			l.bump()
		}
	}
	l.emit(kind, string(l.src[start.Offset:l.off]), start)
}

// scanComment is entered on a slash. Only the double slash line comment
// exists in .tork; a lone slash is reported with the fix in the message,
// following the house rule that an error should name the way out.
func (l *lexer) scanComment(start token.Pos) {
	l.bump()
	if l.peek() != '/' {
		l.errorf(start, "unexpected character '/' (line comments start with //)")
		l.emit(token.KindIllegal, "/", start)
		return
	}
	l.bump()
	textStart := l.off
	for l.off < len(l.src) && l.peek() != '\n' && l.peek() != '\r' {
		l.bump()
	}
	l.emit(token.KindComment, string(l.src[textStart:l.off]), start)
}

// scanString decodes a double quoted literal. On any malformation it
// reports a diagnostic and still emits a KindString token holding
// whatever value accumulated, because downstream stages working on a
// best effort value produce far better follow up diagnostics than ones
// staring at a hole in the token stream.
func (l *lexer) scanString(start token.Pos) {
	l.bump()
	var value []byte
	for {
		if l.off >= len(l.src) {
			l.errorf(start, "unterminated string literal")
			break
		}
		b := l.peek()
		if b == '\n' || b == '\r' {
			l.errorf(start, "unterminated string literal (strings cannot span lines)")
			break
		}
		if b == '"' {
			l.bump()
			break
		}
		if b == '\\' {
			escStart := l.pos()
			l.bump()
			if l.off >= len(l.src) {
				l.errorf(start, "unterminated string literal")
				break
			}
			switch r := l.bumpRune(); r {
			case '"':
				value = append(value, '"')
			case '\\':
				value = append(value, '\\')
			case 'n':
				value = append(value, '\n')
			case 't':
				value = append(value, '\t')
			default:
				// Keep the character so the value stays useful, but
				// flag the escape: silently eating the backslash
				// would make "C:\temp" look accepted.
				l.diags = append(l.diags, diag.Errorf(l.file,
					token.Span{Start: escStart, End: l.pos()},
					`unknown escape sequence \%c in string`, r))
				value = append(value, string(r)...)
			}
			continue
		}
		value = append(value, l.bump())
	}
	l.emit(token.KindString, string(value), start)
}

// scanPunct handles every single character token and, as the last
// resort, the character the language has no use for.
func (l *lexer) scanPunct(start token.Pos) {
	r := l.bumpRune()
	var kind token.Kind
	switch r {
	case '.':
		kind = token.KindDot
	case ',':
		kind = token.KindComma
	case ':':
		kind = token.KindColon
	case '?':
		kind = token.KindQuestion
	case '=':
		kind = token.KindAssign
	case '{':
		kind = token.KindLBrace
	case '}':
		kind = token.KindRBrace
	case '(':
		kind = token.KindLParen
	case ')':
		kind = token.KindRParen
	case '[':
		kind = token.KindLBracket
	case ']':
		kind = token.KindRBracket
	default:
		l.errorf(start, "unexpected character %q", r)
		l.emit(token.KindIllegal, string(r), start)
		return
	}
	l.emit(kind, string(r), start)
}
