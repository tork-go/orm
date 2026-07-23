package gen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tork-go/orm/gen/format"
)

// assertFormat checks the formatted output and, in the same breath,
// that formatting it again changes nothing. Idempotency is what makes
// format on save safe, and it is cheap enough to assert on every case
// rather than in one test that could drift out of step.
func assertFormat(t *testing.T, src, want string) {
	t.Helper()
	got, diags := format.Source("schema.tork", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("Source reported diagnostics:\n%s", strings.Join(diagStrings(diags), "\n"))
	}
	if string(got) != want {
		t.Errorf("Source() =\n%s\nwant =\n%s", got, want)
	}
	again, diags := format.Source("schema.tork", got)
	if len(diags) != 0 {
		t.Fatalf("reformatting reported diagnostics:\n%s", strings.Join(diagStrings(diags), "\n"))
	}
	if string(again) != string(got) {
		t.Errorf("formatting is not idempotent:\nfirst =\n%s\nsecond =\n%s", got, again)
	}
}

func TestFormat_AlignsAndCanonicalizes(t *testing.T) {
	src := `datasource   db   {
provider="postgres"
}
// Publication state.
enum PostStatus{
draft
published
@@map( "post_status" )
}
model User{
// The login handle.
id Int @id @default( autoincrement() )
username String @unique() @db.VarChar( 30 )
email String?
posts Post[] @relation("UserPosts")
@@index([username],name:"idx_users_username")
}
`
	want := `datasource db {
	provider = "postgres"
}

// Publication state.
enum PostStatus {
	draft
	published

	@@map("post_status")
}

model User {
	// The login handle.
	id       Int     @id @default(autoincrement())
	username String  @unique @db.VarChar(30)
	email    String?
	posts    Post[]  @relation("UserPosts")

	@@index([username], name: "idx_users_username")
}
`
	assertFormat(t, src, want)
}

func TestFormat_KeepsCommentsAndParagraphBreaks(t *testing.T) {
	src := `// Leading file comment.

// Documents the model.
model A {
	// Documents id.
	id Int @id // trailing on id

	// A paragraph break above name.
	name String

	// Floating, attached to nothing.

	@@index([name]) // trailing on the index
}

// Trailing file comment.
`
	want := `// Leading file comment.

// Documents the model.
model A {
	// Documents id.
	id   Int    @id // trailing on id

	// A paragraph break above name.
	name String

	// Floating, attached to nothing.

	@@index([name]) // trailing on the index
}

// Trailing file comment.
`
	assertFormat(t, src, want)
}

func TestFormat_NormalizesCommentSpacingAndEmptyComments(t *testing.T) {
	src := "//no space\n//   too much space\n//\nmodel A {\n\tid Int @id\n}\n"
	want := "// no space\n// too much space\n//\nmodel A {\n\tid Int @id\n}\n"
	assertFormat(t, src, want)
}

func TestFormat_RoundTripsEveryValueShape(t *testing.T) {
	src := "model A {\n" +
		"n Int @default(-1) @db.VarChar(30)\n" +
		"f Float @default(-1.5)\n" +
		"b Boolean @default(true) \n" +
		"o Boolean @default(false)\n" +
		"s String @default(\"a\\\"b\\\\c\\nd\\te\")\n" +
		"e String @dummy([a, b], on: [\"x\", \"y\"], f: g(1, \"z\"))\n" +
		"}\n"
	want := "model A {\n" +
		"\tn Int     @default(-1) @db.VarChar(30)\n" +
		"\tf Float   @default(-1.5)\n" +
		"\tb Boolean @default(true)\n" +
		"\to Boolean @default(false)\n" +
		"\ts String  @default(\"a\\\"b\\\\c\\nd\\te\")\n" +
		"\te String  @dummy([a, b], on: [\"x\", \"y\"], f: g(1, \"z\"))\n" +
		"}\n"
	assertFormat(t, src, want)
}

func TestFormat_HandlesEmptyBlocksAndFiles(t *testing.T) {
	assertFormat(t, "", "")
	assertFormat(t, "\n\n", "")
	assertFormat(t, "model A {\n}\n", "model A {\n}\n")
	assertFormat(t, "enum E {\n}\n", "enum E {\n}\n")
	assertFormat(t, "datasource db {\n}\n", "datasource db {\n}\n")
}

func TestFormat_KeepsCommentsInsideOtherwiseEmptyBlocks(t *testing.T) {
	src := "datasource db {\n// only a comment\nprovider = \"postgres\"\n}\n"
	want := "datasource db {\n\tprovider = \"postgres\"\n\t// only a comment\n}\n"
	assertFormat(t, src, want)
}

func TestFormat_ConsecutiveBlockAttributesShareOneBlankLine(t *testing.T) {
	src := "model A {\nid Int @id\n@@index([id])\n@@check(\"id > 0\")\n}\n"
	want := "model A {\n\tid Int @id\n\n\t@@index([id])\n\t@@check(\"id > 0\")\n}\n"
	assertFormat(t, src, want)
}

func TestFormat_LeavesMalformedSourceUntouched(t *testing.T) {
	src := "model A {\n\tid\n"
	got, diags := format.Source("schema.tork", []byte(src))
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for malformed source")
	}
	if string(got) != src {
		t.Errorf("malformed source was rewritten:\ngot =\n%s\nwant =\n%s", got, src)
	}
}

// TestFormat_TestdataSchemasAreCanonical keeps the schemas the golden
// tests generate from in canonical form. They double as the worked
// examples a reader learns the language from, so they must look the
// way the formatter would write them.
func TestFormat_TestdataSchemasAreCanonical(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*", "schema", "*.tork"))
	if err != nil {
		t.Fatalf("listing schemas: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("found no testdata schemas")
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			got, diags := format.Source(filepath.Base(path), src)
			if len(diags) != 0 {
				t.Fatalf("diagnostics:\n%s", strings.Join(diagStrings(diags), "\n"))
			}
			if string(got) != string(src) {
				t.Errorf("%s is not canonical; run make fmt-schemas\ngot =\n%s\nwant =\n%s", path, got, src)
			}
		})
	}
}
