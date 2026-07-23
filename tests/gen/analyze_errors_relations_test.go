package gen_test

import "testing"

func TestAnalyze_IndexAndCheckErrors(t *testing.T) {
	runErrCases(t, []errCase{
		{
			name:  "unique index with an empty list",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@unique([])\n}\n"),
			wantDiags: []string{
				"schema.tork:4:1: @@unique needs a non empty field list, e.g. @@unique([a, b])",
			},
		},
		{
			name:  "unique index with a non list argument",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@unique(5)\n}\n"),
			wantDiags: []string{
				"schema.tork:4:10: @@unique needs a non empty field list, e.g. @@unique([a, b])",
			},
		},
		{
			name:  "unique index with an unknown field",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@unique([zzz])\n}\n"),
			wantDiags: []string{
				`schema.tork:4:11: unknown field "zzz" in @@unique`,
			},
		},
		{
			name:  "unique index with an unknown argument",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@unique([alpha], where: \"x\")\n}\n"),
			wantDiags: []string{
				`schema.tork:4:19: unknown argument "where" in @@unique`,
			},
		},
		{
			name:  "index with nothing to index",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index()\n}\n"),
			wantDiags: []string{
				"schema.tork:4:1: @@index needs fields or on: expressions",
			},
		},
		{
			name:  "index with a misspelled argument",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index([alpha], namee: \"x\")\n}\n"),
			wantDiags: []string{
				`schema.tork:4:18: unknown argument "namee" in @@index (did you mean "name"?)`,
			},
		},
		{
			name:  "index over a relation field",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [id])\n@@index([b])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:5:10: "b" is a relation field; index its foreign key column instead`,
			},
		},
		{
			name:  "index with an empty where",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index([alpha], where: \"\")\n}\n"),
			wantDiags: []string{
				`schema.tork:4:25: where: expects a SQL predicate, e.g. where: "deleted_at IS NULL"`,
			},
		},
		{
			name:  "expression index with an empty expression",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index(on: [\"\"])\n}\n"),
			wantDiags: []string{
				`schema.tork:4:14: on: expects non empty SQL expressions, e.g. on: ["lower(email)"]`,
			},
		},
		{
			name:  "expression index without a name",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index(on: [\"lower(alpha)\"])\n}\n"),
			wantDiags: []string{
				`schema.tork:4:1: an expression index needs a name, e.g. @@index(on: [...], name: "idx_users_lower_email")`,
			},
		},
		{
			name:  "expression index with a non list on argument",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index(on: \"x\")\n}\n"),
			wantDiags: []string{
				`schema.tork:4:13: on: expects non empty SQL expressions, e.g. on: ["lower(email)"]`,
			},
		},
		{
			name:  "expression index with a non string element",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index(on: [5])\n}\n"),
			wantDiags: []string{
				`schema.tork:4:14: on: expects non empty SQL expressions, e.g. on: ["lower(email)"]`,
			},
		},
		{
			name:  "index name repeated",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index([alpha], name: \"x\")\n@@unique([alpha], name: \"x\")\n}\n"),
			wantDiags: []string{
				`schema.tork:5:25: index name "x" repeated in model "A"`,
			},
		},
		{
			name:  "invalid index name",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index([alpha], name: \"1x\")\n}\n"),
			wantDiags: []string{
				`schema.tork:4:24: index name "1x" is not a valid identifier`,
			},
		},
		{
			name:  "index name must be a string",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@index([alpha], name: 5)\n}\n"),
			wantDiags: []string{
				"schema.tork:4:24: name: expects a string in @@index",
			},
		},
		{
			name:  "check without an expression",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@check(name: \"x\")\n}\n"),
			wantDiags: []string{
				`schema.tork:4:1: @@check needs a SQL expression, e.g. @@check("pages > 0")`,
			},
		},
		{
			name:  "check with a non string expression",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@check(5)\n}\n"),
			wantDiags: []string{
				`schema.tork:4:9: @@check needs a SQL expression, e.g. @@check("pages > 0")`,
			},
		},
		{
			name:  "check with an unknown argument",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@check(\"a > 0\", nm: \"x\")\n}\n"),
			wantDiags: []string{
				`schema.tork:4:18: unknown argument "nm" in @@check (did you mean "name"?)`,
			},
		},
		{
			name:  "check with an invalid name",
			files: one(dsLine + "\nmodel A {\nalpha String\n@@check(\"a > 0\", name: \"1\")\n}\n"),
			wantDiags: []string{
				`schema.tork:4:24: check name "1" is not a valid identifier`,
			},
		},
	})
}

func TestAnalyze_RelationErrors(t *testing.T) {
	runErrCases(t, []errCase{
		{
			name:  "missing inverse",
			files: one(dsLine + "\nmodel A {\nposts P[]\n}\nmodel P {\nid Int @id @default(autoincrement())\n}\n"),
			wantDiags: []string{
				`schema.tork:3:1: field "posts" has no matching relation field on model "P"; add one of type A or A[]`,
			},
		},
		{
			name:  "missing inverse of a named relation",
			files: one(dsLine + "\nmodel A {\nposts P[] @relation(\"X\")\n}\nmodel P {\nid Int @id @default(autoincrement())\n}\n"),
			wantDiags: []string{
				`schema.tork:3:11: field "posts" has no matching relation field on model "P"; add one of type A or A[] with @relation("X")`,
			},
		},
		{
			name:  "ambiguous unnamed relations",
			files: one(dsLine + "\nmodel A {\np1 P[]\np2 P[]\n}\nmodel P {\nid Int @id @default(autoincrement())\naid Int\na1 A @relation(fields: [aid], references: [id])\n}\n"),
			wantDiags: []string{
				`schema.tork:3:1: ambiguous relations between "A" and "P"; name each pair with @relation("Name")`,
				`schema.tork:4:1: ambiguous relations between "A" and "P"; name each pair with @relation("Name")`,
				`schema.tork:9:6: ambiguous relations between "A" and "P"; name each pair with @relation("Name")`,
			},
		},
		{
			name:  "named relation on more than two fields",
			files: one(dsLine + "\nmodel A {\np1 P[] @relation(\"X\")\np2 P[] @relation(\"X\")\n}\nmodel P {\nid Int @id @default(autoincrement())\naid Int\na1 A @relation(\"X\", fields: [aid], references: [id])\n}\n"),
			wantDiags: []string{
				`schema.tork:3:8: relation "X" is declared by more than two fields`,
				`schema.tork:4:8: relation "X" is declared by more than two fields`,
				`schema.tork:9:6: relation "X" is declared by more than two fields`,
			},
		},
		{
			name:  "unnamed self relation",
			files: one(dsLine + "\nmodel A {\nid Int @id @default(autoincrement())\npid Int?\nparent A? @relation(fields: [pid], references: [id])\nkids A[]\n}\n"),
			wantDiags: []string{
				`schema.tork:5:11: a self relation must be named: @relation("Name", ...)`,
				`schema.tork:6:1: a self relation must be named: @relation("Name", ...)`,
			},
		},
		{
			name:  "fields on both sides",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\naId Int\na A @relation(fields: [aId], references: [id])\n}\n"),
			wantDiags: []string{
				"schema.tork:4:5: fields: and references: belong on one side of the relation only",
			},
		},
		{
			name:  "fields on neither side",
			files: one(dsLine + "\nmodel A {\nb B\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:3:1: one side of the relation between "A" and "B" must declare fields: and references:`,
			},
		},
		{
			name:  "implicit many to many is rejected",
			files: one(dsLine + "\nmodel A {\nbs B[]\n}\nmodel B {\nid Int @id @default(autoincrement())\nas A[]\n}\n"),
			wantDiags: []string{
				"schema.tork:3:1: many to many requires through: naming the join model on both sides",
				"schema.tork:7:1: many to many requires through: naming the join model on both sides",
			},
		},
		{
			name:  "references on the inverse side",
			files: one(dsLine + "\nmodel A {\nb B @relation(references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\naId Int\na A @relation(fields: [aId], references: [id])\n}\n"),
			wantDiags: []string{
				"schema.tork:3:5: references: also needs fields: on the same side",
			},
		},
		{
			name:  "fields without references",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				"schema.tork:4:5: @relation with fields: also needs references:",
			},
		},
		{
			name:  "arity mismatch",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [id, bId])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				"schema.tork:4:5: fields: and references: have different lengths",
			},
		},
		{
			name:  "empty fields list",
			files: one(dsLine + "\nmodel A {\nb B @relation(fields: [], references: [])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				"schema.tork:3:5: fields: expects a list of field names, e.g. fields: [authorId]",
			},
		},
		{
			name:  "unknown field in fields",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [nope], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:24: unknown field "nope" in fields:`,
			},
		},
		{
			name:  "unknown field in references",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [idd])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:43: model "B" has no field "idd" (referenced in references:) (did you mean "id"?)`,
			},
		},
		{
			name:  "relation field used as a foreign key",
			files: one(dsLine + "\nmodel A {\nc C\nb B @relation(fields: [c], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\nmodel C {\nid Int @id @default(autoincrement())\n}\n"),
			wantDiags: []string{
				`schema.tork:3:1: field "c" has no matching relation field on model "C"; add one of type A or A[]`,
				`schema.tork:4:24: "c" in fields: cannot be a relation field`,
			},
		},
		{
			name:  "foreign key type mismatch",
			files: one(dsLine + "\nmodel A {\nbId String\nb B @relation(fields: [bId], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:24: foreign key "bId" (String) does not match referenced "id" (Int)`,
			},
		},
		{
			name:  "optional relation with a required key",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B? @relation(fields: [bId], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:25: field "b" is optional but its foreign key "bId" is required; make both optional or both required`,
			},
		},
		{
			name:  "required relation with an optional key",
			files: one(dsLine + "\nmodel A {\nbId Int?\nb B @relation(fields: [bId], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:24: field "b" is required but its foreign key "bId" is optional; make both optional or both required`,
			},
		},
		{
			name:  "references that are not unique",
			files: one(dsLine + "\nmodel A {\nbCode Int\nb B @relation(fields: [bCode], references: [code])\n}\nmodel B {\nid Int @id @default(autoincrement())\ncode Int\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:5: warning: references: columns do not form the primary key or a unique index on "B"`,
			},
		},
		{
			name:  "SetNull with a required key",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [id], onDelete: SetNull)\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				"schema.tork:4:5: onDelete: SetNull requires optional foreign key fields",
			},
		},
		{
			name:  "onDelete on the inverse side",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(\"X\", fields: [bId], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\nas A[] @relation(\"X\", onDelete: Cascade)\n}\n"),
			wantDiags: []string{
				"schema.tork:8:8: onDelete:/onUpdate: belong on the side that declares fields:",
			},
		},
		{
			name:  "map on the inverse side",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(\"X\", fields: [bId], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\nas A[] @relation(\"X\", map: \"fk_x\")\n}\n"),
			wantDiags: []string{
				"schema.tork:8:8: map: belongs on the side that declares fields:",
			},
		},
		{
			name:  "fields declared on a list side",
			files: one(dsLine + "\nmodel A {\naId Int\nxs B[] @relation(\"X\", fields: [aId], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\nx A @relation(\"X\")\n}\n"),
			wantDiags: []string{
				"schema.tork:4:8: the side that declares fields: must be singular, not a list",
			},
		},
		{
			name:  "misspelled action",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [id], onDelete: Cascde)\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:58: invalid onDelete action "Cascde" (use Cascade, Restrict, NoAction, SetNull, or SetDefault) (did you mean "Cascade"?)`,
			},
		},
		{
			name:  "action written as a string",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fields: [bId], references: [id], onDelete: \"Cascade\")\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				"schema.tork:4:58: invalid onDelete action (use Cascade, Restrict, NoAction, SetNull, or SetDefault)",
			},
		},
		{
			name:  "unknown relation argument",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(fielsd: [bId], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:5: one side of the relation between "A" and "B" must declare fields: and references:`,
				`schema.tork:4:15: unknown argument "fielsd" in @relation (did you mean "fields"?)`,
			},
		},
		{
			name:  "relation name must be a string",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(5, fields: [bId], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:4:15: the relation name must be a string, e.g. @relation("UserPosts")`,
			},
		},
		{
			name:  "two positional relation arguments",
			files: one(dsLine + "\nmodel A {\nbId Int\nb B @relation(\"X\", \"Y\", fields: [bId], references: [id])\n}\nmodel B {\nid Int @id @default(autoincrement())\na A @relation(\"X\")\n}\n"),
			wantDiags: []string{
				"schema.tork:4:20: @relation takes one positional argument, its name",
			},
		},
		{
			name:  "through names an unknown model",
			files: one(dsLine + "\nmodel A {\nb B @relation(through: Zz)\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:3:5: one side of the relation between "A" and "B" must declare fields: and references:`,
				`schema.tork:3:24: through: names an unknown model "Zz"`,
			},
		},
		{
			name:  "through must name a model",
			files: one(dsLine + "\nmodel A {\nb B @relation(through: \"X\")\n}\nmodel B {\nid Int @id @default(autoincrement())\na A\n}\n"),
			wantDiags: []string{
				`schema.tork:3:5: one side of the relation between "A" and "B" must declare fields: and references:`,
				"schema.tork:3:24: through: expects a model name, e.g. through: PostTag",
			},
		},
		{
			name:  "sides disagree on the through model",
			files: one(dsLine + "\nmodel A {\nid Int @id @default(autoincrement())\nts T[] @relation(\"M\", through: J1)\n}\nmodel T {\nid Int @id @default(autoincrement())\nas A[] @relation(\"M\", through: J2)\n}\nmodel J1 {\naId Int\ntId Int\na A @relation(fields: [aId], references: [id])\nt T @relation(fields: [tId], references: [id])\n}\nmodel J2 {\naId Int\ntId Int\na A @relation(fields: [aId], references: [id])\nt T @relation(fields: [tId], references: [id])\n}\n"),
			wantDiags: []string{
				"schema.tork:4:8: both sides must agree on the same through: model",
				"schema.tork:8:8: both sides must agree on the same through: model",
			},
		},
		{
			name:  "through on a singular relation",
			files: one(dsLine + "\nmodel A {\nb B @relation(\"X\", through: J)\n}\nmodel B {\nid Int @id @default(autoincrement())\na A @relation(\"X\")\n}\nmodel J {\nx Int\n}\n"),
			wantDiags: []string{
				"schema.tork:3:5: through: is only for many to many relations (both sides must be lists)",
			},
		},
		{
			name:  "self referencing many to many",
			files: one(dsLine + "\nmodel A {\nxs A[] @relation(\"S\", through: J)\nys A[] @relation(\"S\", through: J)\n}\nmodel J {\nx Int\n}\n"),
			wantDiags: []string{
				"schema.tork:3:8: many to many between a model and itself is not supported; model the join with two has many relations",
			},
		},
		{
			name:  "many to many with fields",
			files: one(dsLine + "\nmodel A {\nts T[] @relation(\"M\", through: J, fields: [tid])\n}\nmodel T {\nas A[] @relation(\"M\", through: J)\n}\nmodel J {\nx Int\n}\n"),
			wantDiags: []string{
				"schema.tork:3:8: fields:/references: do not apply to many to many relations (the join model owns the keys)",
			},
		},
		{
			name:  "many to many with onDelete",
			files: one(dsLine + "\nmodel A {\nts T[] @relation(\"M\", through: J, onDelete: Cascade)\n}\nmodel T {\nas A[] @relation(\"M\", through: J)\n}\nmodel J {\nx Int\n}\n"),
			wantDiags: []string{
				"schema.tork:3:8: onDelete:/onUpdate: do not apply to many to many relations (set them on the join model)",
			},
		},
		{
			name:  "join model without a belongs to",
			files: one(dsLine + "\nmodel A {\nts T[] @relation(\"M\", through: J)\n}\nmodel T {\nas A[] @relation(\"M\", through: J)\n}\nmodel J {\nx Int\n}\n"),
			wantDiags: []string{
				`schema.tork:3:32: join model "J" needs a belongs to relation to "A" (with fields: and references:)`,
			},
		},
		{
			name:  "join model with two relations to one endpoint",
			files: one(dsLine + "\nmodel A {\nid Int @id @default(autoincrement())\nt1s J[] @relation(\"t1\")\nt2s J[] @relation(\"t2\")\nbs B[] @relation(\"M\", through: J)\n}\nmodel B {\nid Int @id @default(autoincrement())\nts J[] @relation(\"t3\")\nas A[] @relation(\"M\", through: J)\n}\nmodel J {\nx1 Int\nx2 Int\nxb Int\na1 A @relation(\"t1\", fields: [x1], references: [id])\na2 A @relation(\"t2\", fields: [x2], references: [id])\nb B @relation(\"t3\", fields: [xb], references: [id])\n}\n"),
			wantDiags: []string{
				`schema.tork:6:32: join model "J" has more than one relation to "A"; many to many needs exactly one`,
			},
		},
	})
}
