package lsp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tork-go/orm/lsp"
)

// A file leaving the folder has to have its squiggles taken back. Left
// alone they would sit in the editor's problem list forever, pointing
// at a file that no longer exists.
func TestDiagnostics_ClearWhenAFileLeavesTheFolder(t *testing.T) {
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
	aURI := write("a.tork", "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel User {\n\tid Int @id @default(autoincrement())\n}\n")
	bURI := write("b.tork", "model User {\n\tid Int @id @default(autoincrement())\n}\n")

	c.open(aURI, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel User {\n\tid Int @id @default(autoincrement())\n}\n")
	if ds := c.diagnosticsFor(bURI); len(ds) != 1 {
		t.Fatalf("b.tork diagnostics = %v, want the redeclaration", messages(ds))
	}

	// Delete the offending file and nudge the server, which sweeps
	// the diagnostics of everything no longer in the folder.
	if err := os.Remove(filepath.Join(dir, "b.tork")); err != nil {
		t.Fatalf("removing b.tork: %v", err)
	}
	c.notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": aURI}})

	ds := c.diagnosticsFor(bURI)
	if len(ds) != 0 {
		t.Errorf("b.tork diagnostics = %v, want an empty list taking them back", messages(ds))
	}
}

// The document the editor is looking at can itself vanish, at which
// point the only honest report is an empty one.
func TestDiagnostics_ClearWhenTheOpenFileIsDeleted(t *testing.T) {
	c := start(t)
	c.initialize()
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.tork")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("writing schema: %v", err)
	}
	uri := "file://" + filepath.ToSlash(path)

	c.open(uri, "model A {\n\tid Int @id @default(autoincrement())\n}\n")
	if ds := c.diagnosticsFor(uri); len(ds) != 1 {
		t.Fatalf("diagnostics = %v, want the missing datasource", messages(ds))
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("removing the schema: %v", err)
	}
	c.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}})

	if ds := c.diagnosticsFor(uri); len(ds) != 0 {
		t.Errorf("diagnostics = %v, want an empty list", messages(ds))
	}
}

// Diagnostics for a clean file are only sent once it has had some, so
// a directory of valid schemas produces no traffic at all on every
// keystroke.
func TestDiagnostics_AreSilentForFilesThatWereNeverWrong(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{
		"a.tork": "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id @default(autoincrement())\n}\n",
		"b.tork": "model B {\n\tid Int @id @default(autoincrement())\n}\n",
	})
	c.open(uri("a.tork"), "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id @default(autoincrement())\n}\n")

	for _, name := range []string{"a.tork", "b.tork"} {
		if _, published := c.diagnostics(uri(name)); published {
			t.Errorf("%s was published to despite never having a problem", name)
		}
	}
}

// A schema directory the server cannot read is not an error to report
// at the client, since there is no document to report it against; the
// open buffer still analyzes on its own.
func TestWorkspace_UnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("file modes are not enforced for root")
	}
	c := start(t)
	c.initialize()
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.tork")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("writing schema: %v", err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Errorf("restoring the mode: %v", err)
		}
	})

	uri := "file://" + filepath.ToSlash(path)
	c.open(uri, "model A {\n\tid Int @id @default(autoincrement())\n}\n")
	ds := c.diagnosticsFor(uri)
	if len(ds) != 1 || !strings.Contains(ds[0].Message, "missing datasource") {
		t.Errorf("diagnostics = %v, want the buffer analyzed on its own", messages(ds))
	}
}

// Files in other directories are not part of this schema even when the
// editor has them open, which is what keeps one project's schemas from
// bleeding into another's.
func TestWorkspace_OverlaysFromOtherFoldersAreIgnored(t *testing.T) {
	c := start(t)
	c.initialize()
	here := workspace(t, map[string]string{
		"schema.tork": "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id @default(autoincrement())\n}\n",
	})
	elsewhere := workspace(t, map[string]string{"other.tork": ""})

	// A model of the same name, open in another directory entirely.
	c.open(elsewhere("other.tork"), "model A {\n\tid Int @id @default(autoincrement())\n}\n")
	c.open(here("schema.tork"), "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id @default(autoincrement())\n}\n")

	if _, published := c.diagnostics(here("schema.tork")); published {
		t.Errorf("a document in another folder was treated as part of this schema")
	}
}

// Non .tork files in the schema directory are not part of the schema,
// open or not.
func TestWorkspace_IgnoresOtherFileTypes(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{
		"schema.tork": "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id @default(autoincrement())\n}\n",
		"notes.md":    "model A { this is not a schema }",
	})
	c.open(uri("schema.tork"), "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id @default(autoincrement())\n}\n")
	if _, published := c.diagnostics(uri("schema.tork")); published {
		t.Errorf("a markdown file was read as part of the schema")
	}
}

func TestCompletion_InAModelThatFailedToDeclare(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	// The second declaration of a name is dropped, so no model exists
	// to take field names from; completion answers with an empty list
	// rather than failing.
	c.open(uri, "model A {\n\tid Int @id\n}\nmodel A {\n\tbeta Int\n\t@@index([")

	var items []lsp.CompletionItem
	c.call("textDocument/completion", positionAt(uri, 5, 10), &items)
	if len(items) != 0 {
		t.Errorf("completion = %v, want none", labels(items))
	}
}

func TestCompletion_InAModelWithOnlyRelationFields(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "model A {\n\tb B\n\t@@index([")

	var items []lsp.CompletionItem
	c.call("textDocument/completion", positionAt(uri, 2, 10), &items)
	if len(items) != 0 {
		t.Errorf("completion = %v, want none where the model has no columns", labels(items))
	}
}

// A relation attribute written across several lines still classifies
// correctly, since the arguments are read from the line the cursor is
// on rather than from a tree that may not exist yet.
func TestCompletion_RelationArgumentsAfterAComma(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "model A {\n\tbId Int\n\tb B @relation(fields: [bId], ")

	var items []lsp.CompletionItem
	c.call("textDocument/completion", positionAt(uri, 2, 30), &items)
	if got := labels(items); !contains(got, "references") {
		t.Errorf("completion is missing references:; got %v", got)
	}
}

func TestDefinition_OnAModelNameThatFailedToDeclare(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "model A {\n\tid Int @id\n}\nmodel A {\n\tid Int @id\n}\n")

	var locations []lsp.Location
	c.call("textDocument/definition", positionAt(uri, 3, 8), &locations)
	if len(locations) != 0 {
		t.Errorf("locations = %+v, want none", locations)
	}
}

func TestDefinition_OnAModelNothingReferences(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel Lonely {\n\tid Int @id @default(autoincrement())\n}\n")

	var locations []lsp.Location
	c.call("textDocument/definition", positionAt(uri, 4, 8), &locations)
	if len(locations) != 0 {
		t.Errorf("locations = %+v, want none for a model nothing points at", locations)
	}
}
