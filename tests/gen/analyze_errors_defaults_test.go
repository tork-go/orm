package gen_test

import "testing"

func TestAnalyze_DefaultAndNativeErrors(t *testing.T) {
	runErrCases(t, []errCase{
		{
			name:  "empty default",
			files: one(dsLine + "\nmodel A {\nn Int @default()\n}\n"),
			wantDiags: []string{
				"schema.tork:3:7: @default requires a value, e.g. @default(now())",
			},
		},
		{
			name:  "default with two arguments",
			files: one(dsLine + "\nmodel A {\nn Int @default(1, 2)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:7: @default takes one argument",
			},
		},
		{
			name:  "default with a named argument",
			files: one(dsLine + "\nmodel A {\nn Int @default(value: 5)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:16: @default does not take named arguments",
			},
		},
		{
			name:  "now on a non DateTime field",
			files: one(dsLine + "\nmodel A {\nn Int @default(now())\n}\n"),
			wantDiags: []string{
				"schema.tork:3:16: now() requires a DateTime field",
			},
		},
		{
			name:  "uuid on a non Uuid field",
			files: one(dsLine + "\nmodel A {\nn Int @default(uuid())\n}\n"),
			wantDiags: []string{
				"schema.tork:3:16: uuid() requires a Uuid field",
			},
		},
		{
			name:  "dbgenerated with an empty expression",
			files: one(dsLine + "\nmodel A {\nn Int @default(dbgenerated(\"\"))\n}\n"),
			wantDiags: []string{
				`schema.tork:3:28: dbgenerated() requires a SQL expression, e.g. dbgenerated("now()")`,
			},
		},
		{
			name:  "dbgenerated without arguments",
			files: one(dsLine + "\nmodel A {\nn Int @default(dbgenerated())\n}\n"),
			wantDiags: []string{
				`schema.tork:3:16: dbgenerated() requires a SQL expression, e.g. dbgenerated("now()")`,
			},
		},
		{
			name:  "unknown default function",
			files: one(dsLine + "\nmodel A {\nn Int @default(nextval())\n}\n"),
			wantDiags: []string{
				`schema.tork:3:16: unknown default "nextval"`,
			},
		},
		{
			name:  "bare function name",
			files: one(dsLine + "\nmodel A {\nt DateTime @default(now)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:21: now is a function; write @default(now())",
			},
		},
		{
			name:  "enum default is not a member",
			files: one(dsLine + "\nenum E {\ndraft\npublished\n}\nmodel A {\ns E @default(publishd)\n}\n"),
			wantDiags: []string{
				`schema.tork:7:14: "publishd" is not a value of enum E (did you mean "published"?)`,
			},
		},
		{
			name:  "enum default written as a string",
			files: one(dsLine + "\nenum E {\ndraft\n}\nmodel A {\ns E @default(\"draft\")\n}\n"),
			wantDiags: []string{
				"schema.tork:6:14: write the enum member bare: @default(draft)",
			},
		},
		{
			name:  "string default on an Int field",
			files: one(dsLine + "\nmodel A {\nn Int @default(\"x\")\n}\n"),
			wantDiags: []string{
				`schema.tork:3:16: default "x" does not fit type Int`,
			},
		},
		{
			name:  "float default on an Int field",
			files: one(dsLine + "\nmodel A {\nn Int @default(1.5)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:16: default 1.5 does not fit type Int",
			},
		},
		{
			name:  "bool default on a String field",
			files: one(dsLine + "\nmodel A {\ns String @default(true)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:19: default true does not fit type String",
			},
		},
		{
			name:  "int default on a Boolean field",
			files: one(dsLine + "\nmodel A {\nb Boolean @default(1)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:20: default 1 does not fit type Boolean",
			},
		},
		{
			name:  "int32 overflow",
			files: one(dsLine + "\nmodel A {\nn Int32 @default(3000000000)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:18: integer default 3000000000 overflows Int32",
			},
		},
		{
			name:  "literal default on a DateTime field",
			files: one(dsLine + "\nmodel A {\nt DateTime @default(5)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:21: a DateTime default must be now() or dbgenerated(...)",
			},
		},
		{
			name:  "literal default on a Uuid field",
			files: one(dsLine + "\nmodel A {\nu Uuid @default(\"x\")\n}\n"),
			wantDiags: []string{
				"schema.tork:3:17: a Uuid default must be uuid(), go(...), or dbgenerated(...)",
			},
		},
		{
			name:  "literal default on a list field",
			files: one(dsLine + "\nmodel A {\ntags String[] @default(\"x\")\n}\n"),
			wantDiags: []string{
				"schema.tork:3:24: @default on a list field supports only dbgenerated(...)",
			},
		},
		{
			name:  "call default on a list field",
			files: one(dsLine + "\nmodel A {\ntags String[] @default(now())\n}\n"),
			wantDiags: []string{
				"schema.tork:3:24: @default on a list field supports only dbgenerated(...)",
			},
		},
		{
			name:  "literal default on a Json field",
			files: one(dsLine + "\nmodel A {\ndata Json @go.type(\"X\") @default(5)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:34: @default on a Json field supports only dbgenerated(...)",
			},
		},
		{
			name:  "list literal default",
			files: one(dsLine + "\nmodel A {\nn Int @default([1])\n}\n"),
			wantDiags: []string{
				"schema.tork:3:16: @default does not accept list literals; use dbgenerated(...)",
			},
		},
		{
			name:  "go with an invalid reference",
			files: one(dsLine + "\nmodel A {\nn Int @default(go(\"a b\"))\n}\n"),
			wantDiags: []string{
				`schema.tork:3:19: invalid Go function reference "a b" (write "Name" for the generated package, or "import/path.Name")`,
			},
		},
		{
			name:  "go with a non string argument",
			files: one(dsLine + "\nmodel A {\nn Int @default(go(5))\n}\n"),
			wantDiags: []string{
				`schema.tork:3:19: go() requires a function reference, e.g. go("mypkg.NewID")`,
			},
		},
		{
			name:  "go without arguments",
			files: one(dsLine + "\nmodel A {\nn Int @default(go())\n}\n"),
			wantDiags: []string{
				`schema.tork:3:16: go() requires a function reference, e.g. go("mypkg.NewID")`,
			},
		},
		{
			name:  "duplicate default",
			files: one(dsLine + "\nmodel A {\nn Int @default(1) @default(2)\n}\n"),
			wantDiags: []string{
				`schema.tork:3:19: @default repeated on field "n"`,
			},
		},
		{
			name:  "autoincrement with arguments",
			files: one(dsLine + "\nmodel A {\nn Int @default(autoincrement(1))\n}\n"),
			wantDiags: []string{
				"schema.tork:3:16: autoincrement() takes no arguments",
			},
		},
		{
			name:  "now with arguments",
			files: one(dsLine + "\nmodel A {\nt DateTime @default(now(1))\n}\n"),
			wantDiags: []string{
				"schema.tork:3:21: now() takes no arguments",
			},
		},
		{
			name:  "uuid with arguments",
			files: one(dsLine + "\nmodel A {\nu Uuid @default(uuid(1))\n}\n"),
			wantDiags: []string{
				"schema.tork:3:17: uuid() takes no arguments",
			},
		},
		{
			name:  "VarChar on a non String field",
			files: one(dsLine + "\nmodel A {\nn Int @db.VarChar(30)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:7: @db.VarChar applies only to String fields",
			},
		},
		{
			name:  "VarChar without a length",
			files: one(dsLine + "\nmodel A {\ns String @db.VarChar()\n}\n"),
			wantDiags: []string{
				"schema.tork:3:10: @db.VarChar needs a positive length, e.g. @db.VarChar(255)",
			},
		},
		{
			name:  "VarChar with a zero length",
			files: one(dsLine + "\nmodel A {\ns String @db.VarChar(0)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:22: @db.VarChar needs a positive length, e.g. @db.VarChar(255)",
			},
		},
		{
			name:  "VarChar with a string argument",
			files: one(dsLine + "\nmodel A {\ns String @db.VarChar(\"30\")\n}\n"),
			wantDiags: []string{
				"schema.tork:3:22: @db.VarChar needs a positive length, e.g. @db.VarChar(255)",
			},
		},
		{
			name:  "VarChar with a float argument",
			files: one(dsLine + "\nmodel A {\ns String @db.VarChar(1.5)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:22: @db.VarChar needs a positive length, e.g. @db.VarChar(255)",
			},
		},
		{
			name:  "VarChar with a named argument",
			files: one(dsLine + "\nmodel A {\ns String @db.VarChar(n: 30)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:22: @db.VarChar does not take named arguments",
			},
		},
		{
			name:  "Text on a non String field",
			files: one(dsLine + "\nmodel A {\nn Int @db.Text\n}\n"),
			wantDiags: []string{
				"schema.tork:3:7: @db.Text applies only to String fields",
			},
		},
		{
			name:  "Text with arguments",
			files: one(dsLine + "\nmodel A {\ns String @db.Text(1)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:10: @db.Text takes no arguments",
			},
		},
		{
			name:  "Numeric on a non Decimal field",
			files: one(dsLine + "\nmodel A {\ns String @db.Numeric(10, 2)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:10: @db.Numeric applies only to Decimal fields",
			},
		},
		{
			name:  "Numeric with one argument",
			files: one(dsLine + "\nmodel A {\nd Decimal @db.Numeric(10)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:11: @db.Numeric needs precision and scale, e.g. @db.Numeric(10, 2)",
			},
		},
		{
			name:  "Numeric scale exceeding precision",
			files: one(dsLine + "\nmodel A {\nd Decimal @db.Numeric(2, 5)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:26: @db.Numeric scale cannot exceed precision",
			},
		},
		{
			name:  "Numeric with a negative scale",
			files: one(dsLine + "\nmodel A {\nd Decimal @db.Numeric(10, -1)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:11: @db.Numeric needs precision and scale, e.g. @db.Numeric(10, 2)",
			},
		},
		{
			name:  "Json native on a non Json field",
			files: one(dsLine + "\nmodel A {\ns String @db.Json\n}\n"),
			wantDiags: []string{
				"schema.tork:3:10: @db.Json applies only to Json fields",
			},
		},
		{
			name:  "unknown native type",
			files: one(dsLine + "\nmodel A {\ns String @db.Varchar(30)\n}\n"),
			wantDiags: []string{
				`schema.tork:3:10: unknown native type @db.Varchar for provider "postgres" (did you mean "VarChar"?)`,
			},
		},
		{
			name:  "db native without a datasource",
			files: one("model A {\ns String @db.VarChar(30)\n}\n"),
			wantDiags: []string{
				`schema.tork:1:1: missing datasource block; add: datasource db { provider = "postgres" }`,
				`schema.tork:2:10: @db attributes require a datasource block (add: datasource db { provider = "postgres" })`,
			},
		},
		{
			name:  "unknown attribute with suggestion",
			files: one(dsLine + "\nmodel A {\ns String @uniqe\n}\n"),
			wantDiags: []string{
				`schema.tork:3:10: unknown attribute @uniqe (did you mean "unique"?)`,
			},
		},
		{
			name:  "id takes no arguments",
			files: one(dsLine + "\nmodel A {\nn Int @id(5)\n}\n"),
			wantDiags: []string{
				"schema.tork:3:7: @id takes no arguments",
			},
		},
		{
			name:  "map with an invalid identifier",
			files: one(dsLine + "\nmodel A {\ns String @map(\"1x\")\n}\n"),
			wantDiags: []string{
				`schema.tork:3:15: @map value "1x" is not a valid identifier`,
			},
		},
		{
			name:  "map with a non string argument",
			files: one(dsLine + "\nmodel A {\ns String @map(5)\n}\n"),
			wantDiags: []string{
				`schema.tork:3:15: @map expects a string, e.g. @map("column_name")`,
			},
		},
		{
			name:  "table map with an invalid identifier",
			files: one(dsLine + "\nmodel A {\ns String\n@@map(\"x y\")\n}\n"),
			wantDiags: []string{
				`schema.tork:4:7: @@map value "x y" is not a valid identifier`,
			},
		},
		{
			name:  "soft delete on a required DateTime",
			files: one(dsLine + "\nmodel A {\nt DateTime @softDelete\n}\n"),
			wantDiags: []string{
				"schema.tork:3:12: @softDelete requires an optional DateTime field (DateTime?)",
			},
		},
		{
			name:  "soft delete on an optional String",
			files: one(dsLine + "\nmodel A {\ns String? @softDelete\n}\n"),
			wantDiags: []string{
				"schema.tork:3:11: @softDelete requires an optional DateTime field (DateTime?)",
			},
		},
		{
			name:  "second soft delete field",
			files: one(dsLine + "\nmodel A {\na DateTime? @softDelete\nb DateTime? @softDelete\n}\n"),
			wantDiags: []string{
				`schema.tork:4:13: model "A" already has a soft delete field ("a")`,
			},
		},
		{
			name:  "unknown block attribute with suggestion",
			files: one(dsLine + "\nmodel A {\ns String\n@@indx([s])\n}\n"),
			wantDiags: []string{
				`schema.tork:4:1: unknown attribute @@indx (did you mean "index"?)`,
			},
		},
		{
			name:  "relation attribute on a scalar field",
			files: one(dsLine + "\nmodel A {\ns String @relation(\"X\")\n}\n"),
			wantDiags: []string{
				"schema.tork:3:10: @relation applies only to fields whose type is a model",
			},
		},
		{
			name:  "repeated attribute",
			files: one(dsLine + "\nmodel A {\ns String @unique @unique\n}\n"),
			wantDiags: []string{
				`schema.tork:3:18: @unique repeated on field "s"`,
			},
		},
	})
}
