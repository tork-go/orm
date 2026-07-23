package gen_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/tork-go/orm/gen/analyze"
	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/diag"
	"github.com/tork-go/orm/gen/parser"
)

// analyzeFiles parses and analyzes a schema given as file name to
// source. It fails the test on parser diagnostics, so analyzer tests
// can only ever exercise the analyzer.
func analyzeFiles(t *testing.T, files map[string]string) (*analyze.Schema, []diag.Diagnostic) {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var parsed []*ast.File
	for _, name := range names {
		f, ds := parser.Parse(name, []byte(files[name]))
		if len(ds) != 0 {
			t.Fatalf("parse diagnostics in %s before analysis:\n%s", name, strings.Join(diagStrings(ds), "\n"))
		}
		parsed = append(parsed, f)
	}
	return analyze.Analyze(parsed)
}

func analyzeOne(t *testing.T, src string) (*analyze.Schema, []diag.Diagnostic) {
	t.Helper()
	return analyzeFiles(t, map[string]string{"schema.tork": src})
}

// modelNamed fails the test rather than returning nil, keeping the
// call sites free of nil checks.
func modelNamed(t *testing.T, s *analyze.Schema, name string) *analyze.Model {
	t.Helper()
	for _, m := range s.Models {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("schema has no model %q", name)
	return nil
}

func fieldNamed(t *testing.T, m *analyze.Model, name string) *analyze.Field {
	t.Helper()
	if f := m.FieldNamed(name); f != nil {
		return f
	}
	t.Fatalf("model %q has no field %q", m.Name, name)
	return nil
}

const dsLine = `datasource db { provider = "postgres" }`

func TestAnalyze_ResolvesAWellFormedSchema(t *testing.T) {
	src := dsLine + `

// Publication state.
enum Status {
	draft
	published

	@@map("post_status")
}

model User {
	id        Int       @id @default(autoincrement())
	username  String    @unique @db.VarChar(30)
	email     String?   @map("mail")
	age       Int32     @default(18)
	balance   Decimal   @db.Numeric(10, 2) @default(0)
	ratio     Double    @default(-1.5)
	active    Boolean   @default(true)
	bio       String    @db.Text
	tags      String[]  @db.VarChar(20)
	scores    Int[]?
	profile   Json      @go.type("Profile") @db.Json
	settings  Json?     @go.type("myapp/config.Settings")
	token     Uuid      @unique @default(uuid())
	ref       Uuid      @default(go("myapp/ids.NewRef"))
	slug      String    @default(dbgenerated("md5(random()::text)"))
	createdAt DateTime  @default(now())
	deletedAt DateTime? @softDelete
	nick      String    @default("anon") @index
	status    Status    @default(draft)
	posts     Post[]    @relation("UserPosts")

	@@index([username, createdAt], name: "idx_user_name_date", where: "deleted_at IS NULL")
	@@index(on: ["lower(mail)"], name: "idx_user_lower_mail")
	@@unique([age, ratio], name: "u_age_ratio")
	@@check("age >= 0", name: "ck_age")
}

model Post {
	id       Int    @id @default(autoincrement())
	title    String @db.VarChar(100)
	authorId Int
	author   User   @relation("UserPosts", fields: [authorId], references: [id], onDelete: Cascade, onUpdate: Restrict, map: "fk_posts_author")

	@@map("blog_posts")
}
`
	s, diags := analyzeOne(t, src)
	if len(diags) != 0 {
		t.Fatalf("Analyze reported diagnostics on a well formed schema:\n%s", strings.Join(diagStrings(diags), "\n"))
	}

	if s.Datasource.Name != "db" || s.Datasource.Provider != "postgres" {
		t.Errorf("Datasource = %+v, want db/postgres", s.Datasource)
	}
	if len(s.Models) != 2 || s.Models[0].Name != "Post" || s.Models[1].Name != "User" {
		t.Fatalf("Models are not sorted by name: got %d models", len(s.Models))
	}

	e := s.Enums[0]
	if e.Name != "Status" || e.DBName != "post_status" {
		t.Errorf("enum = %s/%s, want Status/post_status", e.Name, e.DBName)
	}
	if len(e.Values) != 2 || e.Values[0] != "draft" || e.Values[1] != "published" {
		t.Errorf("enum values = %v, want [draft published]", e.Values)
	}
	if e.Doc != "Publication state." {
		t.Errorf("enum doc = %q", e.Doc)
	}

	user := modelNamed(t, s, "User")
	post := modelNamed(t, s, "Post")
	if user.TableName != "users" {
		t.Errorf("User table = %q, want users", user.TableName)
	}
	if post.TableName != "blog_posts" {
		t.Errorf("Post table = %q, want blog_posts (@@map)", post.TableName)
	}

	id := fieldNamed(t, user, "id")
	if !id.IsID || id.GoName != "ID" || id.ColumnName != "id" {
		t.Errorf("id = %+v, want IsID/ID/id", id)
	}
	if id.Default == nil || id.Default.Kind != analyze.DefaultAutoincrement {
		t.Errorf("id default = %+v, want autoincrement", id.Default)
	}
	if len(user.PrimaryKey) != 1 || user.PrimaryKey[0] != id {
		t.Errorf("User primary key = %v, want [id]", user.PrimaryKey)
	}

	username := fieldNamed(t, user, "username")
	if !username.Unique || username.VarcharLen != 30 {
		t.Errorf("username = unique %t len %d, want true 30", username.Unique, username.VarcharLen)
	}
	email := fieldNamed(t, user, "email")
	if !email.Optional || email.ColumnName != "mail" {
		t.Errorf("email = optional %t column %q, want true mail", email.Optional, email.ColumnName)
	}

	for _, tt := range []struct {
		field string
		kind  analyze.DefaultKind
		sql   string
	}{
		{"age", analyze.DefaultLiteral, "18"},
		{"balance", analyze.DefaultLiteral, "0"},
		{"ratio", analyze.DefaultLiteral, "-1.5"},
		{"active", analyze.DefaultLiteral, "TRUE"},
		{"slug", analyze.DefaultDBGenerated, "md5(random()::text)"},
		{"createdAt", analyze.DefaultNow, "now()"},
		{"nick", analyze.DefaultLiteral, "'anon'"},
		{"status", analyze.DefaultLiteral, "'draft'"},
	} {
		f := fieldNamed(t, user, tt.field)
		if f.Default == nil || f.Default.Kind != tt.kind || f.Default.SQL != tt.sql {
			t.Errorf("%s default = %+v, want kind %d SQL %q", tt.field, f.Default, tt.kind, tt.sql)
		}
	}

	balance := fieldNamed(t, user, "balance")
	if balance.NumericPrecision != 10 || balance.NumericScale != 2 {
		t.Errorf("balance numeric = (%d, %d), want (10, 2)", balance.NumericPrecision, balance.NumericScale)
	}
	tags := fieldNamed(t, user, "tags")
	if !tags.List || tags.VarcharLen != 20 {
		t.Errorf("tags = list %t len %d, want true 20", tags.List, tags.VarcharLen)
	}
	scores := fieldNamed(t, user, "scores")
	if !scores.List || !scores.Optional {
		t.Errorf("scores = list %t optional %t, want both true", scores.List, scores.Optional)
	}

	profile := fieldNamed(t, user, "profile")
	if !profile.JSONText || profile.GoType != (analyze.GoTypeRef{Name: "Profile"}) {
		t.Errorf("profile = jsonText %t goType %+v, want true/Profile", profile.JSONText, profile.GoType)
	}
	settings := fieldNamed(t, user, "settings")
	if settings.JSONText || settings.GoType != (analyze.GoTypeRef{ImportPath: "myapp/config", Name: "Settings"}) {
		t.Errorf("settings = %+v, want jsonb myapp/config.Settings", settings.GoType)
	}

	token := fieldNamed(t, user, "token")
	if token.Default.Kind != analyze.DefaultUUID || token.Default.GoFunc != (analyze.GoTypeRef{ImportPath: "github.com/google/uuid", Name: "New"}) {
		t.Errorf("token default = %+v, want uuid.New", token.Default)
	}
	ref := fieldNamed(t, user, "ref")
	if ref.Default.Kind != analyze.DefaultGoFunc || ref.Default.GoFunc != (analyze.GoTypeRef{ImportPath: "myapp/ids", Name: "NewRef"}) {
		t.Errorf("ref default = %+v, want myapp/ids.NewRef", ref.Default)
	}

	deletedAt := fieldNamed(t, user, "deletedAt")
	if !deletedAt.SoftDelete || user.SoftDelete != deletedAt {
		t.Errorf("deletedAt soft delete not registered")
	}
	if nick := fieldNamed(t, user, "nick"); !nick.Indexed {
		t.Errorf("nick.Indexed = false, want true")
	}
	status := fieldNamed(t, user, "status")
	if status.Type.Kind != analyze.TypeEnum || status.Type.Enum != e {
		t.Errorf("status type = %+v, want enum Status", status.Type)
	}

	if len(user.Indexes) != 3 {
		t.Fatalf("User indexes = %d, want 3", len(user.Indexes))
	}
	named := user.Indexes[0]
	if named.Name != "idx_user_name_date" || named.Where != "deleted_at IS NULL" || len(named.Fields) != 2 || named.Unique {
		t.Errorf("index 0 = %+v, want named partial two column", named)
	}
	expr := user.Indexes[1]
	if len(expr.Expressions) != 1 || expr.Expressions[0] != "lower(mail)" || len(expr.Fields) != 0 || expr.Name != "idx_user_lower_mail" {
		t.Errorf("index 1 = %+v, want a named expression index", expr)
	}
	uniq := user.Indexes[2]
	if !uniq.Unique || uniq.Name != "u_age_ratio" {
		t.Errorf("index 2 = %+v, want unique u_age_ratio", uniq)
	}
	if len(user.Checks) != 1 || user.Checks[0].Expression != "age >= 0" || user.Checks[0].Name != "ck_age" {
		t.Errorf("checks = %+v, want ck_age", user.Checks)
	}

	authorID := fieldNamed(t, post, "authorId")
	if authorID.GoName != "AuthorID" || authorID.ColumnName != "author_id" {
		t.Errorf("authorId = %s/%s, want AuthorID/author_id", authorID.GoName, authorID.ColumnName)
	}
	author := fieldNamed(t, post, "author")
	rel := author.Relation
	if rel == nil || rel.Kind != analyze.RelBelongsTo {
		t.Fatalf("author relation = %+v, want belongs to", rel)
	}
	if rel.Target != user || rel.Inverse != fieldNamed(t, user, "posts") {
		t.Errorf("author relation target/inverse wrong")
	}
	if len(rel.Fields) != 1 || rel.Fields[0] != authorID || len(rel.References) != 1 || rel.References[0] != id {
		t.Errorf("author relation keys wrong: %+v", rel)
	}
	if rel.OnDelete != "Cascade" || rel.OnUpdate != "Restrict" || rel.FKName != "fk_posts_author" {
		t.Errorf("author relation actions = %s/%s/%s", rel.OnDelete, rel.OnUpdate, rel.FKName)
	}
	posts := fieldNamed(t, user, "posts")
	if posts.Relation == nil || posts.Relation.Kind != analyze.RelHasMany || posts.Relation.Target != post {
		t.Fatalf("posts relation = %+v, want has many of Post", posts.Relation)
	}
	if len(posts.Relation.Fields) != 1 || posts.Relation.Fields[0] != authorID {
		t.Errorf("posts relation must carry the owner side key for Via wiring")
	}
}

func TestAnalyze_ResolvesRelationShapes(t *testing.T) {
	src := dsLine + `
model User {
	id      Int      @id @default(autoincrement())
	profile Profile? @relation("UserProfile")
}
model Profile {
	id     Int  @id @default(autoincrement())
	userId Int  @unique
	user   User @relation("UserProfile", fields: [userId], references: [id])
}
model Category {
	id       Int        @id @default(autoincrement())
	parentId Int?
	parent   Category?  @relation("Tree", fields: [parentId], references: [id])
	children Category[] @relation("Tree")
}
model Post {
	id   Int   @id @default(autoincrement())
	tags Tag[] @relation("PostTags", through: PostTag)
}
model Tag {
	id    Int    @id @default(autoincrement())
	posts Post[] @relation("PostTags", through: PostTag)
}
model PostTag {
	postId Int
	tagId  Int
	post   Post @relation(fields: [postId], references: [id])
	tag    Tag  @relation(fields: [tagId], references: [id])

	@@id([postId, tagId])
}
`
	s, diags := analyzeOne(t, src)
	if len(diags) != 0 {
		t.Fatalf("Analyze reported diagnostics:\n%s", strings.Join(diagStrings(diags), "\n"))
	}

	user := modelNamed(t, s, "User")
	profile := modelNamed(t, s, "Profile")
	up := fieldNamed(t, user, "profile")
	if up.Relation.Kind != analyze.RelHasOne || up.Relation.Target != profile {
		t.Errorf("User.profile = %+v, want has one of Profile", up.Relation)
	}
	if pu := fieldNamed(t, profile, "user"); pu.Relation.Kind != analyze.RelBelongsTo || pu.Relation.Inverse != up {
		t.Errorf("Profile.user = %+v, want belongs to inverse of profile", pu.Relation)
	}

	cat := modelNamed(t, s, "Category")
	parent := fieldNamed(t, cat, "parent")
	children := fieldNamed(t, cat, "children")
	if parent.Relation.Kind != analyze.RelBelongsTo || parent.Relation.Target != cat || parent.Relation.Inverse != children {
		t.Errorf("self relation parent = %+v", parent.Relation)
	}
	if children.Relation.Kind != analyze.RelHasMany || children.Relation.Inverse != parent {
		t.Errorf("self relation children = %+v", children.Relation)
	}

	post := modelNamed(t, s, "Post")
	tag := modelNamed(t, s, "Tag")
	joins := modelNamed(t, s, "PostTag")
	tags := fieldNamed(t, post, "tags")
	if tags.Relation.Kind != analyze.RelManyToMany || tags.Relation.Through != joins {
		t.Fatalf("Post.tags = %+v, want many to many through PostTag", tags.Relation)
	}
	if tags.Relation.ThroughLocal != fieldNamed(t, joins, "postId") || tags.Relation.ThroughForeign != fieldNamed(t, joins, "tagId") {
		t.Errorf("Post.tags join keys = %+v", tags.Relation)
	}
	posts := fieldNamed(t, tag, "posts")
	if posts.Relation.ThroughLocal != fieldNamed(t, joins, "tagId") || posts.Relation.ThroughForeign != fieldNamed(t, joins, "postId") {
		t.Errorf("Tag.posts join keys mirrored wrong: %+v", posts.Relation)
	}
	if len(joins.PrimaryKey) != 2 || !fieldNamed(t, joins, "postId").IsID || !fieldNamed(t, joins, "tagId").IsID {
		t.Errorf("PostTag composite key not applied")
	}
}

func TestAnalyze_MergesFilesIntoOneNamespace(t *testing.T) {
	s, diags := analyzeFiles(t, map[string]string{
		"a.tork": dsLine + `
model User {
	id    Int    @id @default(autoincrement())
	role  Role
	posts Post[] @relation("UP")
}
`,
		"b.tork": `enum Role {
	admin
	member
}
model Post {
	id       Int  @id @default(autoincrement())
	authorId Int
	author   User @relation("UP", fields: [authorId], references: [id])
}
`,
	})
	if len(diags) != 0 {
		t.Fatalf("Analyze reported diagnostics:\n%s", strings.Join(diagStrings(diags), "\n"))
	}
	user := modelNamed(t, s, "User")
	if role := fieldNamed(t, user, "role"); role.Type.Enum == nil || role.Type.Enum.Name != "Role" {
		t.Errorf("role did not resolve to the enum in b.tork")
	}
	if posts := fieldNamed(t, user, "posts"); posts.Relation == nil || posts.Relation.Target.Name != "Post" {
		t.Errorf("cross file relation did not resolve")
	}
}

func TestAnalyze_ReportsCrossFileRedeclaration(t *testing.T) {
	_, diags := analyzeFiles(t, map[string]string{
		"a.tork": dsLine + "\nmodel User {\nid Int @id @default(autoincrement())\n}\n",
		"c.tork": "model User {\nid Int @id @default(autoincrement())\n}\n",
	})
	want := []string{`c.tork:1:7: model "User" redeclared (first declared at a.tork:2:7)`}
	assertStrings(t, "diag", diagStrings(diags), want)
}

func TestAnalyze_DatasourceRules(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		wantDiags []string
	}{
		{
			name:  "missing datasource",
			files: map[string]string{"schema.tork": "model A {\nid Int @id @default(autoincrement())\n}\n"},
			wantDiags: []string{
				`schema.tork:1:1: missing datasource block; add: datasource db { provider = "postgres" }`,
			},
		},
		{
			name: "duplicate datasource across files",
			files: map[string]string{
				"a.tork": dsLine + "\n",
				"b.tork": dsLine + "\n",
			},
			wantDiags: []string{
				"b.tork:1:12: duplicate datasource block (first declared at a.tork:1:12)",
			},
		},
		{
			name:  "missing provider",
			files: map[string]string{"schema.tork": "datasource db {\n}\n"},
			wantDiags: []string{
				`schema.tork:1:12: datasource "db" is missing its provider (add: provider = "postgres")`,
			},
		},
		{
			name:  "unknown provider",
			files: map[string]string{"schema.tork": "datasource db {\n\tprovider = \"mysql\"\n}\n"},
			wantDiags: []string{
				`schema.tork:2:13: unknown provider "mysql" (supported: "postgres")`,
			},
		},
		{
			name:  "misspelled provider gets a suggestion",
			files: map[string]string{"schema.tork": "datasource db {\n\tprovider = \"postgre\"\n}\n"},
			wantDiags: []string{
				`schema.tork:2:13: unknown provider "postgre" (supported: "postgres") (did you mean "postgres"?)`,
			},
		},
		{
			name:  "provider must be a string",
			files: map[string]string{"schema.tork": "datasource db {\n\tprovider = 42\n}\n"},
			wantDiags: []string{
				`schema.tork:2:2: provider must be a string, e.g. provider = "postgres"`,
			},
		},
		{
			name:  "unknown setting",
			files: map[string]string{"schema.tork": "datasource db {\n\turl = \"x\"\n}\n"},
			wantDiags: []string{
				`schema.tork:1:12: datasource "db" is missing its provider (add: provider = "postgres")`,
				`schema.tork:2:2: unknown datasource setting "url" (only "provider" is supported)`,
			},
		},
		{
			name:  "repeated setting",
			files: map[string]string{"schema.tork": "datasource db {\n\tprovider = \"postgres\"\n\tprovider = \"postgres\"\n}\n"},
			wantDiags: []string{
				`schema.tork:3:2: datasource setting "provider" repeated`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := analyzeFiles(t, tt.files)
			assertStrings(t, "diag", diagStrings(diags), tt.wantDiags)
		})
	}
}
