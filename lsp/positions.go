package lsp

import (
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/tork-go/orm/gen/token"
)

// Everything in this file exists because the toolchain and the
// protocol count differently. The parser records byte offsets with 1
// based lines and columns; the protocol wants 0 based lines and
// character offsets measured in UTF-16 code units. Keeping the
// conversion in one place means the rest of the server can think in
// exactly one coordinate system, the parser's.

// document is one file's text with the line offsets that make position
// conversion a lookup rather than a scan.
type document struct {
	text  string
	lines []int // byte offset where each line starts
}

// newDocument indexes a document's line starts, counting breaks the
// way the lexer does: LF, CRLF, and a lone CR each end one line. Any
// other rule and the two would disagree about which line a span is on
// the moment a file arrived with old style endings.
func newDocument(text string) *document {
	d := &document{text: text, lines: []int{0}}
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			d.lines = append(d.lines, i+1)
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				continue
			}
			d.lines = append(d.lines, i+1)
		}
	}
	return d
}

// lineText returns line n, counted from zero, without its terminator.
// Callers bound n first; this is only ever asked about a line the
// document has.
func (d *document) lineText(n int) string {
	start := d.lines[n]
	end := len(d.text)
	if n+1 < len(d.lines) {
		end = d.lines[n+1]
	}
	return strings.TrimRight(d.text[start:end], "\r\n")
}

// offset converts a protocol position to a byte offset. Positions past
// the end of a line or of the file clamp rather than fail: an editor
// may legitimately ask about the position just past the last
// character, and a client that asks about nonsense should get the
// nearest sensible answer instead of an error.
func (d *document) offset(p Position) int {
	if p.Line < 0 {
		return 0
	}
	if p.Line >= len(d.lines) {
		return len(d.text)
	}
	line := d.lineText(p.Line)
	units := 0
	for i, r := range line {
		if units >= p.Character {
			return d.lines[p.Line] + i
		}
		units += utf16Len(r)
	}
	return d.lines[p.Line] + len(line)
}

// position converts a parser position to a protocol one. The span
// always comes from this very document, so the line is in range;
// the column is clamped because a span may end just past the last
// character of its line.
func (d *document) position(p token.Pos) Position {
	line := p.Line - 1
	text := d.lineText(line)
	byteCol := min(p.Col-1, len(text))
	units := 0
	for _, r := range text[:byteCol] {
		units += utf16Len(r)
	}
	return Position{Line: line, Character: units}
}

func (d *document) rangeOf(s token.Span) Range {
	return Range{Start: d.position(s.Start), End: d.position(s.End)}
}

// wholeRange spans the entire document, which is what a formatting
// edit replaces.
func (d *document) wholeRange() Range {
	last := len(d.lines) - 1
	return Range{
		Start: Position{},
		End:   Position{Line: last, Character: utf16Width(d.lineText(last))},
	}
}

func utf16Len(r rune) int {
	if r > 0xFFFF {
		return 2
	}
	return 1
}

func utf16Width(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// uriToPath turns a file URI into a filesystem path. Anything that is
// not a file URI comes back empty, and the caller treats that as a
// document it cannot serve, since this server only ever works against
// real directories on disk.
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	// u.Path is already decoded; unescaping it again would turn a file
	// literally named "a%20b" into "a b".
	path := u.Path
	// A Windows URI carries the drive letter as the first path
	// element, as in file:///c:/x, which becomes /c:/x on parsing.
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

// pathToURI is the inverse, used to point diagnostics and definitions
// at files the editor may not have open. Every path it sees came from
// a URI, and so is absolute already.
func pathToURI(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	return u.String()
}
