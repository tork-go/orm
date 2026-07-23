package gen_test

import (
	"testing"

	"github.com/tork-go/orm/gen/parser"
)

// The recovery suite pins down two promises at once: the exact
// diagnostic a mistake produces, and the partial tree that survives it.
// The second half is what the language server lives off, so a case that
// only checks the error message would miss the point.
func TestParse_MalformedInputs(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantDump  []string
		wantDiags []string
	}{
		{
			name:     "unknown top level word resyncs at next declaration",
			src:      "foo bar\nmodel A {\n\tid Int\n}\n",
			wantDump: []string{"bad", "model A", "  field id Int"},
			wantDiags: []string{
				`schema.tork:1:1: unexpected "foo" at top level (expected model, enum, or datasource)`,
			},
		},
		{
			name:     "generator block is rejected with the Go wiring hint",
			src:      "generator client {\n\tprovider = \"go\"\n}\nmodel A {\n\tid Int\n}\n",
			wantDump: []string{"bad", "model A", "  field id Int"},
			wantDiags: []string{
				`schema.tork:1:1: unsupported block "generator" (tork is configured in Go; wire gen/cli into your cmd/generate instead)`,
			},
		},
		{
			name:     "missing model name",
			src:      "model {\n\tid Int\n}\n",
			wantDump: []string{"bad"},
			wantDiags: []string{
				`schema.tork:1:7: expected a model name after "model", found "{"`,
			},
		},
		{
			name:     "missing opening brace",
			src:      "model A\nid Int\n",
			wantDump: []string{"bad"},
			wantDiags: []string{
				`schema.tork:1:8: expected { to open model "A", found end of line`,
			},
		},
		{
			name:     "missing closing brace before the next model",
			src:      "model A {\n\tid Int\nmodel B {\n\tx Int\n}\n",
			wantDump: []string{"model A", "  field id Int", "model B", "  field x Int"},
			wantDiags: []string{
				`schema.tork:3:1: model "A" is missing its closing }`,
			},
		},
		{
			name:     "missing closing brace at end of file",
			src:      "model A {\n\tid Int\n",
			wantDump: []string{"model A", "  field id Int"},
			wantDiags: []string{
				`schema.tork:3:1: model "A" is missing its closing }`,
			},
		},
		{
			name:     "field missing a type",
			src:      "model A {\n\tid\n}\n",
			wantDump: []string{"model A", "  field id <missing>"},
			wantDiags: []string{
				`schema.tork:2:2: field "id" is missing a type`,
			},
		},
		{
			name:     "junk after a field costs one diagnostic",
			src:      "model A {\n\tid Int 42\n}\n",
			wantDump: []string{"model A", "  field id Int"},
			wantDiags: []string{
				`schema.tork:2:9: unexpected "42" after field "id"`,
			},
		},
		{
			name: "unclosed attribute arguments bail at the closing brace",
			src:  "model A {\n\tid Int @default(autoincrement()\n\tname String\n}\n",
			wantDump: []string{
				"model A",
				"  field id Int attrs=[@default(autoincrement(), String)]",
			},
			wantDiags: []string{
				`schema.tork:3:2: expected , or ) in the arguments of @default, found "name"`,
				`schema.tork:4:1: missing ) to close the arguments of @default`,
			},
		},
		{
			name:     "block attribute sigil without a name",
			src:      "model A {\n\t@@\n}\n",
			wantDump: []string{"model A", "  @@"},
			wantDiags: []string{
				"schema.tork:2:2: expected an attribute name after @@",
			},
		},
		{
			name:     "attribute sigil without a name",
			src:      "model A {\n\tid Int @ @unique\n}\n",
			wantDump: []string{"model A", "  field id Int attrs=[@, @unique]"},
			wantDiags: []string{
				"schema.tork:2:9: expected an attribute name after @",
			},
		},
		{
			name:     "dot without an identifier in a dotted attribute",
			src:      "model A {\n\tid Int @db.(30)\n}\n",
			wantDump: []string{"model A", "  field id Int attrs=[@db(30)]"},
			wantDiags: []string{
				`schema.tork:2:13: expected an identifier after . in attribute @db`,
			},
		},
		{
			name:     "argument list closed by the model brace",
			src:      "model A {\n\tid Int @default(}\n}\n",
			wantDump: []string{"model A", "  field id Int attrs=[@default()]", "bad"},
			wantDiags: []string{
				"schema.tork:2:18: missing ) to close the arguments of @default",
				`schema.tork:3:1: unexpected "}" at top level (expected model, enum, or datasource)`,
			},
		},
		{
			name:     "missing value recovers at the comma",
			src:      "model A {\n\tid Int @default(,)\n}\n",
			wantDump: []string{"model A", "  field id Int attrs=[@default(<bad>)]"},
			wantDiags: []string{
				`schema.tork:2:18: expected a value in @default, found ","`,
			},
		},
		{
			name:     "unclosed call bails and reports both levels",
			src:      "model A {\n\tid Int @default(go(\n}\n",
			wantDump: []string{"model A", "  field id Int attrs=[@default(go())]"},
			wantDiags: []string{
				"schema.tork:3:1: missing ) to close the call go(...) in @default",
				"schema.tork:3:1: missing ) to close the arguments of @default",
			},
		},
		{
			name:     "junk between call arguments",
			src:      "model A {\n\tid Int @default(go(\"a\" \"b\"))\n}\n",
			wantDump: []string{"model A", `  field id Int attrs=[@default(go("a"))]`},
			wantDiags: []string{
				`schema.tork:2:25: expected , or ) in the call go(...), found string "b"`,
			},
		},
		{
			name:     "junk between list elements",
			src:      "model A {\n\t@@index([a b])\n}\n",
			wantDump: []string{"model A", "  @@index([a])"},
			wantDiags: []string{
				`schema.tork:2:13: expected , or ] in the list in @@index, found "b"`,
			},
		},
		{
			name:     "unclosed list bails at the closing parenthesis",
			src:      "model A {\n\t@@index([a)\n}\n",
			wantDump: []string{"model A", "  @@index([a])"},
			wantDiags: []string{
				"schema.tork:2:12: missing ] to close the list in @@index",
			},
		},
		{
			name:     "non field token inside a model",
			src:      "model A {\n\t\"x\" Int\n}\n",
			wantDump: []string{"model A"},
			wantDiags: []string{
				`schema.tork:2:2: expected a field or a @@ attribute in model "A", found string "x"`,
			},
		},
		{
			name:     "datasource entry missing the equals sign",
			src:      "datasource db {\n\tprovider \"postgres\"\n}\n",
			wantDump: []string{"datasource db"},
			wantDiags: []string{
				`schema.tork:2:11: expected = after "provider" in datasource "db", found string "postgres"`,
			},
		},
		{
			name:     "junk after a datasource entry",
			src:      "datasource db {\n\tprovider = \"postgres\" 42\n}\n",
			wantDump: []string{"datasource db", `  provider = "postgres"`},
			wantDiags: []string{
				`schema.tork:2:24: unexpected "42" after the "provider" entry`,
			},
		},
		{
			name:     "non entry token inside a datasource",
			src:      "datasource db {\n\t42\n}\n",
			wantDump: []string{"datasource db"},
			wantDiags: []string{
				`schema.tork:2:2: expected a key = value entry in datasource "db", found "42"`,
			},
		},
		{
			name:     "datasource missing its closing brace",
			src:      "datasource db {\n\tprovider = \"postgres\"\n",
			wantDump: []string{"datasource db", `  provider = "postgres"`},
			wantDiags: []string{
				`schema.tork:3:1: datasource "db" is missing its closing }`,
			},
		},
		{
			name:     "datasource closed by the next declaration",
			src:      "datasource db {\nmodel A {\n\tid Int\n}\n",
			wantDump: []string{"datasource db", "model A", "  field id Int"},
			wantDiags: []string{
				`schema.tork:2:1: datasource "db" is missing its closing }`,
			},
		},
		{
			name:     "junk after an enum value",
			src:      "enum E {\n\ta b\n}\n",
			wantDump: []string{"enum E", "  a"},
			wantDiags: []string{
				`schema.tork:2:4: unexpected "b" after enum value "a"`,
			},
		},
		{
			name:     "non value token inside an enum",
			src:      "enum E {\n\t42\n}\n",
			wantDump: []string{"enum E"},
			wantDiags: []string{
				`schema.tork:2:2: expected an enum value or a @@ attribute in enum "E", found "42"`,
			},
		},
		{
			name:     "enum missing its closing brace",
			src:      "enum E {\n\ta\n",
			wantDump: []string{"enum E", "  a"},
			wantDiags: []string{
				`schema.tork:3:1: enum "E" is missing its closing }`,
			},
		},
		{
			name:     "enum closed by the next declaration",
			src:      "enum E {\n\ta\nmodel A {\n\tid Int\n}\n",
			wantDump: []string{"enum E", "  a", "model A", "  field id Int"},
			wantDiags: []string{
				`schema.tork:3:1: enum "E" is missing its closing }`,
			},
		},
		{
			name:     "model keyword at end of file",
			src:      "model",
			wantDump: []string{"bad"},
			wantDiags: []string{
				`schema.tork:1:6: expected a model name after "model", found end of file`,
			},
		},
		{
			name:     "enum missing its opening brace",
			src:      "enum E\n",
			wantDump: []string{"bad"},
			wantDiags: []string{
				`schema.tork:1:7: expected { to open enum "E", found end of line`,
			},
		},
		{
			name:     "datasource missing its opening brace",
			src:      "datasource db\n",
			wantDump: []string{"bad"},
			wantDiags: []string{
				`schema.tork:1:14: expected { to open datasource "db", found end of line`,
			},
		},
		{
			name:     "junk line cut short by end of file",
			src:      "model A {\n\t42",
			wantDump: []string{"model A"},
			wantDiags: []string{
				`schema.tork:2:2: expected a field or a @@ attribute in model "A", found "42"`,
				`schema.tork:2:4: model "A" is missing its closing }`,
			},
		},
		{
			name:     "illegal character inside a model",
			src:      "model A {\n\t% id Int\n}\n",
			wantDump: []string{"model A"},
			wantDiags: []string{
				"schema.tork:2:2: unexpected character '%'",
				`schema.tork:2:2: expected a field or a @@ attribute in model "A", found "%"`,
			},
		},
		{
			name:     "value that never starts is consumed once",
			src:      "model A {\n\tid Int @default(?)\n}\n",
			wantDump: []string{"model A", "  field id Int attrs=[@default(<bad>)]"},
			wantDiags: []string{
				`schema.tork:2:18: expected a value in @default, found "?"`,
			},
		},
		{
			name:     "missing enum name",
			src:      "enum {\n\ta\n}\n",
			wantDump: []string{"bad"},
			wantDiags: []string{
				`schema.tork:1:6: expected an enum name after "enum", found "{"`,
			},
		},
		{
			name:     "missing datasource name",
			src:      "datasource {\n}\n",
			wantDump: []string{"bad"},
			wantDiags: []string{
				`schema.tork:1:12: expected a datasource name after "datasource", found "{"`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, diags := parser.Parse("schema.tork", []byte(tt.src))
			assertStrings(t, "dump", dumpFile(f), tt.wantDump)
			assertStrings(t, "diag", diagStrings(diags), tt.wantDiags)
		})
	}
}
