package gen_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/tork-go/orm/gen/diag"
	"github.com/tork-go/orm/gen/parser"
	"github.com/tork-go/orm/gen/token"
)

// tokenStrings flattens a token stream into one readable line per token,
// quoting the literal where one carries information, so test failures
// show exactly which token diverged.
func tokenStrings(toks []token.Token) []string {
	out := make([]string, 0, len(toks))
	for _, tk := range toks {
		switch tk.Kind {
		case token.KindIdent, token.KindString, token.KindInt, token.KindFloat,
			token.KindComment, token.KindIllegal:
			out = append(out, fmt.Sprintf("%s %q", tk.Kind, tk.Lit))
		default:
			out = append(out, tk.Kind.String())
		}
	}
	return out
}

func diagStrings(ds []diag.Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.String())
	}
	return out
}

func assertStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s count = %d, want %d\ngot:\n%s\nwant:\n%s",
			label, len(got), len(want),
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %s\nwant%s = %s", label, i, got[i], strings.Repeat(" ", len(label)-1), want[i])
		}
	}
}

func TestScan_TokenizesTheFullVocabulary(t *testing.T) {
	src := `datasource db { provider = "postgres" }
model User {
	id Int @id @default(autoincrement())
	tags String[]? @db.VarChar(30)
	score Float @default(-1.5) // best so far
	@@index([id, tags], name: "idx_user")
}
`
	toks, diags := parser.Scan("schema.tork", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("Scan reported diagnostics on well formed input:\n%s", strings.Join(diagStrings(diags), "\n"))
	}
	want := []string{
		`ident "datasource"`, `ident "db"`, "{", `ident "provider"`, "=", `string "postgres"`, "}", "newline",
		`ident "model"`, `ident "User"`, "{", "newline",
		`ident "id"`, `ident "Int"`, "@", `ident "id"`, "@", `ident "default"`, "(", `ident "autoincrement"`, "(", ")", ")", "newline",
		`ident "tags"`, `ident "String"`, "[", "]", "?", "@", `ident "db"`, ".", `ident "VarChar"`, "(", `int "30"`, ")", "newline",
		`ident "score"`, `ident "Float"`, "@", `ident "default"`, "(", `float "-1.5"`, ")", `comment " best so far"`, "newline",
		"@@", `ident "index"`, "(", "[", `ident "id"`, ",", `ident "tags"`, "]", ",", `ident "name"`, ":", `string "idx_user"`, ")", "newline",
		"}", "newline",
		"EOF",
	}
	assertStrings(t, "token", tokenStrings(toks), want)
}

func TestScan_ReportsBytePrecisePositions(t *testing.T) {
	toks, diags := parser.Scan("schema.tork", []byte("a b\ncd\n"))
	if len(diags) != 0 {
		t.Fatalf("Scan reported diagnostics: %v", diagStrings(diags))
	}
	want := []token.Token{
		{Kind: token.KindIdent, Lit: "a", Span: token.Span{Start: token.Pos{Offset: 0, Line: 1, Col: 1}, End: token.Pos{Offset: 1, Line: 1, Col: 2}}},
		{Kind: token.KindIdent, Lit: "b", Span: token.Span{Start: token.Pos{Offset: 2, Line: 1, Col: 3}, End: token.Pos{Offset: 3, Line: 1, Col: 4}}},
		{Kind: token.KindNewline, Lit: "\n", Span: token.Span{Start: token.Pos{Offset: 3, Line: 1, Col: 4}, End: token.Pos{Offset: 4, Line: 2, Col: 1}}},
		{Kind: token.KindIdent, Lit: "cd", Span: token.Span{Start: token.Pos{Offset: 4, Line: 2, Col: 1}, End: token.Pos{Offset: 6, Line: 2, Col: 3}}},
		{Kind: token.KindNewline, Lit: "\n", Span: token.Span{Start: token.Pos{Offset: 6, Line: 2, Col: 3}, End: token.Pos{Offset: 7, Line: 3, Col: 1}}},
		{Kind: token.KindEOF, Span: token.Span{Start: token.Pos{Offset: 7, Line: 3, Col: 1}, End: token.Pos{Offset: 7, Line: 3, Col: 1}}},
	}
	if !reflect.DeepEqual(toks, want) {
		t.Errorf("tokens = %+v\nwant   = %+v", toks, want)
	}
}

func TestScan_DecodesStringEscapes(t *testing.T) {
	toks, diags := parser.Scan("schema.tork", []byte(`"a\"b\\c\nd\te"`))
	if len(diags) != 0 {
		t.Fatalf("Scan reported diagnostics: %v", diagStrings(diags))
	}
	if want := "a\"b\\c\nd\te"; toks[0].Lit != want {
		t.Errorf("string value = %q, want %q", toks[0].Lit, want)
	}
}

func TestScan_TreatsEveryLineBreakStyleAsOneNewline(t *testing.T) {
	toks, diags := parser.Scan("schema.tork", []byte("a\r\nb\rc\nd"))
	if len(diags) != 0 {
		t.Fatalf("Scan reported diagnostics: %v", diagStrings(diags))
	}
	want := []string{`ident "a"`, "newline", `ident "b"`, "newline", `ident "c"`, "newline", `ident "d"`, "EOF"}
	assertStrings(t, "token", tokenStrings(toks), want)

	lines := []int{1, 1, 2, 2, 3, 3, 4, 4}
	for i, tk := range toks {
		if tk.Span.Start.Line != lines[i] {
			t.Errorf("token[%d] (%s) starts at line %d, want %d", i, tk.Kind, tk.Span.Start.Line, lines[i])
		}
	}
}

func TestScan_EmptyInputYieldsOnlyEOF(t *testing.T) {
	toks, diags := parser.Scan("schema.tork", nil)
	if len(diags) != 0 {
		t.Fatalf("Scan reported diagnostics: %v", diagStrings(diags))
	}
	want := []token.Token{
		{Kind: token.KindEOF, Span: token.Span{Start: token.Pos{Offset: 0, Line: 1, Col: 1}, End: token.Pos{Offset: 0, Line: 1, Col: 1}}},
	}
	if !reflect.DeepEqual(toks, want) {
		t.Errorf("tokens = %+v\nwant   = %+v", toks, want)
	}
}

func TestScan_NumberEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"dot not followed by digit stays a dot", "1.x", []string{`int "1"`, ".", `ident "x"`, "EOF"}},
		{"trailing dot stays a dot", "1.", []string{`int "1"`, ".", "EOF"}},
		{"negative int", "-12", []string{`int "-12"`, "EOF"}},
		{"negative float", "-0.25", []string{`float "-0.25"`, "EOF"}},
		{"leading zeros are lexed verbatim", "007", []string{`int "007"`, "EOF"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, diags := parser.Scan("schema.tork", []byte(tt.src))
			if len(diags) != 0 {
				t.Fatalf("Scan reported diagnostics: %v", diagStrings(diags))
			}
			assertStrings(t, "token", tokenStrings(toks), tt.want)
		})
	}
}

func TestScan_CommentEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"comment at end of file without newline", "// hi", []string{`comment " hi"`, "EOF"}},
		{"empty comment", "//", []string{`comment ""`, "EOF"}},
		{"comment stops at carriage return", "// a\r\nb", []string{`comment " a"`, "newline", `ident "b"`, "EOF"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, diags := parser.Scan("schema.tork", []byte(tt.src))
			if len(diags) != 0 {
				t.Fatalf("Scan reported diagnostics: %v", diagStrings(diags))
			}
			assertStrings(t, "token", tokenStrings(toks), tt.want)
		})
	}
}

func TestScan_MalformedInputs(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		wantTokens []string
		wantDiags  []string
	}{
		{
			name:       "unterminated string at end of file",
			src:        `"abc`,
			wantTokens: []string{`string "abc"`, "EOF"},
			wantDiags:  []string{"schema.tork:1:1: unterminated string literal"},
		},
		{
			name:       "unterminated string at line break keeps lexing",
			src:        "\"abc\nid",
			wantTokens: []string{`string "abc"`, "newline", `ident "id"`, "EOF"},
			wantDiags:  []string{"schema.tork:1:1: unterminated string literal (strings cannot span lines)"},
		},
		{
			name:       "unknown escape keeps the character",
			src:        `"a\qb"`,
			wantTokens: []string{`string "aqb"`, "EOF"},
			wantDiags:  []string{`schema.tork:1:3: unknown escape sequence \q in string`},
		},
		{
			name:       "backslash at end of file",
			src:        `"a\`,
			wantTokens: []string{`string "a"`, "EOF"},
			wantDiags:  []string{"schema.tork:1:1: unterminated string literal"},
		},
		{
			name:       "lone slash names the comment syntax",
			src:        "a / b",
			wantTokens: []string{`ident "a"`, `illegal "/"`, `ident "b"`, "EOF"},
			wantDiags:  []string{"schema.tork:1:3: unexpected character '/' (line comments start with //)"},
		},
		{
			name:       "stray character",
			src:        "a % b",
			wantTokens: []string{`ident "a"`, `illegal "%"`, `ident "b"`, "EOF"},
			wantDiags:  []string{"schema.tork:1:3: unexpected character '%'"},
		},
		{
			name:       "stray non ASCII character",
			src:        "é",
			wantTokens: []string{`illegal "é"`, "EOF"},
			wantDiags:  []string{"schema.tork:1:1: unexpected character 'é'"},
		},
		{
			name:       "minus without a digit",
			src:        "-x",
			wantTokens: []string{`illegal "-"`, `ident "x"`, "EOF"},
			wantDiags:  []string{"schema.tork:1:1: unexpected character '-'"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, diags := parser.Scan("schema.tork", []byte(tt.src))
			assertStrings(t, "token", tokenStrings(toks), tt.wantTokens)
			assertStrings(t, "diag", diagStrings(diags), tt.wantDiags)
		})
	}
}
