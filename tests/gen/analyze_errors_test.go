package gen_test

import "testing"

// errCase is one malformed schema and the exact report it must produce.
type errCase struct {
	name      string
	files     map[string]string
	wantDiags []string
}

func runErrCases(t *testing.T, tests []errCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := analyzeFiles(t, tt.files)
			assertStrings(t, "diag", diagStrings(diags), tt.wantDiags)
		})
	}
}

func one(src string) map[string]string {
	return map[string]string{"schema.tork": src}
}

func TestAnalyze_StructureAndTypeErrors(t *testing.T) {
	runErrCases(t, []errCase{
		{
			name: "model redeclared",
			files: one(dsLine + "\nmodel A {\nid Int @id @default(autoincrement())\n}\nmodel A {\nid Int @id @default(autoincrement())\n}\n"),
			wantDiags: []string{
				`schema.tork:5:7: model "A" redeclared (first declared at schema.tork:2:7)`,
			},
		},
		{
			name: "enum redeclared",
			files: one(dsLine + "\nenum E {\na\n}\nenum E {\nb\n}\n"),
			wantDiags: []string{
				`schema.tork:5:6: enum "E" redeclared (first declared at schema.tork:2:6)`,
			},
		},
		{
			name: "enum conflicts with model",
			files: one(dsLine + "\nmodel X {\nid Int @id @default(autoincrement())\n}\nenum X {\na\n}\n"),
			wantDiags: []string{
				`schema.tork:5:6: enum "X" conflicts with the model of the same name (declared at schema.tork:2:7)`,
			},
		},
		{
			name: "model conflicts with enum",
			files: one(dsLine + "\nenum X {\na\n}\nmodel X {\nid Int @id @default(autoincrement())\n}\n"),
			wantDiags: []string{
				`schema.tork:5:7: model "X" conflicts with the enum of the same name (declared at schema.tork:2:6)`,
			},
		},
		{
			name: "lowercase model name",
			files: one(dsLine + "\nmodel user {\nid Int @id @default(autoincrement())\n}\n"),
			wantDiags: []string{
				`schema.tork:2:7: model name "user" must start with an uppercase letter`,
			},
		},
		{
			name: "lowercase enum name",
			files: one(dsLine + "\nenum status {\na\n}\n"),
			wantDiags: []string{
				`schema.tork:2:6: enum name "status" must start with an uppercase letter`,
			},
		},
		{
			name: "model shadows a built in type",
			files: one(dsLine + "\nmodel String {\nid Int @id @default(autoincrement())\n}\n"),
			wantDiags: []string{
				`schema.tork:2:7: "String" is a built in type name and cannot be used as a model name`,
			},
		},
		{
			name: "enum shadows a built in type",
			files: one(dsLine + "\nenum Json {\na\n}\n"),
			wantDiags: []string{
				`schema.tork:2:6: "Json" is a built in type name and cannot be used as an enum name`,
			},
		},
		{
			name:  "empty model",
			files: one(dsLine + "\nmodel A {\n}\n"),
			wantDiags: []string{
				`schema.tork:2:7: model "A" has no fields`,
			},
		},
		{
			name:  "empty enum",
			files: one(dsLine + "\nenum E {\n}\n"),
			wantDiags: []string{
				`schema.tork:2:6: enum "E" has no values`,
			},
		},
		{
			name:  "enum value repeated",
			files: one(dsLine + "\nenum E {\na\na\n}\n"),
			wantDiags: []string{
				`schema.tork:4:1: enum value "a" repeated in enum "E"`,
			},
		},
		{
			name:  "uppercase field name",
			files: one(dsLine + "\nmodel A {\nId Int @id @default(autoincrement())\n}\n"),
			wantDiags: []string{
				`schema.tork:3:1: field name "Id" must start with a lowercase letter`,
			},
		},
		{
			name:  "field redeclared",
			files: one(dsLine + "\nmodel A {\nid Int @id @default(autoincrement())\nid String\n}\n"),
			wantDiags: []string{
				`schema.tork:4:1: field "id" redeclared in model "A"`,
			},
		},
		{
			name:  "column collision through map",
			files: one(dsLine + "\nmodel A {\na Int @map(\"x\")\nb Int @map(\"x\")\n}\n"),
			wantDiags: []string{
				`schema.tork:4:1: fields "a" and "b" both map to column "x" in model "A" (adjust @map)`,
			},
		},
		{
			name:  "Go name and column collisions",
			files: one(dsLine + "\nmodel A {\nauthorId Int\nauthorID Int\n}\n"),
			wantDiags: []string{
				`schema.tork:4:1: fields "authorId" and "authorID" produce the same Go field name "AuthorID"; rename one`,
				`schema.tork:4:1: fields "authorId" and "authorID" both map to column "author_id" in model "A" (adjust @map)`,
			},
		},
		{
			name:  "only map is allowed inside an enum",
			files: one(dsLine + "\nenum E {\na\n@@index([a])\n}\n"),
			wantDiags: []string{
				"schema.tork:4:1: only @@map is allowed inside an enum",
			},
		},
		{
			name: "table collision",
			files: one(dsLine + "\nmodel User {\nid Int @id @default(autoincrement())\n}\nmodel A {\nid Int @id @default(autoincrement())\n@@map(\"users\")\n}\n"),
			wantDiags: []string{
				`schema.tork:5:7: table "users" is used by both model "User" and model "A" (rename one with @@map)`,
			},
		},
		{
			name: "enum type collision",
			files: one(dsLine + "\nenum A {\nx\n@@map(\"t\")\n}\nenum B {\ny\n@@map(\"t\")\n}\n"),
			wantDiags: []string{
				`schema.tork:6:6: enum type "t" is used by both enum "A" and enum "B" (rename one with @@map)`,
			},
		},
		{
			name: "enum type collides with a table",
			files: one(dsLine + "\nmodel User {\nid Int @id @default(autoincrement())\n}\nenum Users {\na\n}\n"),
			wantDiags: []string{
				`schema.tork:5:6: enum type "users" collides with the table of model "User"; Postgres cannot hold both (rename one with @@map)`,
			},
		},
		{
			name:  "unknown type with suggestion",
			files: one(dsLine + "\nmodel A {\nname Strng\n}\n"),
			wantDiags: []string{
				`schema.tork:3:6: unknown type "Strng" (did you mean "String"?)`,
			},
		},
		{
			name:  "unknown type without suggestion",
			files: one(dsLine + "\nmodel A {\nname Zzzzzz\n}\n"),
			wantDiags: []string{
				`schema.tork:3:6: unknown type "Zzzzzz"`,
			},
		},
		{
			name:  "Json without go.type",
			files: one(dsLine + "\nmodel A {\ndata Json\n}\n"),
			wantDiags: []string{
				`schema.tork:3:1: a Json field needs @go.type to name its Go type, e.g. @go.type("Profile")`,
			},
		},
		{
			name:  "go.type on a non Json field",
			files: one(dsLine + "\nmodel A {\nname String @go.type(\"X\")\n}\n"),
			wantDiags: []string{
				"schema.tork:3:13: @go.type applies only to Json fields",
			},
		},
		{
			name:  "invalid go.type reference",
			files: one(dsLine + "\nmodel A {\ndata Json @go.type(\"models Profile\")\n}\n"),
			wantDiags: []string{
				`schema.tork:3:20: invalid Go type reference "models Profile" (write "Name" for a type in the generated package, or "import/path.Name")`,
			},
		},
		{
			name:  "go.type with a named argument",
			files: one(dsLine + "\nmodel A {\ndata Json @go.type(t: \"X\")\n}\n"),
			wantDiags: []string{
				"schema.tork:3:20: @go.type does not take named arguments",
			},
		},
		{
			name:  "enum list field",
			files: one(dsLine + "\nmodel A {\ns E[]\n}\nenum E {\na\n}\n"),
			wantDiags: []string{
				"schema.tork:3:3: enum fields cannot be lists (the ORM has no enum array column)",
			},
		},
		{
			name:  "Json list field",
			files: one(dsLine + "\nmodel A {\ndata Json[]\n}\n"),
			wantDiags: []string{
				"schema.tork:3:6: Json fields cannot be lists (bind a slice type with @go.type instead)",
			},
		},
		{
			name:  "optional relation list",
			files: one(dsLine + "\nmodel A {\nposts P[]?\n}\nmodel P {\nid Int @id @default(autoincrement())\n}\n"),
			wantDiags: []string{
				"schema.tork:3:7: a relation list cannot be optional (it loads as an empty slice when there are no rows)",
			},
		},
		{
			name:  "duplicate id",
			files: one(dsLine + "\nmodel A {\na Int @id @default(autoincrement())\nb Int @id\n}\n"),
			wantDiags: []string{
				`schema.tork:4:7: duplicate @id; model "A" already marks "a" as its primary key`,
			},
		},
		{
			name:  "id on an optional field",
			files: one(dsLine + "\nmodel A {\nid Int? @id\n}\n"),
			wantDiags: []string{
				"schema.tork:3:9: @id cannot apply to an optional field",
			},
		},
		{
			name:  "id on a list field",
			files: one(dsLine + "\nmodel A {\nid Int[] @id\n}\n"),
			wantDiags: []string{
				"schema.tork:3:10: @id cannot apply to a list field",
			},
		},
		{
			name:  "id on a Json field",
			files: one(dsLine + "\nmodel A {\nid Json @id @go.type(\"X\")\n}\n"),
			wantDiags: []string{
				"schema.tork:3:9: @id cannot apply to a Json field",
			},
		},
		{
			name:  "id on a relation field",
			files: one(dsLine + "\nmodel A {\nu U @id\n}\nmodel U {\nid Int @id @default(autoincrement())\n}\n"),
			wantDiags: []string{
				`schema.tork:3:1: field "u" has no matching relation field on model "U"; add one of type A or A[]`,
				"schema.tork:3:5: @id cannot apply to a relation field (mark the foreign key field instead)",
			},
		},
		{
			name:  "id and composite id together",
			files: one(dsLine + "\nmodel A {\na Int @id @default(autoincrement())\nb Int\n@@id([a, b])\n}\n"),
			wantDiags: []string{
				`schema.tork:5:1: model "A" has both @id and @@id; use one`,
			},
		},
		{
			name:  "composite id repeated",
			files: one(dsLine + "\nmodel A {\na String\nb String\n@@id([a])\n@@id([b])\n}\n"),
			wantDiags: []string{
				`schema.tork:6:1: @@id repeated in model "A"`,
			},
		},
		{
			name:  "composite id with empty list",
			files: one(dsLine + "\nmodel A {\na Int\n@@id([])\n}\n"),
			wantDiags: []string{
				"schema.tork:4:1: @@id needs a non empty field list, e.g. @@id([a, b])",
			},
		},
		{
			name:  "composite id with named argument",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@id(x: [alpha])\n}\n"),
			wantDiags: []string{
				"schema.tork:4:6: @@id does not take named arguments",
			},
		},
		{
			name:  "composite id with two arguments",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@id([alpha], [alpha])\n}\n"),
			wantDiags: []string{
				"schema.tork:4:1: @@id needs a non empty field list, e.g. @@id([a, b])",
			},
		},
		{
			name:  "composite id with a non list argument",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@id(5)\n}\n"),
			wantDiags: []string{
				"schema.tork:4:6: @@id needs a non empty field list, e.g. @@id([a, b])",
			},
		},
		{
			name:  "composite id with a non ident element",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@id([5])\n}\n"),
			wantDiags: []string{
				"schema.tork:4:7: @@id needs a non empty field list, e.g. @@id([a, b])",
			},
		},
		{
			name:  "composite id with unknown field",
			files: one(dsLine + "\nmodel A {\nalpha Int\nbeta Int\n@@id([gamma])\n}\n"),
			wantDiags: []string{
				`schema.tork:5:7: unknown field "gamma" in @@id`,
			},
		},
		{
			name:  "composite id with repeated field",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@id([alpha, alpha])\n}\n"),
			wantDiags: []string{
				`schema.tork:4:14: field "alpha" repeated in @@id`,
			},
		},
		{
			name:  "composite id with optional member",
			files: one(dsLine + "\nmodel A {\nalpha Int?\n@@id([alpha])\n}\n"),
			wantDiags: []string{
				`schema.tork:4:7: @@id cannot include optional field "alpha"`,
			},
		},
		{
			name:  "autoincrement on a non integer field",
			files: one(dsLine + "\nmodel A {\ns String @default(autoincrement())\n}\n"),
			wantDiags: []string{
				"schema.tork:3:19: @default(autoincrement()) requires an integer field",
			},
		},
		{
			name:  "autoincrement off the primary key",
			files: one(dsLine + "\nmodel A {\nid Int @id @default(autoincrement())\nn Int @default(autoincrement())\n}\n"),
			wantDiags: []string{
				"schema.tork:4:16: @default(autoincrement()) requires the field to be the primary key",
			},
		},
		{
			name:  "autoincrement inside a composite key",
			files: one(dsLine + "\nmodel A {\na Int @default(autoincrement())\nb Int\n@@id([a, b])\n}\n"),
			wantDiags: []string{
				"schema.tork:3:16: @default(autoincrement()) requires a single column primary key",
			},
		},
		{
			name:  "implicit identity gets a warning",
			files: one(dsLine + "\nmodel A {\nid Int @id\n}\n"),
			wantDiags: []string{
				`schema.tork:3:1: warning: "id" is the single integer primary key, so it becomes GENERATED ALWAYS AS IDENTITY; add @default(autoincrement()) to make that explicit`,
			},
		},
		{
			name:  "identity conflicts with a server default",
			files: one(dsLine + "\nmodel A {\nid Int @id @default(5)\n}\n"),
			wantDiags: []string{
				`schema.tork:3:21: "id" is a generated identity column (single integer primary key); it cannot also have a server side default`,
			},
		},
	})
}
