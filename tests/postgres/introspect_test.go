//go:build integration

package postgres_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/tork-go/orm/driver/postgres"
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
    CONSTRAINT pk_test_introspect_child PRIMARY KEY (parent_id, seq),
    CONSTRAINT fk_test_introspect_child_parent FOREIGN KEY (parent_id) REFERENCES test_introspect_parent (id)
);`
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
	}
	if !reflect.DeepEqual(child.Columns, wantChildColumns) {
		t.Errorf("child.Columns = %+v, want %+v", child.Columns, wantChildColumns)
	}
	wantChildPK := &schema.PrimaryKey{Name: "pk_test_introspect_child", Columns: []string{"parent_id", "seq"}}
	if !reflect.DeepEqual(child.PrimaryKey, wantChildPK) {
		t.Errorf("child.PrimaryKey = %+v, want %+v", child.PrimaryKey, wantChildPK)
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
