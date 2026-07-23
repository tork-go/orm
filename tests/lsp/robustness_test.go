package lsp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tork-go/orm/lsp"
)

// Hover and definition are asked about constructs the analyzer threw
// away as often as about ones it kept, because a user hovers a thing
// precisely when it is not behaving. Every one of those has to answer
// with nothing rather than reach through a nil.
func TestFeatures_OnConstructsTheAnalyzerDiscarded(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{
		// A model redeclared in another file is registered to the
		// first file only, so the copy here resolves to nothing.
		"a.tork": "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel Dup {\n\tid Int @id @default(autoincrement())\n}\n",
	})("b.tork")
	// A name that shadows a built in type is refused outright, so
	// neither the block nor its members resolve to anything.
	c.open(uri, "enum Json {\n\ta\n}\n\nmodel String {\n\tid Int @id @default(autoincrement())\n\tbad Zzz\n}\n\nmodel Dup {\n\tid Int @id @default(autoincrement())\n}\n")

	for _, tt := range []struct {
		name string
		line int
		col  int
	}{
		{"an enum shadowing a built in type", 0, 6},
		{"a value of an enum that was refused", 1, 2},
		{"a model shadowing a built in type", 4, 8},
		{"a field whose type did not resolve", 6, 3},
		{"a model redeclared from another file", 9, 8},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var hover *lsp.Hover
			c.call("textDocument/hover", positionAt(uri, tt.line, tt.col), &hover)
			if hover != nil {
				t.Errorf("hover = %q, want null", hover.Contents.Value)
			}
			var locations []lsp.Location
			c.call("textDocument/definition", positionAt(uri, tt.line, tt.col), &locations)
			if len(locations) != 0 {
				t.Errorf("locations = %+v, want none", locations)
			}
		})
	}
}

func TestHover_ShowsAFieldsOwnDocComment(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n"+
		"\tid Int @id @default(autoincrement())\n"+
		"\t// The login handle.\n\t// Unique per tenant.\n\tname String @unique\n}\n")

	var hover *lsp.Hover
	c.call("textDocument/hover", positionAt(uri, 8, 3), &hover)
	if hover == nil {
		t.Fatal("no hover on the documented field")
	}
	for _, want := range []string{"The login handle.", "Unique per tenant.", "Column `name`"} {
		if !strings.Contains(hover.Contents.Value, want) {
			t.Errorf("hover is missing %q:\n%s", want, hover.Contents.Value)
		}
	}
}

// A dotted attribute name is rebuilt from its parts, so hovering the
// namespace and hovering the member both explain the whole attribute.
func TestHover_OnBothHalvesOfADottedAttribute(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id @default(autoincrement())\n\ts String @db.VarChar(30)\n}\n")

	for _, col := range []int{11, 14} {
		var hover *lsp.Hover
		c.call("textDocument/hover", positionAt(uri, 6, col), &hover)
		if hover == nil {
			t.Fatalf("no hover at column %d", col)
		}
		if !strings.Contains(hover.Contents.Value, "VARCHAR") {
			t.Errorf("hover at column %d does not explain @db.VarChar:\n%s", col, hover.Contents.Value)
		}
	}
}

// Positions inside a block but on nothing in particular, and inside
// argument lists of several elements, both walk further than the
// simple cases do.
func TestSymbols_AtPositionsBetweenConstructs(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\n"+
		"enum E {\n\ta\n\n}\n\n"+
		"model A {\n\talpha Int\n\tbeta  Int\n\tgamma String @default(dbgenerated(alpha))\n\n"+
		"\t@@id([alpha, beta])\n\t@@index([beta], name: \"idx\")\n}\n")

	t.Run("the second element of a list resolves", func(t *testing.T) {
		var locations []lsp.Location
		// "beta" inside @@id([alpha, beta])
		c.call("textDocument/definition", positionAt(uri, 14, 16), &locations)
		if len(locations) != 1 {
			t.Fatalf("locations = %+v, want the beta field", locations)
		}
		if locations[0].Range.Start.Line != 11 {
			t.Errorf("line = %d, want 11", locations[0].Range.Start.Line)
		}
	})

	t.Run("an identifier inside a call resolves", func(t *testing.T) {
		var hover *lsp.Hover
		// "alpha" inside dbgenerated(alpha)
		c.call("textDocument/hover", positionAt(uri, 12, 36), &hover)
		if hover == nil || !strings.Contains(hover.Contents.Value, "Column `alpha`") {
			t.Fatalf("hover = %+v, want the alpha field", hover)
		}
	})

	t.Run("blank space inside an enum has no symbol", func(t *testing.T) {
		var hover *lsp.Hover
		c.call("textDocument/hover", positionAt(uri, 6, 0), &hover)
		if hover != nil {
			t.Errorf("hover = %q, want null", hover.Contents.Value)
		}
	})

	t.Run("an index name argument has no symbol", func(t *testing.T) {
		var hover *lsp.Hover
		c.call("textDocument/hover", positionAt(uri, 15, 25), &hover)
		if hover != nil {
			t.Errorf("hover = %q, want null", hover.Contents.Value)
		}
	})
}

// Old style line endings are still line endings. The lexer counts a
// lone carriage return as one, so the position converter has to as
// well or every span below the first would land a line off.
func TestPositions_CarriageReturnOnlyLineEndings(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "datasource db {\r\tprovider = \"postgres\"\r}\r\rmodel A {\r\tid Int @id @default(autoincrement())\r\tbad Zzz\r}\r")

	ds := c.diagnosticsFor(uri)
	if len(ds) != 1 {
		t.Fatalf("diagnostics = %v, want the unknown type", messages(ds))
	}
	if ds[0].Range.Start.Line != 6 {
		t.Errorf("line = %d, want 6", ds[0].Range.Start.Line)
	}
	if ds[0].Range.Start.Character != 5 {
		t.Errorf("character = %d, want 5", ds[0].Range.Start.Character)
	}
}

// A percent escape that is part of the file's actual name must survive
// the round trip rather than being decoded into the character it names.
func TestURIs_PercentInAFileNameIsNotDecodedTwice(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"a%20b.tork": ""})("a%20b.tork")
	if !strings.Contains(uri, "a%2520b") {
		t.Fatalf("the test URI does not encode the percent: %s", uri)
	}
	c.open(uri, "model A {\n\tid Int @id @default(autoincrement())\n}\n")
	if ds := c.diagnosticsFor(uri); len(ds) != 1 {
		t.Errorf("diagnostics = %v, want the file to have been found", messages(ds))
	}
}

// A Windows style URI carries its drive letter inside the path, which
// has to be stripped back off before the path is usable.
func TestURIs_WindowsDriveLetters(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := "file:///c:/nonexistent/schema.tork"
	c.open(uri, "model A {\n\tid Int @id\n}\n")
	m := c.call("textDocument/hover", positionAt(uri, 0, 8), nil)
	if m.Error != nil {
		t.Errorf("error = %+v, want a null result for a path that is not there", m.Error)
	}
	if string(m.Result) != "null" {
		t.Errorf("result = %s, want null", m.Result)
	}
}

// An editor that dies mid session leaves the server writing into a
// closed pipe. That is an abnormal end, reported as one, rather than a
// server that spins on a stream nobody is reading.
func TestServer_ReportsWriteFailures(t *testing.T) {
	c := start(t)
	c.initialize()
	c.closeReader()

	// The reply to this has nowhere to go.
	c.request("shutdown", nil)
	select {
	case code := <-c.done:
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
	case <-timeout():
		t.Fatal("the server did not notice it could not write")
	}
	if c.errOut.Len() == 0 {
		t.Errorf("nothing was reported on stderr")
	}
}

// The same holds while answering a message the server could not even
// decode: the error response has nowhere to go either.
func TestServer_ReportsWriteFailuresOnErrorResponses(t *testing.T) {
	c := start(t)
	c.initialize()
	c.closeReader()

	c.sendRaw("{not json")
	select {
	case code := <-c.done:
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
	case <-timeout():
		t.Fatal("the server did not notice it could not write")
	}
}

// A write that fails while diagnostics are going out ends the session
// the same way any other broken pipe does.
func TestServer_ReportsWriteFailuresWhilePublishing(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.closeReader()

	c.open(uri, "model A {\n\tid Int @id @default(autoincrement())\n}\n")
	select {
	case code := <-c.done:
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
	case <-timeout():
		t.Fatal("the server did not notice it could not publish")
	}
}

// The same while taking diagnostics back for a file that has left the
// folder, which is a separate write from publishing the new ones.
func TestServer_ReportsWriteFailuresWhileClearing(t *testing.T) {
	c := start(t)
	c.initialize()
	dir := t.TempDir()
	write := func(name, text string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return "file://" + filepath.ToSlash(path)
	}
	const aFile = "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel User {\n\tid Int @id @default(autoincrement())\n}\n"
	aURI := write("a.tork", aFile)
	bURI := write("b.tork", "model User {\n\tid Int @id @default(autoincrement())\n}\n")

	c.open(aURI, aFile)
	if ds := c.diagnosticsFor(bURI); len(ds) != 1 {
		t.Fatalf("b.tork diagnostics = %v, want the redeclaration", messages(ds))
	}

	if err := os.Remove(filepath.Join(dir, "b.tork")); err != nil {
		t.Fatalf("removing b.tork: %v", err)
	}
	c.closeReader()
	c.notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": aURI}})

	select {
	case code := <-c.done:
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
	case <-timeout():
		t.Fatal("the server did not notice it could not clear")
	}
}

// Opening a file that is not part of any schema clears nothing,
// because nothing was ever reported about it.
func TestDiagnostics_ForAFileOutsideTheSchemaAreSilent(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"notes.md": "not a schema"})("notes.md")

	c.open(uri, "model A {\n\tid Int @id\n}\n")
	if _, published := c.diagnostics(uri); published {
		t.Errorf("a file outside the schema was published to")
	}
}

func TestFormatting_RejectsInvalidParams(t *testing.T) {
	c := start(t)
	c.initialize()
	c.sendRaw(encode(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "textDocument/formatting",
		"params":  "not an object",
	}))
	m := c.read()
	if m.Error == nil || m.Error.Code != -32602 {
		t.Errorf("error = %+v, want code -32602 (invalid params)", m.Error)
	}
}

func TestFraming_TruncatedMessagesEndTheSession(t *testing.T) {
	tests := map[string]string{
		"a header cut short": "Content-Length: 2",
		"a body cut short":   "Content-Length: 100\r\n\r\n{}",
	}
	for name, framing := range tests {
		t.Run(name, func(t *testing.T) {
			c := start(t)
			if _, err := c.in.Write([]byte(framing)); err != nil {
				t.Fatalf("writing: %v", err)
			}
			if err := c.in.Close(); err != nil {
				t.Fatalf("closing: %v", err)
			}
			select {
			case code := <-c.done:
				if code != 1 {
					t.Errorf("exit code = %d, want 1", code)
				}
			case <-timeout():
				t.Fatal("the server waited forever for a message that was cut off")
			}
			if c.errOut.Len() == 0 {
				t.Errorf("nothing was reported on stderr")
			}
		})
	}
}

// Windows line endings are two bytes for one break, and must not be
// counted as two.
func TestPositions_WindowsLineEndings(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "datasource db {\r\n\tprovider = \"postgres\"\r\n}\r\n\r\nmodel A {\r\n\tid Int @id @default(autoincrement())\r\n\tbad Zzz\r\n}\r\n")

	ds := c.diagnosticsFor(uri)
	if len(ds) != 1 {
		t.Fatalf("diagnostics = %v, want the unknown type", messages(ds))
	}
	if ds[0].Range.Start.Line != 6 || ds[0].Range.Start.Character != 5 {
		t.Errorf("start = %+v, want line 6 character 5", ds[0].Range.Start)
	}
}
