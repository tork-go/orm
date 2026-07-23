package lsp_test

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tork-go/orm/lsp"
)

// The protocol counts characters in UTF-16 code units while the parser
// counts bytes, so every position crossing that boundary is a chance
// to be off by the width of a character. These tests put non-ASCII
// text to the left of the interesting column on purpose.

func TestPositions_AreCountedInUTF16Units(t *testing.T) {
	tests := []struct {
		name string
		// runes sit inside a string on the field line, to the left of
		// the repeated attribute that earns the diagnostic.
		runes string
		// want is the character offset that diagnostic should carry.
		want int
	}{
		{"ascii", "ab", 32},
		{"two byte runes", "éé", 32},
		{"three byte runes", "日本", 32},
		{"surrogate pairs", "🙂🙂", 34},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := start(t)
			c.initialize()
			uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
			text := "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id @default(autoincrement())\n" +
				"\tname String @map(\"" + tt.runes + "\") @unique @unique\n}\n"
			c.open(uri, text)

			// Non-ASCII is not a valid SQL identifier, so those cases
			// also earn a complaint about the @map value; the repeated
			// attribute is the one whose position is under test.
			ds := c.diagnosticsFor(uri)
			var repeated *lsp.Diagnostic
			for i := range ds {
				if strings.HasPrefix(ds[i].Message, "@unique repeated") {
					repeated = &ds[i]
				}
			}
			if repeated == nil {
				t.Fatalf("no diagnostic for the repeated attribute")
			}
			if repeated.Range.Start.Line != 6 {
				t.Errorf("line = %d, want 6", repeated.Range.Start.Line)
			}
			if repeated.Range.Start.Character != tt.want {
				t.Errorf("character = %d, want %d", repeated.Range.Start.Character, tt.want)
			}
		})
	}
}

// A client's position also arrives in UTF-16 units, so hovering past
// non-ASCII text has to land on the same construct the diagnostic
// pointed at.
func TestPositions_RequestsCountInUTF16UnitsToo(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id @default(autoincrement()) // 🙂🙂 comment\n\tname String\n}\n")

	var hover *lsp.Hover
	c.call("textDocument/hover", positionAt(uri, 6, 3), &hover)
	if hover == nil || !strings.Contains(hover.Contents.Value, "Column `name`") {
		t.Fatalf("hover = %+v, want the name field", hover)
	}
	// The range comes back in the same units it was asked in.
	if hover.Range.Start.Character != 1 || hover.Range.End.Character != 5 {
		t.Errorf("range = %+v, want characters 1 to 5", hover.Range)
	}
}

// Clients do send positions past the end of a line or a file, most
// often while a document is being replaced. Clamping to the nearest
// real position is better than refusing to answer.
func TestPositions_OutOfRangeClamp(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id @default(autoincrement())\n}\n")

	for _, tt := range []struct {
		name string
		line int
		col  int
	}{
		{"past the last line", 99, 0},
		{"past the end of a line", 5, 999},
		{"a negative line", -1, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var hover *lsp.Hover
			m := c.call("textDocument/hover", positionAt(uri, tt.line, tt.col), &hover)
			if m.Error != nil {
				t.Errorf("error = %+v, want an answer rather than a refusal", m.Error)
			}
		})
	}
}

func TestFormatting_EditSpansAFileWithoutATrailingNewline(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "model  A {\n\tid Int @id\n}")

	var edits []lsp.TextEdit
	c.call("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}, &edits)
	if len(edits) != 1 {
		t.Fatalf("edits = %+v, want one", edits)
	}
	// The last line has no terminator, so the edit must reach its end
	// column rather than the start of a line that does not exist.
	if edits[0].Range.End.Line != 2 || edits[0].Range.End.Character != 1 {
		t.Errorf("end = %+v, want line 2 character 1", edits[0].Range.End)
	}
}

// A path with characters that must be percent encoded still resolves,
// which matters because temp directories and user home directories
// routinely contain spaces.
func TestURIs_WithEscapedCharactersResolve(t *testing.T) {
	c := start(t)
	c.initialize()

	dir := filepath.Join(t.TempDir(), "my schema dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating directory: %v", err)
	}
	path := filepath.Join(dir, "a b.tork")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	uri := u.String()
	if !strings.Contains(uri, "%20") {
		t.Fatalf("the test URI is not escaped: %s", uri)
	}

	c.open(uri, "model A {\n\tid Int @id @default(autoincrement())\n}\n")
	ds := c.diagnosticsFor(uri)
	if len(ds) != 1 || !strings.Contains(ds[0].Message, "missing datasource") {
		t.Errorf("diagnostics = %v, want the missing datasource for the escaped path", messages(ds))
	}
}

func TestURIs_MalformedAreIgnored(t *testing.T) {
	c := start(t)
	c.initialize()
	for _, uri := range []string{"::not a uri", "http://example.com/schema.tork", ""} {
		t.Run(uri, func(t *testing.T) {
			c.open(uri, "model A {\n\tid Int @id\n}\n")
			m := c.call("textDocument/hover", positionAt(uri, 0, 0), nil)
			if m.Error != nil {
				t.Errorf("error = %+v, want a null result", m.Error)
			}
			if string(m.Result) != "null" {
				t.Errorf("result = %s, want null", m.Result)
			}
		})
	}
}
