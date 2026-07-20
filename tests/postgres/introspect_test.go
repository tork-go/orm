//go:build integration

package postgres_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

func TestIntrospect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	// t.Cleanup, not defer: it must be registered before the table-drop
	// cleanup below so it runs after it (t.Cleanup callbacks run in LIFO
	// order, same as defer, but all of them run after the test function's
	// own defers have already fired).
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	setup := `
DROP TABLE IF EXISTS test_introspect_child, test_introspect_parent CASCADE;

CREATE TABLE test_introspect_parent (
    id INTEGER GENERATED ALWAYS AS IDENTITY CONSTRAINT pk_test_introspect_parent PRIMARY KEY,
    code VARCHAR(20) NOT NULL,
    name TEXT,
    CONSTRAINT uq_test_introspect_parent_code UNIQUE (code)
);

CREATE TABLE test_introspect_child (
    parent_id INTEGER NOT NULL,
    seq INTEGER NOT NULL,
    label VARCHAR(50),
    active BOOLEAN NOT NULL,
    score DOUBLE PRECISION,
    ratio REAL,
    big_num BIGINT,
    created_at TIMESTAMP WITHOUT TIME ZONE,
    ext_id UUID,
    CONSTRAINT pk_test_introspect_child PRIMARY KEY (parent_id, seq),
    CONSTRAINT fk_test_introspect_child_parent FOREIGN KEY (parent_id) REFERENCES test_introspect_parent (id)
);

CREATE INDEX ix_test_introspect_child_active_score ON test_introspect_child (active, score);`
	if err := conn.Exec(ctx, setup); err != nil {
		t.Fatalf("test setup failed: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS test_introspect_child, test_introspect_parent CASCADE`)
	})

	got, err := dialect.Introspect(ctx, conn, []string{"test_introspect_parent", "test_introspect_child"})
	if err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}
	if len(got.Tables) != 2 {
		t.Fatalf("got %d tables, want 2: %+v", len(got.Tables), got.Tables)
	}

	parent := tableNamed(t, got, "test_introspect_parent")
	wantParentColumns := []schema.Column{
		{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true},
		{Name: "code", Type: schema.ColumnType{Kind: schema.KindVarchar, Length: 20}, NotNull: true},
		{Name: "name", Type: schema.ColumnType{Kind: schema.KindText}, NotNull: false},
	}
	if !reflect.DeepEqual(parent.Columns, wantParentColumns) {
		t.Errorf("parent.Columns = %+v, want %+v", parent.Columns, wantParentColumns)
	}
	wantParentPK := &schema.PrimaryKey{Name: "pk_test_introspect_parent", Columns: []string{"id"}}
	if !reflect.DeepEqual(parent.PrimaryKey, wantParentPK) {
		t.Errorf("parent.PrimaryKey = %+v, want %+v", parent.PrimaryKey, wantParentPK)
	}
	wantParentUniques := []schema.UniqueConstraint{{Name: "uq_test_introspect_parent_code", Columns: []string{"code"}}}
	if !reflect.DeepEqual(parent.Uniques, wantParentUniques) {
		t.Errorf("parent.Uniques = %+v, want %+v", parent.Uniques, wantParentUniques)
	}
	// The unique constraint above is backed by its own index in Postgres;
	// it must not also show up as a plain Index (NOT indisunique in the
	// introspection query excludes it).
	if len(parent.Indexes) != 0 {
		t.Errorf("parent.Indexes = %+v, want none (a unique constraint's backing index must not be double-counted)", parent.Indexes)
	}
	if len(parent.ForeignKeys) != 0 {
		t.Errorf("parent.ForeignKeys = %+v, want none", parent.ForeignKeys)
	}

	child := tableNamed(t, got, "test_introspect_child")
	wantChildColumns := []schema.Column{
		{Name: "parent_id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true},
		{Name: "seq", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true},
		{Name: "label", Type: schema.ColumnType{Kind: schema.KindVarchar, Length: 50}, NotNull: false},
		{Name: "active", Type: schema.ColumnType{Kind: schema.KindBoolean}, NotNull: true},
		{Name: "score", Type: schema.ColumnType{Kind: schema.KindDouble}, NotNull: false},
		{Name: "ratio", Type: schema.ColumnType{Kind: schema.KindFloat}, NotNull: false},
		{Name: "big_num", Type: schema.ColumnType{Kind: schema.KindBigInteger}, NotNull: false},
		{Name: "created_at", Type: schema.ColumnType{Kind: schema.KindTimestamp}, NotNull: false},
		{Name: "ext_id", Type: schema.ColumnType{Kind: schema.KindUUID}, NotNull: false},
	}
	if !reflect.DeepEqual(child.Columns, wantChildColumns) {
		t.Errorf("child.Columns = %+v, want %+v", child.Columns, wantChildColumns)
	}
	wantChildPK := &schema.PrimaryKey{Name: "pk_test_introspect_child", Columns: []string{"parent_id", "seq"}}
	if !reflect.DeepEqual(child.PrimaryKey, wantChildPK) {
		t.Errorf("child.PrimaryKey = %+v, want %+v", child.PrimaryKey, wantChildPK)
	}
	wantChildIndexes := []schema.Index{{Name: "ix_test_introspect_child_active_score", Columns: []string{"active", "score"}}}
	if !reflect.DeepEqual(child.Indexes, wantChildIndexes) {
		t.Errorf("child.Indexes = %+v, want %+v", child.Indexes, wantChildIndexes)
	}
	wantChildFKs := []schema.ForeignKey{{
		Name:              "fk_test_introspect_child_parent",
		Columns:           []string{"parent_id"},
		ReferencedTable:   "test_introspect_parent",
		ReferencedColumns: []string{"id"},
	}}
	if !reflect.DeepEqual(child.ForeignKeys, wantChildFKs) {
		t.Errorf("child.ForeignKeys = %+v, want %+v", child.ForeignKeys, wantChildFKs)
	}
}

// TestIntrospect_PartialAndExpressionIndexes_Excluded documents a known
// gap (see driver/postgres/doc.go): schema.Index has no way to represent
// an expression key or a WHERE predicate, so both kinds are excluded from
// introspection entirely rather than misrepresented as a plain index.
func TestIntrospect_PartialAndExpressionIndexes_Excluded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	t.Cleanup(func() {
		_ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS test_introspect_special CASCADE`)
	})
	setup := `
DROP TABLE IF EXISTS test_introspect_special CASCADE;
CREATE TABLE test_introspect_special (
    id INTEGER,
    name TEXT,
    active BOOLEAN
);
CREATE INDEX ix_special_partial ON test_introspect_special (id) WHERE active;
CREATE INDEX ix_special_expr ON test_introspect_special (lower(name));`
	if err := conn.Exec(ctx, setup); err != nil {
		t.Fatalf("test setup failed: %v", err)
	}

	got, err := dialect.Introspect(ctx, conn, []string{"test_introspect_special"})
	if err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}
	table := tableNamed(t, got, "test_introspect_special")
	if len(table.Indexes) != 0 {
		t.Errorf("Indexes = %+v, want none (partial and expression indexes must be excluded)", table.Indexes)
	}
}

func TestIntrospect_UnknownTable_ReturnsEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer conn.Close(ctx)

	got, err := dialect.Introspect(ctx, conn, []string{"table_that_does_not_exist"})
	if err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}
	if len(got.Tables) != 0 {
		t.Errorf("got %d tables, want 0: %+v", len(got.Tables), got.Tables)
	}
}

// TestIntrospect_NewColumnKindsAndConstraints covers everything added this
// round: NUMERIC(p,s), a native enum column, JSON/JSONB, array columns
// (including an array of a bounded-length/precision element, exercising
// the format_type() parsing path), a CHECK constraint, and a foreign key
// with non-default ON DELETE/ON UPDATE actions, all round-tripped through
// real Postgres.
func TestIntrospect_NewColumnKindsAndConstraints(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	t.Cleanup(func() {
		_ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS test_v2_child, test_v2_parent CASCADE; DROP TYPE IF EXISTS test_order_status`)
	})
	setup := `
DROP TABLE IF EXISTS test_v2_child, test_v2_parent CASCADE;
DROP TYPE IF EXISTS test_order_status;

CREATE TYPE test_order_status AS ENUM ('pending', 'paid', 'done');

CREATE TABLE test_v2_parent (
    id INTEGER GENERATED ALWAYS AS IDENTITY CONSTRAINT pk_test_v2_parent PRIMARY KEY
);

CREATE TABLE test_v2_child (
    id INTEGER GENERATED ALWAYS AS IDENTITY CONSTRAINT pk_test_v2_child PRIMARY KEY,
    parent_id INTEGER,
    age INTEGER NOT NULL,
    amount NUMERIC(10,2),
    status test_order_status,
    data JSON,
    data_b JSONB,
    tags VARCHAR(20)[],
    scores NUMERIC(5,2)[],
    flags BOOLEAN[],
    ext_ids UUID[],
    CONSTRAINT ck_test_v2_child_age CHECK (age >= 0),
    CONSTRAINT fk_test_v2_child_parent FOREIGN KEY (parent_id) REFERENCES test_v2_parent (id)
        ON DELETE CASCADE ON UPDATE SET NULL
);`
	if err := conn.Exec(ctx, setup); err != nil {
		t.Fatalf("test setup failed: %v", err)
	}

	got, err := dialect.Introspect(ctx, conn, []string{"test_v2_parent", "test_v2_child"})
	if err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}

	wantEnum := []schema.EnumType{{Name: "test_order_status", Values: []string{"pending", "paid", "done"}}}
	if !reflect.DeepEqual(got.EnumTypes, wantEnum) {
		t.Errorf("EnumTypes = %+v, want %+v", got.EnumTypes, wantEnum)
	}

	child := tableNamed(t, got, "test_v2_child")
	colType := func(name string) schema.ColumnType {
		for _, c := range child.Columns {
			if c.Name == name {
				return c.Type
			}
		}
		t.Fatalf("no column named %q in %+v", name, child.Columns)
		return schema.ColumnType{}
	}

	if got := colType("amount"); got != (schema.ColumnType{Kind: schema.KindNumeric, Precision: 10, Scale: 2}) {
		t.Errorf("amount.Type = %+v, want KindNumeric(10,2)", got)
	}
	if got := colType("status"); got != (schema.ColumnType{Kind: schema.KindEnum, TypeName: "test_order_status"}) {
		t.Errorf("status.Type = %+v, want KindEnum(test_order_status)", got)
	}
	if got := colType("data"); got.Kind != schema.KindJSON {
		t.Errorf("data.Type.Kind = %v, want KindJSON", got.Kind)
	}
	if got := colType("data_b"); got.Kind != schema.KindJSONB {
		t.Errorf("data_b.Type.Kind = %v, want KindJSONB", got.Kind)
	}
	tagsType := colType("tags")
	if tagsType.Kind != schema.KindArray || tagsType.Elem == nil || *tagsType.Elem != (schema.ColumnType{Kind: schema.KindVarchar, Length: 20}) {
		t.Errorf("tags.Type = %+v, want KindArray of VARCHAR(20)", tagsType)
	}
	scoresType := colType("scores")
	if scoresType.Kind != schema.KindArray || scoresType.Elem == nil || *scoresType.Elem != (schema.ColumnType{Kind: schema.KindNumeric, Precision: 5, Scale: 2}) {
		t.Errorf("scores.Type = %+v, want KindArray of NUMERIC(5,2)", scoresType)
	}
	flagsType := colType("flags")
	if flagsType.Kind != schema.KindArray || flagsType.Elem == nil || *flagsType.Elem != (schema.ColumnType{Kind: schema.KindBoolean}) {
		t.Errorf("flags.Type = %+v, want KindArray of KindBoolean", flagsType)
	}
	extIDsType := colType("ext_ids")
	if extIDsType.Kind != schema.KindArray || extIDsType.Elem == nil || *extIDsType.Elem != (schema.ColumnType{Kind: schema.KindUUID}) {
		t.Errorf("ext_ids.Type = %+v, want KindArray of KindUUID", extIDsType)
	}

	if len(child.Checks) != 1 || child.Checks[0].Name != "ck_test_v2_child_age" {
		t.Fatalf("Checks = %+v, want one named ck_test_v2_child_age", child.Checks)
	}
	if !strings.Contains(child.Checks[0].Expression, "age") || !strings.Contains(child.Checks[0].Expression, "0") {
		t.Errorf("Checks[0].Expression = %q, want it to mention age and 0", child.Checks[0].Expression)
	}

	if len(child.ForeignKeys) != 1 {
		t.Fatalf("ForeignKeys = %+v, want 1", child.ForeignKeys)
	}
	fk := child.ForeignKeys[0]
	if fk.OnDelete != schema.ActionCascade {
		t.Errorf("ForeignKeys[0].OnDelete = %v, want ActionCascade", fk.OnDelete)
	}
	if fk.OnUpdate != schema.ActionSetNull {
		t.Errorf("ForeignKeys[0].OnUpdate = %v, want ActionSetNull", fk.OnUpdate)
	}
}

// TestIntrospect_UnsupportedUserDefinedType_Errors proves a USER-DEFINED
// column that isn't one of the enum types Introspect itself discovered (a
// composite type, in this case, not an enum) produces a clear error
// rather than being silently mismapped. A domain-typed column does NOT
// exercise this path: information_schema.columns.data_type reports a
// domain's underlying base type (e.g. "integer"), not "USER-DEFINED", so
// Introspect treats it exactly like a plain column of that base type.
func TestIntrospect_UnsupportedUserDefinedType_Errors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	t.Cleanup(func() {
		_ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS test_composite_col CASCADE; DROP TYPE IF EXISTS test_point_type`)
	})
	setup := `
DROP TABLE IF EXISTS test_composite_col CASCADE;
DROP TYPE IF EXISTS test_point_type;
CREATE TYPE test_point_type AS (x INTEGER, y INTEGER);
CREATE TABLE test_composite_col (p test_point_type);`
	if err := conn.Exec(ctx, setup); err != nil {
		t.Fatalf("test setup failed: %v", err)
	}

	_, err = dialect.Introspect(ctx, conn, []string{"test_composite_col"})
	if err == nil || !strings.Contains(err.Error(), "unsupported user-defined type") {
		t.Fatalf("Introspect error = %v, want it to contain %q", err, "unsupported user-defined type")
	}
}

// TestIntrospect_CheckExpression_NormalizedRoundTrip exercises the
// concrete risk flagged in docs/2.fundamentals/4.constraints.md: Postgres
// can reformat a CHECK expression when storing it, so diffing the
// introspected expression against the originally authored one can
// false-positive a "changed" constraint. This documents, with a real
// database, exactly which representative shapes round-trip cleanly
// (produce no diff operation) and which don't.
func TestIntrospect_CheckExpression_NormalizedRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	t.Cleanup(func() {
		_ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS test_check_roundtrip CASCADE`)
	})

	tests := []struct {
		name       string
		expression string
		wantNoOp   bool
	}{
		{name: "simple comparison round-trips", expression: "age >= 0", wantNoOp: true},
		{
			name:       "conjunction, known false positive (each operand gets its own parens)",
			expression: "a > 0 AND b > 0",
			wantNoOp:   false,
		},
		{
			name:       "like pattern, known false positive (rewritten to the ~~ operator)",
			expression: "name LIKE 'x%'",
			wantNoOp:   false,
		},
		{
			name:       "in-list, known false positive (rewritten to = ANY (ARRAY[...]))",
			expression: "status IN ('a', 'b')",
			wantNoOp:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := `DROP TABLE IF EXISTS test_check_roundtrip CASCADE;
CREATE TABLE test_check_roundtrip (a INTEGER, b INTEGER, age INTEGER, name TEXT, status TEXT,
    CONSTRAINT ck_roundtrip CHECK (` + tt.expression + `));`
			if err := conn.Exec(ctx, setup); err != nil {
				t.Fatalf("test setup failed: %v", err)
			}

			got, err := dialect.Introspect(ctx, conn, []string{"test_check_roundtrip"})
			if err != nil {
				t.Fatalf("Introspect failed: %v", err)
			}
			introspected := tableNamed(t, got, "test_check_roundtrip")

			desired := schema.Schema{Tables: []schema.Table{{
				Name:    "test_check_roundtrip",
				Columns: introspected.Columns,
				Checks:  []schema.Check{{Name: "ck_roundtrip", Expression: tt.expression}},
			}}}
			current := schema.Schema{Tables: []schema.Table{introspected}}

			ops, err := migrate.Diff(current, desired)
			if err != nil {
				t.Fatalf("Diff failed: %v", err)
			}
			gotNoOp := len(ops) == 0
			if gotNoOp != tt.wantNoOp {
				t.Errorf("expression %q: introspected as %q, Diff produced %d ops, want no-op=%v",
					tt.expression, introspected.Checks[0].Expression, len(ops), tt.wantNoOp)
			}
		})
	}
}

// TestIntrospect_ForeignKeyActions_AllValues covers the referential
// actions TestIntrospect_NewColumnKindsAndConstraints doesn't (SET
// DEFAULT, RESTRICT, and NO ACTION declared explicitly).
func TestIntrospect_ForeignKeyActions_AllValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dialect := postgres.Dialect{}
	conn, err := dialect.Connect(ctx, dsn())
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	t.Cleanup(func() {
		_ = conn.Exec(context.Background(), `DROP TABLE IF EXISTS test_fk_action_child, test_fk_action_parent CASCADE`)
	})

	tests := []struct {
		name     string
		clause   string
		onDelete schema.ForeignKeyAction
	}{
		{name: "set default", clause: "ON DELETE SET DEFAULT", onDelete: schema.ActionSetDefault},
		{name: "restrict", clause: "ON DELETE RESTRICT", onDelete: schema.ActionRestrict},
		{name: "no action declared explicitly", clause: "ON DELETE NO ACTION", onDelete: schema.ActionNoAction},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := `
DROP TABLE IF EXISTS test_fk_action_child, test_fk_action_parent CASCADE;
CREATE TABLE test_fk_action_parent (id INTEGER GENERATED ALWAYS AS IDENTITY CONSTRAINT pk_test_fk_action_parent PRIMARY KEY);
CREATE TABLE test_fk_action_child (
    parent_id INTEGER DEFAULT NULL,
    CONSTRAINT fk_test_fk_action_child_parent FOREIGN KEY (parent_id) REFERENCES test_fk_action_parent (id) ` + tt.clause + `
);`
			if err := conn.Exec(ctx, setup); err != nil {
				t.Fatalf("test setup failed: %v", err)
			}
			got, err := dialect.Introspect(ctx, conn, []string{"test_fk_action_parent", "test_fk_action_child"})
			if err != nil {
				t.Fatalf("Introspect failed: %v", err)
			}
			child := tableNamed(t, got, "test_fk_action_child")
			if len(child.ForeignKeys) != 1 {
				t.Fatalf("ForeignKeys = %+v, want 1", child.ForeignKeys)
			}
			if got := child.ForeignKeys[0].OnDelete; got != tt.onDelete {
				t.Errorf("OnDelete = %v, want %v", got, tt.onDelete)
			}
		})
	}
}

func tableNamed(t *testing.T, s schema.Schema, name string) schema.Table {
	t.Helper()
	for _, tbl := range s.Tables {
		if tbl.Name == name {
			return tbl
		}
	}
	t.Fatalf("no table named %q in %+v", name, s.Tables)
	return schema.Table{}
}
