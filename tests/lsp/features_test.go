package lsp_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm/lsp"
)

// The two file schema below is the fixture most feature tests work
// against. Splitting it deliberately: every cross file answer the
// server gives, from a diagnostic landing in the right file to a jump
// into another one, is only interesting because the files are apart.
const usersFile = `datasource db {
	provider = "postgres"
}

// Publication state.
enum Status {
	draft
	published
}

// An application account.
model User {
	id       Int     @id @default(autoincrement())
	username String  @unique @db.VarChar(30)
	email    String?
	posts    Post[]  @relation("UserPosts")

	@@index([username], name: "idx_users_username")
}
`

const postsFile = `model Post {
	id       Int    @id @default(autoincrement())
	title    String @db.VarChar(100)
	status   Status @default(draft)
	authorId Int
	author   User   @relation("UserPosts", fields: [authorId], references: [id], onDelete: Cascade)
}
`

func blogWorkspace(t *testing.T) func(string) string {
	t.Helper()
	return workspace(t, map[string]string{
		"users.tork": usersFile,
		"posts.tork": postsFile,
	})
}

func TestDidOpen_PublishesDiagnosticsForTheWholeFolder(t *testing.T) {
	c := start(t)
	c.initialize()
	const aFile = "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel User {\n\tid Int @id @default(autoincrement())\n}\n"
	uri := workspace(t, map[string]string{
		"a.tork": aFile,
		"b.tork": "model User {\n\tid Int @id @default(autoincrement())\n}\n",
	})
	c.open(uri("a.tork"), aFile)

	// The redeclaration is reported against b.tork, the second
	// declaration, even though a.tork is the file being edited.
	ds := c.diagnosticsFor(uri("b.tork"))
	if len(ds) != 1 {
		t.Fatalf("b.tork diagnostics = %v, want one", messages(ds))
	}
	if want := `model "User" redeclared (first declared at a.tork:5:7)`; ds[0].Message != want {
		t.Errorf("message = %q, want %q", ds[0].Message, want)
	}
	if ds[0].Severity != 1 {
		t.Errorf("severity = %d, want 1 (error)", ds[0].Severity)
	}
	if ds[0].Source != "tork" {
		t.Errorf("source = %q, want tork", ds[0].Source)
	}
	if ds[0].Range.Start.Line != 0 || ds[0].Range.Start.Character != 6 {
		t.Errorf("range start = %+v, want line 0 character 6", ds[0].Range.Start)
	}
}

// The buffer the editor holds is what the server analyzes, including
// before it has ever been saved. Without that, every answer would lag
// a save behind.
func TestDidChange_OverlayWinsOverDisk(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{
		"schema.tork": "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel User {\n\tid Int @id @default(autoincrement())\n}\n",
	})("schema.tork")

	c.open(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel User {\n\tid Int @id @default(autoincrement())\n\tbad Nope\n}\n")
	ds := c.diagnosticsFor(uri)
	if len(ds) != 1 || !strings.Contains(ds[0].Message, `unknown type "Nope"`) {
		t.Fatalf("diagnostics = %v, want the unsaved error", messages(ds))
	}

	// Typing the fix clears it, again without a save.
	c.change(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel User {\n\tid Int @id @default(autoincrement())\n\tgood String\n}\n")
	if ds := c.diagnosticsFor(uri); len(ds) != 0 {
		t.Errorf("diagnostics = %v, want an empty list clearing the squiggle", messages(ds))
	}
}

// Closing a document throws the buffer away, so what is on disk speaks
// again, which may be better or worse than what was being edited.
func TestDidClose_FallsBackToDisk(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{
		"schema.tork": "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel User {\n\tid Int @id @default(autoincrement())\n\tbad Nope\n}\n",
	})("schema.tork")

	c.open(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel User {\n\tid Int @id @default(autoincrement())\n}\n")
	if ds, published := c.diagnostics(uri); published && len(ds) != 0 {
		t.Fatalf("diagnostics = %v, want none for the clean buffer", messages(ds))
	}

	c.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}})
	ds := c.diagnosticsFor(uri)
	if len(ds) != 1 || !strings.Contains(ds[0].Message, `unknown type "Nope"`) {
		t.Errorf("diagnostics = %v, want the error that is still on disk", messages(ds))
	}
}

func TestDidSave_Republishes(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := blogWorkspace(t)("users.tork")
	c.open(uri, usersFile)
	c.notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": uri}})

	// Nothing is wrong, so nothing is published; the session simply
	// carries on answering.
	var hover *lsp.Hover
	c.call("textDocument/hover", positionAt(uri, 11, 8), &hover)
	if hover == nil {
		t.Error("the server stopped answering after a save")
	}
}

func TestDiagnostics_ClearWhenTheProblemIsFixed(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")

	c.open(uri, "model A {\n\tid Int @id @default(autoincrement())\n}\n")
	if ds := c.diagnosticsFor(uri); len(ds) != 1 {
		t.Fatalf("diagnostics = %v, want the missing datasource", messages(ds))
	}
	c.change(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id @default(autoincrement())\n}\n")
	if ds := c.diagnosticsFor(uri); len(ds) != 0 {
		t.Errorf("diagnostics = %v, want an empty list clearing the squiggle", messages(ds))
	}
}

func TestDiagnostics_ReportWarnings(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel A {\n\tid Int @id\n}\n")

	ds := c.diagnosticsFor(uri)
	if len(ds) != 1 {
		t.Fatalf("diagnostics = %v, want the identity warning", messages(ds))
	}
	if ds[0].Severity != 2 {
		t.Errorf("severity = %d, want 2 (warning)", ds[0].Severity)
	}
}

func TestCompletion_OffersTypesIncludingOnesFromOtherFiles(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := blogWorkspace(t)("posts.tork")
	c.open(uri, postsFile)

	var items []lsp.CompletionItem
	// Just after "status " on the status line, where a type belongs.
	c.call("textDocument/completion", positionAt(uri, 3, 10), &items)
	got := labels(items)
	for _, want := range []string{"String", "Int", "DateTime", "Json", "Status", "User", "Post"} {
		if !contains(got, want) {
			t.Errorf("completion is missing %q; got %v", want, got)
		}
	}
	for _, it := range items {
		if it.Label == "Status" && !strings.Contains(it.Documentation, "draft, published") {
			t.Errorf("the enum suggestion should list its values: %+v", it)
		}
	}
}

func TestCompletion_ContextSensitive(t *testing.T) {
	tests := []struct {
		name string
		// text is the whole document; the cursor sits at its end.
		text string
		want []string
	}{
		{
			name: "top level keywords",
			text: "mo",
			want: []string{"model", "enum", "datasource"},
		},
		{
			name: "datasource provider values",
			text: "datasource db {\n\tprovider = \"",
			want: []string{"postgres"},
		},
		{
			name: "field attributes",
			text: "model A {\n\tid Int @",
			want: []string{"id", "unique", "default", "relation", "map"},
		},
		{
			name: "native types after the db namespace",
			text: "model A {\n\ts String @db.",
			want: []string{"VarChar", "Text", "Numeric"},
		},
		{
			name: "block attributes",
			text: "model A {\n\tid Int @id\n\t@@",
			want: []string{"id", "unique", "index", "check", "map"},
		},
		{
			name: "field names inside an index list",
			text: "model A {\n\talpha Int @id\n\tbeta String\n\t@@index([",
			want: []string{"alpha", "beta"},
		},
		{
			name: "relation arguments",
			text: "model A {\n\tbId Int\n\tb B @relation(",
			want: []string{"fields", "references", "onDelete", "through"},
		},
		{
			name: "referential actions",
			text: "model A {\n\tbId Int\n\tb B @relation(fields: [bId], references: [id], onDelete: ",
			want: []string{"Cascade", "SetNull", "Restrict"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := start(t)
			c.initialize()
			uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
			c.open(uri, tt.text)

			lines := strings.Split(tt.text, "\n")
			var items []lsp.CompletionItem
			c.call("textDocument/completion",
				positionAt(uri, len(lines)-1, len([]rune(lines[len(lines)-1]))), &items)

			got := labels(items)
			for _, want := range tt.want {
				if !contains(got, want) {
					t.Errorf("completion is missing %q; got %v", want, got)
				}
			}
		})
	}
}

func TestCompletion_EveryItemCarriesDocumentation(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "model A {\n\tid Int @")

	var items []lsp.CompletionItem
	c.call("textDocument/completion", positionAt(uri, 1, 9), &items)
	if len(items) == 0 {
		t.Fatal("no completions offered")
	}
	for _, it := range items {
		if it.Documentation == "" {
			t.Errorf("%q has no documentation", it.Label)
		}
		if it.Detail == "" {
			t.Errorf("%q has no detail", it.Label)
		}
	}
}

func TestHover_ExplainsFieldsTypesAndAttributes(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := blogWorkspace(t)
	usersURI, postsURI := uri("users.tork"), uri("posts.tork")
	c.open(usersURI, usersFile)
	c.open(postsURI, postsFile)

	tests := []struct {
		name string
		uri  string
		line int
		col  int
		want []string
	}{
		{"a column field", usersURI, 13, 3, []string{"username String", "Column `username`", "unique", "VARCHAR"}},
		{"a nullable field", usersURI, 14, 3, []string{"email String?", "*string", "nullable"}},
		{"a relation field", usersURI, 15, 3, []string{"Has many `Post`", "UserPosts"}},
		{"a model name", usersURI, 11, 8, []string{"model User", "Table users", "An application account."}},
		{"an enum type reference", postsURI, 3, 12, []string{"enum Status", "draft, published"}},
		{"a model type reference", postsURI, 5, 12, []string{"model User", "Table users"}},
		{"a scalar type", postsURI, 2, 12, []string{"String", "TEXT"}},
		{"an attribute name", usersURI, 12, 19, []string{"@id", "primary key"}},
		{"a block attribute", usersURI, 17, 4, []string{"@@index", "index"}},
		{"a field named in an index", usersURI, 17, 12, []string{"username String", "Column `username`"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hover *lsp.Hover
			c.call("textDocument/hover", positionAt(tt.uri, tt.line, tt.col), &hover)
			if hover == nil {
				t.Fatalf("no hover at %d:%d", tt.line, tt.col)
			}
			if hover.Contents.Kind != "markdown" {
				t.Errorf("kind = %q, want markdown", hover.Contents.Kind)
			}
			for _, want := range tt.want {
				if !strings.Contains(hover.Contents.Value, want) {
					t.Errorf("hover is missing %q:\n%s", want, hover.Contents.Value)
				}
			}
			if hover.Range == nil {
				t.Errorf("hover carries no range to underline")
			}
		})
	}
}

func TestHover_IsNullWhereNothingIsUnderTheCursor(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := blogWorkspace(t)("users.tork")
	c.open(uri, usersFile)

	var hover *lsp.Hover
	// A blank line inside the model.
	c.call("textDocument/hover", positionAt(uri, 16, 0), &hover)
	if hover != nil {
		t.Errorf("hover = %+v, want null on an empty line", hover)
	}
}

func TestDefinition_JumpsAcrossFiles(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := blogWorkspace(t)
	usersURI, postsURI := uri("users.tork"), uri("posts.tork")
	c.open(usersURI, usersFile)
	c.open(postsURI, postsFile)

	tests := []struct {
		name    string
		from    string
		line    int
		col     int
		wantURI string
		want    lsp.Position
	}{
		{"a model type reference", postsURI, 5, 12, usersURI, lsp.Position{Line: 11, Character: 6}},
		{"an enum type reference", postsURI, 3, 12, usersURI, lsp.Position{Line: 5, Character: 5}},
		{"a field in fields:", postsURI, 5, 52, postsURI, lsp.Position{Line: 4, Character: 1}},
		{"a field in an index list", usersURI, 17, 12, usersURI, lsp.Position{Line: 13, Character: 1}},
		{"a relation field jumps to its counterpart", usersURI, 15, 3, postsURI, lsp.Position{Line: 5, Character: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var locations []lsp.Location
			c.call("textDocument/definition", positionAt(tt.from, tt.line, tt.col), &locations)
			if len(locations) != 1 {
				t.Fatalf("locations = %+v, want exactly one", locations)
			}
			if locations[0].URI != tt.wantURI {
				t.Errorf("uri = %s, want %s", locations[0].URI, tt.wantURI)
			}
			if locations[0].Range.Start != tt.want {
				t.Errorf("start = %+v, want %+v", locations[0].Range.Start, tt.want)
			}
		})
	}
}

// Standing on a declaration and asking for its definition is really
// asking where it is used, which is the only question left there.
func TestDefinition_OnAModelNameFindsItsReferences(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := blogWorkspace(t)
	c.open(uri("users.tork"), usersFile)
	c.open(uri("posts.tork"), postsFile)

	var locations []lsp.Location
	c.call("textDocument/definition", positionAt(uri("users.tork"), 11, 8), &locations)
	if len(locations) != 1 {
		t.Fatalf("locations = %+v, want the one reference in posts.tork", locations)
	}
	if locations[0].URI != uri("posts.tork") {
		t.Errorf("uri = %s, want posts.tork", locations[0].URI)
	}
}

func TestDefinition_IsNullWhereThereIsNothingToJumpTo(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := blogWorkspace(t)("posts.tork")
	c.open(uri, postsFile)

	for _, tt := range []struct {
		name string
		line int
		col  int
	}{
		{"a scalar type", 2, 12},
		{"a field's own name", 1, 3},
		{"an attribute name", 1, 12},
		{"whitespace", 6, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var locations []lsp.Location
			c.call("textDocument/definition", positionAt(uri, tt.line, tt.col), &locations)
			if len(locations) != 0 {
				t.Errorf("locations = %+v, want none", locations)
			}
		})
	}
}

func TestFormatting_ReturnsOneWholeDocumentEdit(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "model  A {\nid   Int @id\n}\n")

	var edits []lsp.TextEdit
	c.call("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      map[string]any{"tabSize": 4, "insertSpaces": false},
	}, &edits)

	if len(edits) != 1 {
		t.Fatalf("edits = %+v, want exactly one", edits)
	}
	if want := "model A {\n\tid Int @id\n}\n"; edits[0].NewText != want {
		t.Errorf("new text =\n%s\nwant =\n%s", edits[0].NewText, want)
	}
	if edits[0].Range.Start != (lsp.Position{}) {
		t.Errorf("the edit does not start at the top of the file: %+v", edits[0].Range.Start)
	}
	if edits[0].Range.End.Line != 3 {
		t.Errorf("the edit does not reach the end of the file: %+v", edits[0].Range.End)
	}
}

func TestFormatting_IsEmptyForCanonicalAndForBrokenSource(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
	}{
		{"already canonical", "model A {\n\tid Int @id\n}\n"},
		{"cannot be parsed", "model A {\n\tid\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := start(t)
			c.initialize()
			uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
			c.open(uri, tt.text)

			var edits []lsp.TextEdit
			c.call("textDocument/formatting", map[string]any{
				"textDocument": map[string]any{"uri": uri},
			}, &edits)
			if len(edits) != 0 {
				t.Errorf("edits = %+v, want none", edits)
			}
		})
	}
}

// Features still answer against a file the parser could not finish,
// which is the normal state of a document being typed into.
func TestFeatures_WorkOnSourceWithSyntaxErrors(t *testing.T) {
	c := start(t)
	c.initialize()
	uri := workspace(t, map[string]string{"schema.tork": ""})("schema.tork")
	c.open(uri, "datasource db {\n\tprovider = \"postgres\"\n}\n\nmodel User {\n\tid Int @id @default(autoincrement())\n\tname String\n\tbroken \n")

	ds := c.diagnosticsFor(uri)
	if len(ds) == 0 {
		t.Fatal("expected diagnostics for the unfinished model")
	}
	var hover *lsp.Hover
	c.call("textDocument/hover", positionAt(uri, 6, 3), &hover)
	if hover == nil || !strings.Contains(hover.Contents.Value, "Column `name`") {
		t.Errorf("hover = %+v, want the field above the break to still resolve", hover)
	}
	var items []lsp.CompletionItem
	c.call("textDocument/completion", positionAt(uri, 7, 8), &items)
	if len(labels(items)) == 0 {
		t.Errorf("completion gave up on a file with a syntax error")
	}
}

// A request about a document outside any schema directory is answered
// with null rather than an error, which is what a client expects when
// it asks about a file the server has nothing to say about.
func TestFeatures_AreNullForUnknownDocuments(t *testing.T) {
	c := start(t)
	c.initialize()
	missing := workspace(t, nil)("nowhere.tork")

	for _, method := range []string{
		"textDocument/hover", "textDocument/definition", "textDocument/completion",
	} {
		t.Run(method, func(t *testing.T) {
			m := c.call(method, positionAt(missing, 0, 0), nil)
			if m.Error != nil {
				t.Fatalf("error = %+v, want a null result", m.Error)
			}
			if string(m.Result) != "null" {
				t.Errorf("result = %s, want null", m.Result)
			}
		})
	}
	m := c.call("textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": missing},
	}, nil)
	if m.Error != nil || string(m.Result) != "null" {
		t.Errorf("formatting result = %s (error %+v), want null", m.Result, m.Error)
	}
}

func TestFeatures_IgnoreNonFileURIs(t *testing.T) {
	c := start(t)
	c.initialize()
	c.open("untitled:Untitled-1", "model A {\n\tid Int @id\n}\n")

	m := c.call("textDocument/hover", positionAt("untitled:Untitled-1", 0, 0), nil)
	if m.Error != nil {
		t.Fatalf("error = %+v, want a null result", m.Error)
	}
	if string(m.Result) != "null" {
		t.Errorf("result = %s, want null", m.Result)
	}
}
