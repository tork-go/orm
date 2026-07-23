package lsp

// The vocabulary tables below are the language's documentation as the
// editor sees it. Each entry's text is the one sentence a user needs
// while typing, which is a different job from the reference prose in
// docs/, and short enough to read in a completion popup.

type entry struct {
	label  string
	detail string
	doc    string
}

var keywordEntries = []entry{
	{"model", "model Name { ... }", "Declares a table. Its fields become columns, and its name is pluralized into the table name unless @@map says otherwise."},
	{"enum", "enum Name { ... }", "Declares a database enum type and the values it accepts."},
	{"datasource", `datasource db { provider = "postgres" }`, "Names the database this schema targets. Exactly one datasource per schema, and it gates the @db native type namespace."},
}

var scalarEntries = []entry{
	{"Boolean", "bool", "A true or false column."},
	{"Int", "int", "A 32 bit integer column. As a lone primary key it becomes an identity column."},
	{"Int32", "int32", "A 32 bit integer column scanning into int32."},
	{"BigInt", "int64", "A 64 bit integer column."},
	{"Float", "float32", "A single precision floating point column."},
	{"Double", "float64", "A double precision floating point column."},
	{"Decimal", "decimal.Decimal", "An exact numeric column. Size it with @db.Numeric(precision, scale)."},
	{"String", "string", "A text column. @db.VarChar(n) makes it VARCHAR(n) instead of TEXT."},
	{"DateTime", "time.Time", "A timestamp column."},
	{"Uuid", "uuid.UUID", "A UUID column."},
	{"Json", "the type named by @go.type", "A JSONB column bound to a Go type. @db.Json stores text json instead."},
}

var fieldAttrEntries = []entry{
	{"id", "@id", "Marks the primary key. Use @@id for a composite key."},
	{"unique", "@unique", "Adds a unique constraint on this column."},
	{"index", "@index", "Adds an index on this column."},
	{"default", "@default(value)", "Sets a default: autoincrement(), now(), uuid(), a literal, dbgenerated(\"sql\"), or go(\"pkg.Func\")."},
	{"map", `@map("column_name")`, "Overrides the column name, which otherwise is the field name in snake case."},
	{"softDelete", "@softDelete", "Marks the DateTime? column that records a soft delete."},
	{"relation", `@relation("Name", fields: [...], references: [...])`, "Declares a relation. The side carrying fields: and references: owns the foreign key."},
	{"db.VarChar", "@db.VarChar(255)", "Stores a String as VARCHAR(n) rather than TEXT."},
	{"db.Text", "@db.Text", "Stores a String as TEXT, which is already the default."},
	{"db.Numeric", "@db.Numeric(10, 2)", "Sizes a Decimal column."},
	{"db.Json", "@db.Json", "Stores a Json column as json rather than jsonb."},
	{"db.JsonB", "@db.JsonB", "Stores a Json column as jsonb, which is already the default."},
	{"go.type", `@go.type("Profile")`, "Names the Go type a Json column scans into. Required on every Json field."},
}

var nativeEntries = []entry{
	{"VarChar", "@db.VarChar(255)", "Stores a String as VARCHAR(n)."},
	{"Text", "@db.Text", "Stores a String as TEXT."},
	{"Numeric", "@db.Numeric(10, 2)", "Sizes a Decimal column."},
	{"Json", "@db.Json", "Stores a Json column as json."},
	{"JsonB", "@db.JsonB", "Stores a Json column as jsonb."},
}

var blockAttrEntries = []entry{
	{"id", "@@id([a, b])", "Declares a composite primary key."},
	{"unique", "@@unique([a, b])", "Declares a unique constraint over several columns."},
	{"index", `@@index([a, b], name: "...", where: "...", on: ["lower(a)"])`, "Declares an index, optionally partial or over expressions."},
	{"check", `@@check("pages > 0")`, "Declares a check constraint."},
	{"map", `@@map("table_name")`, "Overrides the table name, which otherwise is the pluralized model name."},
}

var relationArgEntries = []entry{
	{"fields", "fields: [authorId]", "The columns on this model that hold the foreign key."},
	{"references", "references: [id]", "The columns on the other model that the key points at."},
	{"onDelete", "onDelete: Cascade", "What happens to this row when the referenced row is deleted."},
	{"onUpdate", "onUpdate: Cascade", "What happens to this key when the referenced columns change."},
	{"through", "through: PostTag", "The join model of a many to many relation."},
	{"map", `map: "fk_posts_author"`, "Names the foreign key constraint."},
}

var actionEntries = []entry{
	{"Cascade", "onDelete: Cascade", "Delete or update the referencing rows along with the referenced one."},
	{"Restrict", "onDelete: Restrict", "Refuse to delete or update while referencing rows exist."},
	{"NoAction", "onDelete: NoAction", "Leave it to the database's own default timing, which is the default."},
	{"SetNull", "onDelete: SetNull", "Null out the foreign key. Requires optional key columns."},
	{"SetDefault", "onDelete: SetDefault", "Reset the foreign key to its column default."},
}

var providerEntries = []entry{
	{"postgres", `provider = "postgres"`, "PostgreSQL, the driver this ORM ships."},
}

// fieldAttrDoc finds an attribute's documentation by its dotted name,
// which is what hover shows when the cursor is on one.
func fieldAttrDoc(name string) (entry, bool) {
	for _, e := range fieldAttrEntries {
		if e.label == name {
			return e, true
		}
	}
	return entry{}, false
}

func blockAttrDoc(name string) (entry, bool) {
	for _, e := range blockAttrEntries {
		if e.label == name {
			return e, true
		}
	}
	return entry{}, false
}
