package gen_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm/gen/analyze"
	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/parser"
)

func TestAnalyze_NamingAndOddShapes(t *testing.T) {
	src := dsLine + `
model Category {
id String @id
xmlHTTPRequest Int
created_at DateTime
flag Boolean @default(false)
y Json @go.type("gopkg.in/yaml.v3.Node")
}
model Box {
id String @id
}
model Day {
id String @id
}
`
	s, diags := analyzeOne(t, src)
	if len(diags) != 0 {
		t.Fatalf("Analyze reported diagnostics:\n%s", strings.Join(diagStrings(diags), "\n"))
	}
	for model, table := range map[string]string{"Category": "categories", "Box": "boxes", "Day": "days"} {
		if m := modelNamed(t, s, model); m.TableName != table {
			t.Errorf("%s table = %q, want %q", model, m.TableName, table)
		}
	}
	cat := modelNamed(t, s, "Category")
	xml := fieldNamed(t, cat, "xmlHTTPRequest")
	if xml.GoName != "XmlHTTPRequest" || xml.ColumnName != "xml_http_request" {
		t.Errorf("xmlHTTPRequest = %s/%s, want XmlHTTPRequest/xml_http_request", xml.GoName, xml.ColumnName)
	}
	if flag := fieldNamed(t, cat, "flag"); flag.Default.SQL != "FALSE" {
		t.Errorf("flag default = %q, want FALSE", flag.Default.SQL)
	}
	if y := fieldNamed(t, cat, "y"); y.GoType != (analyze.GoTypeRef{ImportPath: "gopkg.in/yaml.v3", Name: "Node"}) {
		t.Errorf("y GoType = %+v, want gopkg.in/yaml.v3.Node", y.GoType)
	}
	if created := fieldNamed(t, cat, "created_at"); created.GoName != "CreatedAt" || created.ColumnName != "created_at" {
		t.Errorf("created_at = %s/%s, want CreatedAt/created_at", created.GoName, created.ColumnName)
	}
}

func TestAnalyze_SingleUniqueColumnReference(t *testing.T) {
	src := dsLine + `
model A {
bCode Int
b B @relation(fields: [bCode], references: [code])
}
model B {
id Int @id @default(autoincrement())
code Int @unique
a A
}
`
	_, diags := analyzeOne(t, src)
	if len(diags) != 0 {
		t.Fatalf("referencing a single @unique column should not warn:\n%s", strings.Join(diagStrings(diags), "\n"))
	}
}

func TestAnalyze_ReferencesCoveredByUniqueIndex(t *testing.T) {
	src := dsLine + `
model A {
xa Int
xb Int
r B @relation(fields: [xa, xb], references: [a, b])
}
model B {
id Int @id @default(autoincrement())
a Int
b Int
rs A[]
@@unique([a, b])
}
`
	_, diags := analyzeOne(t, src)
	if len(diags) != 0 {
		t.Fatalf("composite unique references should not warn:\n%s", strings.Join(diagStrings(diags), "\n"))
	}
}

func TestAnalyze_EdgeCaseErrors(t *testing.T) {
	runErrCases(t, []errCase{
		{
			name:  "map without arguments",
			files: one(dsLine + "\nmodel A {\ns String @map()\n}\n"),
			wantDiags: []string{
				`schema.tork:3:10: @map expects a string, e.g. @map("column_name")`,
			},
		},
		{
			name:  "varchar length out of range",
			files: one(dsLine + "\nmodel A {\ns String @db.VarChar(99999999999999999999)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:22: integer 99999999999999999999 is out of range",
			},
		},
		{
			name: "default on a relation field",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [id]) @default(now())\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				"schema.tork:4:48: @default cannot apply to a relation field",
			},
		},
		{
			name: "column attributes on a relation field",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [id]) @map(\"x\") @unique @index\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				"schema.tork:4:48: @map cannot apply to a relation field (a relation has no column)",
				"schema.tork:4:58: @unique cannot apply to a relation field (mark the foreign key field instead)",
				"schema.tork:4:66: @index cannot apply to a relation field (index the foreign key field instead)",
			},
		},
		{
			name:  "unique takes no arguments",
			files: one(dsLine + "\nmodel A {\ns String @unique(1)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:10: @unique takes no arguments",
			},
		},
		{
			name:  "index takes no arguments",
			files: one(dsLine + "\nmodel A {\ns String @index(1)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:10: @index takes no arguments",
			},
		},
		{
			name:  "db.Json takes no arguments",
			files: one(dsLine + "\nmodel A {\ndata Json @go.type(\"X\") @db.Json(1)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:25: @db.Json takes no arguments",
			},
		},
		{
			name:  "softDelete takes no arguments",
			files: one(dsLine + "\nmodel A {\nt DateTime? @softDelete(1)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:13: @softDelete takes no arguments",
			},
		},
		{
			name:  "numeric without a datasource",
			files: one("model A {\nd Decimal @db.Numeric(10, 2)\n}\n"),
			wantDiags: []string{
				`schema.tork:1:1: missing datasource block; add: datasource db { provider = "postgres" }`,
				`schema.tork:2:11: @db attributes require a datasource block (add: datasource db { provider = "postgres" })`,
			},
		},
		{
			name:  "numeric with named arguments",
			files: one(dsLine + "\nmodel A {\nd Decimal @db.Numeric(p: 10, s: 2)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:23: @db.Numeric does not take named arguments",
			},
		},
		{
			name:  "numeric with a bad precision argument",
			files: one(dsLine + "\nmodel A {\nd Decimal @db.Numeric(\"a\", 2)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:23: @db.Numeric needs precision and scale, e.g. @db.Numeric(10, 2)",
			},
		},
		{
			name:  "numeric with a bad scale argument",
			files: one(dsLine + "\nmodel A {\nd Decimal @db.Numeric(10, \"b\")\n}\n"),
			wantDiags: []string{
				"schema.tork:3:27: @db.Numeric needs precision and scale, e.g. @db.Numeric(10, 2)",
			},
		},
		{
			name:  "unknown bare default",
			files: one(dsLine + "\nmodel A {\nn Int @default(foo)\n}\n"),
			wantDiags: []string{
				`schema.tork:3:16: unknown default "foo"`,
			},
		},
		{
			name:  "literal default on an enum field",
			files: one(dsLine + "\nenum E {\na\n}\nmodel A {\ns E @default(5)\n}\n"),
			wantDiags: []string{
				"schema.tork:6:14: default 5 does not fit type E",
			},
		},
		{
			name:  "unknown type suggests an enum",
			files: one(dsLine + "\nenum Color {\nred\n}\nmodel A {\nc Colr\n}\n"),
			wantDiags: []string{
				`schema.tork:6:3: unknown type "Colr" (did you mean "Color"?)`,
			},
		},
		{
			name:  "go.type path without a type name",
			files: one(dsLine + "\nmodel A {\ndata Json @go.type(\"myapp/models\")\n}\n"),
			wantDiags: []string{
				`schema.tork:3:20: invalid Go type reference "myapp/models" (write "Name" for a type in the generated package, or "import/path.Name")`,
			},
		},
		{
			name:  "go.type with an invalid type name",
			files: one(dsLine + "\nmodel A {\ndata Json @go.type(\"pkg.1Bad\")\n}\n"),
			wantDiags: []string{
				`schema.tork:3:20: invalid Go type reference "pkg.1Bad" (write "Name" for a type in the generated package, or "import/path.Name")`,
			},
		},
		{
			name:  "go.type with an empty path segment",
			files: one(dsLine + "\nmodel A {\ndata Json @go.type(\"a//b.T\")\n}\n"),
			wantDiags: []string{
				`schema.tork:3:20: invalid Go type reference "a//b.T" (write "Name" for a type in the generated package, or "import/path.Name")`,
			},
		},
		{
			name:  "go.type with a bad path character",
			files: one(dsLine + "\nmodel A {\ndata Json @go.type(\"a b/c.T\")\n}\n"),
			wantDiags: []string{
				`schema.tork:3:20: invalid Go type reference "a b/c.T" (write "Name" for a type in the generated package, or "import/path.Name")`,
			},
		},
		{
			name:  "map with an empty string",
			files: one(dsLine + "\nmodel A {\ns String @map(\"\")\n}\n"),
			wantDiags: []string{
				`schema.tork:3:15: @map value "" is not a valid identifier`,
			},
		},
		{
			name:  "go.type with a bare dot",
			files: one(dsLine + "\nmodel A {\ndata Json @go.type(\".Bad\")\n}\n"),
			wantDiags: []string{
				`schema.tork:3:20: invalid Go type reference ".Bad" (write "Name" for a type in the generated package, or "import/path.Name")`,
			},
		},
		{
			name:  "index with a non list positional argument",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index(5)\n}\n"),
			wantDiags: []string{
				"schema.tork:4:9: @@index expects a field list, e.g. @@index([a, b])",
			},
		},
		{
			name: "relation map must be a string",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [id], map: 5)\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				"schema.tork:4:53: map: expects a string in @relation",
			},
		},
		{
			name: "relation map with an invalid identifier",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [id], map: \"1x\")\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:53: map: value "1x" is not a valid identifier`,
			},
		},
		{
			name: "Json field used as a foreign key",
			files: one(dsLine + "\nmodel A {\nj Json @go.type(\"X\")\nb B @relation(fields: [j], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:24: "j" in fields: cannot be a Json field`,
			},
		},
		{
			name: "join model with a broken belongs to",
			files: one(dsLine + "\nmodel A {\nid Int @id @default(autoincrement())\nts T[] @relation(\"M\", through: J)\n}\nmodel T {\nid Int @id @default(autoincrement())\nas A[] @relation(\"M\", through: J)\n}\nmodel J {\naId Int\ntId Int\na A @relation(fields: [aId], references: [nope])\nt T @relation(fields: [tId], references: [id])\n}\n"),
			wantDiags: []string{
				`schema.tork:13:43: model "A" has no field "nope" (referenced in references:)`,
			},
		},
		{
			name: "list field in references",
			files: one(dsLine + "\nmodel A {\nbTags Int\nb B @relation(fields: [bTags], references: [tags])\n}\nmodel B {\nid Int @id @default(autoincrement())\ntags String[]\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:45: "tags" in references: cannot be a list field`,
			},
		},
		{
			name: "join model missing the second endpoint",
			files: one(dsLine + "\nmodel A {\nid Int @id @default(autoincrement())\nts T[] @relation(\"M\", through: J)\n}\nmodel T {\nid Int @id @default(autoincrement())\nas A[] @relation(\"M\", through: J)\n}\nmodel J {\naId Int\na A @relation(fields: [aId], references: [id])\n}\n"),
			wantDiags: []string{
				`schema.tork:4:32: join model "J" needs a belongs to relation to "T" (with fields: and references:)`,
			},
		},
		{
			name:  "unique index name must be a string",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@unique([alpha], name: 5)\n}\n"),
			wantDiags: []string{
				"schema.tork:4:25: name: expects a string in @@unique",
			},
		},
		{
			name:  "index where must be a string",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index([alpha], where: 5)\n}\n"),
			wantDiags: []string{
				"schema.tork:4:25: where: expects a string in @@index",
			},
		},
		{
			name:  "check name must be a string",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@check(\"x\", name: 5)\n}\n"),
			wantDiags: []string{
				"schema.tork:4:20: name: expects a string in @@check",
			},
		},
	})
}

// TestAnalyze_ToleratesParserArtifacts feeds the analyzer trees that
// carry parse errors (Bad expressions, attributes without names,
// fields without types) and pins the contract that the parser's
// diagnostic is the only one: the analyzer must skip the wreckage in
// silence, not echo it.
func TestAnalyze_ToleratesParserArtifacts(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantDiags []string
	}{
		{name: "field without a type", src: dsLine + "\nmodel A {\nid\n}\n"},
		{name: "attribute without a name", src: dsLine + "\nmodel A {\nid Int @ @unique\n}\n"},
		{name: "bad expression in map", src: dsLine + "\nmodel A {\ns String @map(,)\n}\n"},
		{name: "bad expression in default", src: dsLine + "\nmodel A {\nn Int @default(,)\n}\n"},
		{name: "bad expression in varchar", src: dsLine + "\nmodel A {\ns String @db.VarChar(,)\n}\n"},
		{name: "bad expression in go.type", src: dsLine + "\nmodel A {\ndata Json @go.type(,)\n}\n"},
		{name: "bad expression in datasource", src: "datasource db {\nprovider = ,\n}\n"},
		{name: "bad expression in composite id", src: dsLine + "\nmodel A {\nalpha Int\n@@id([,])\n}\n"},
		{name: "bad expression in index name", src: dsLine + "\nmodel A {\nalpha Int\n@@index([alpha], name: ,)\n}\n"},
		{
			name: "bad expression in onDelete",
			src:  dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [id], onDelete: ,)\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n",
		},
		{name: "bad expression in check", src: dsLine + "\nmodel A {\nalpha Int\n@@check(,)\n}\n"},
		{
			name:      "bad expression in through",
			src:       dsLine + "\nmodel A {\nb B @relation(through: ,)\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n",
			wantDiags: []string{`schema.tork:3:5: one side of the relation between "A" and "B" must declare fields: and references:`},
		},
		{
			name:      "bad expression as the relation name",
			src:       dsLine + "\nmodel A {\nb B @relation(,)\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n",
			wantDiags: []string{`schema.tork:3:5: one side of the relation between "A" and "B" must declare fields: and references:`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, parseDiags := parser.Parse("schema.tork", []byte(tt.src))
			if len(parseDiags) == 0 {
				t.Fatalf("expected the source to carry parse errors; it parsed cleanly")
			}
			_, diags := analyze.Analyze([]*ast.File{f})
			assertStrings(t, "diag", diagStrings(diags), tt.wantDiags)
		})
	}
}
