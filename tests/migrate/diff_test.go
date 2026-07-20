package migrate_test

import (
	"testing"

	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

func table(name string) schema.Table { return schema.Table{Name: name} }

func TestDiff_NoChanges(t *testing.T) {
	s := schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
	}}}
	if ops := migrate.Diff(s, s); len(ops) != 0 {
		t.Errorf("Diff(s, s) = %v, want no operations", ops)
	}
}

func TestDiff_CreateTable_NoForeignKeys(t *testing.T) {
	desired := schema.Schema{Tables: []schema.Table{{
		Name:       "users",
		Columns:    []schema.Column{{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
		PrimaryKey: &schema.PrimaryKey{Name: "pk_users", Columns: []string{"id"}},
	}}}

	ops := migrate.Diff(schema.Schema{}, desired)
	if len(ops) != 1 {
		t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
	}
	ct, ok := ops[0].(migrate.CreateTable)
	if !ok {
		t.Fatalf("ops[0] is %T, want CreateTable", ops[0])
	}
	if ct.Table.Name != "users" || ct.Table.PrimaryKey == nil {
		t.Errorf("CreateTable = %+v, want table users with a primary key", ct.Table)
	}
}

func TestDiff_CreateTable_ForeignKeyIsSeparateOp(t *testing.T) {
	desired := schema.Schema{Tables: []schema.Table{{
		Name:    "posts",
		Columns: []schema.Column{{Name: "author_id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
		ForeignKeys: []schema.ForeignKey{{
			Name: "fk_posts_author_id", Columns: []string{"author_id"},
			ReferencedTable: "users", ReferencedColumns: []string{"id"},
		}},
	}}}

	ops := migrate.Diff(schema.Schema{}, desired)
	if len(ops) != 2 {
		t.Fatalf("got %d ops, want 2 (CreateTable, AddForeignKey): %+v", len(ops), ops)
	}
	ct, ok := ops[0].(migrate.CreateTable)
	if !ok {
		t.Fatalf("ops[0] is %T, want CreateTable", ops[0])
	}
	if len(ct.Table.ForeignKeys) != 0 {
		t.Errorf("CreateTable.Table.ForeignKeys = %v, want none (foreign keys are always a separate op)", ct.Table.ForeignKeys)
	}
	if _, ok := ops[1].(migrate.AddForeignKey); !ok {
		t.Fatalf("ops[1] is %T, want AddForeignKey", ops[1])
	}
}

func TestDiff_DropTable(t *testing.T) {
	current := schema.Schema{Tables: []schema.Table{table("users")}}
	ops := migrate.Diff(current, schema.Schema{})
	if len(ops) != 1 {
		t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
	}
	if got, ok := ops[0].(migrate.DropTable); !ok || got.Table != "users" {
		t.Errorf("ops[0] = %+v, want DropTable{Table: users}", ops[0])
	}
}

func TestDiff_DropTable_ForeignKeyDroppedFirst(t *testing.T) {
	current := schema.Schema{Tables: []schema.Table{{
		Name: "posts",
		ForeignKeys: []schema.ForeignKey{{
			Name: "fk_posts_author_id", Columns: []string{"author_id"},
			ReferencedTable: "users", ReferencedColumns: []string{"id"},
		}},
	}}}
	ops := migrate.Diff(current, schema.Schema{})
	if len(ops) != 2 {
		t.Fatalf("got %d ops, want 2: %+v", len(ops), ops)
	}
	if _, ok := ops[0].(migrate.DropForeignKey); !ok {
		t.Errorf("ops[0] = %T, want DropForeignKey (must come before DropTable)", ops[0])
	}
	if _, ok := ops[1].(migrate.DropTable); !ok {
		t.Errorf("ops[1] = %T, want DropTable", ops[1])
	}
}

func TestDiff_AddColumn(t *testing.T) {
	current := schema.Schema{Tables: []schema.Table{table("users")}}
	desired := schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "age", Type: schema.ColumnType{Kind: schema.KindInteger}}},
	}}}
	ops := migrate.Diff(current, desired)
	if len(ops) != 1 {
		t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
	}
	got, ok := ops[0].(migrate.AddColumn)
	if !ok || got.Table != "users" || got.Column.Name != "age" {
		t.Errorf("ops[0] = %+v, want AddColumn{Table: users, Column.Name: age}", ops[0])
	}
}

func TestDiff_DropColumn(t *testing.T) {
	current := schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "age", Type: schema.ColumnType{Kind: schema.KindInteger}}},
	}}}
	desired := schema.Schema{Tables: []schema.Table{table("users")}}
	ops := migrate.Diff(current, desired)
	if len(ops) != 1 {
		t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
	}
	got, ok := ops[0].(migrate.DropColumn)
	if !ok || got.Table != "users" || got.Column != "age" {
		t.Errorf("ops[0] = %+v, want DropColumn{Table: users, Column: age}", ops[0])
	}
}

func TestDiff_AlterColumnType(t *testing.T) {
	current := schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "age", Type: schema.ColumnType{Kind: schema.KindInteger}}},
	}}}
	desired := schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "age", Type: schema.ColumnType{Kind: schema.KindBigInteger}}},
	}}}
	ops := migrate.Diff(current, desired)
	if len(ops) != 1 {
		t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
	}
	got, ok := ops[0].(migrate.AlterColumnType)
	if !ok || got.Column.Type.Kind != schema.KindBigInteger {
		t.Errorf("ops[0] = %+v, want AlterColumnType to KindBigInteger", ops[0])
	}
}

func TestDiff_AlterColumnNullability(t *testing.T) {
	current := schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "age", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: false}},
	}}}
	desired := schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "age", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
	}}}
	ops := migrate.Diff(current, desired)
	if len(ops) != 1 {
		t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
	}
	got, ok := ops[0].(migrate.AlterColumnNullability)
	if !ok || got.Column != "age" || !got.NotNull {
		t.Errorf("ops[0] = %+v, want AlterColumnNullability{Column: age, NotNull: true}", ops[0])
	}
}

func TestDiff_PrimaryKey_AddDropAndChange(t *testing.T) {
	noPK := schema.Table{Name: "users"}
	pkA := schema.Table{Name: "users", PrimaryKey: &schema.PrimaryKey{Name: "pk_users", Columns: []string{"a"}}}
	pkB := schema.Table{Name: "users", PrimaryKey: &schema.PrimaryKey{Name: "pk_users", Columns: []string{"b"}}}

	t.Run("add", func(t *testing.T) {
		ops := migrate.Diff(schema.Schema{Tables: []schema.Table{noPK}}, schema.Schema{Tables: []schema.Table{pkA}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.AddPrimaryKey); !ok {
			t.Errorf("ops[0] = %T, want AddPrimaryKey", ops[0])
		}
	})
	t.Run("drop", func(t *testing.T) {
		ops := migrate.Diff(schema.Schema{Tables: []schema.Table{pkA}}, schema.Schema{Tables: []schema.Table{noPK}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.DropPrimaryKey); !ok {
			t.Errorf("ops[0] = %T, want DropPrimaryKey", ops[0])
		}
	})
	t.Run("change columns", func(t *testing.T) {
		ops := migrate.Diff(schema.Schema{Tables: []schema.Table{pkA}}, schema.Schema{Tables: []schema.Table{pkB}})
		if len(ops) != 2 {
			t.Fatalf("got %d ops, want 2 (DropPrimaryKey, AddPrimaryKey): %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.DropPrimaryKey); !ok {
			t.Errorf("ops[0] = %T, want DropPrimaryKey", ops[0])
		}
		if _, ok := ops[1].(migrate.AddPrimaryKey); !ok {
			t.Errorf("ops[1] = %T, want AddPrimaryKey", ops[1])
		}
	})
}

func TestDiff_UniqueAndForeignKey_AddDrop(t *testing.T) {
	withUnique := schema.Table{Name: "users", Uniques: []schema.UniqueConstraint{{Name: "uq_users_email", Columns: []string{"email"}}}}
	withoutUnique := schema.Table{Name: "users"}

	t.Run("add unique", func(t *testing.T) {
		ops := migrate.Diff(schema.Schema{Tables: []schema.Table{withoutUnique}}, schema.Schema{Tables: []schema.Table{withUnique}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.AddUnique); !ok {
			t.Errorf("ops[0] = %T, want AddUnique", ops[0])
		}
	})
	t.Run("drop unique", func(t *testing.T) {
		ops := migrate.Diff(schema.Schema{Tables: []schema.Table{withUnique}}, schema.Schema{Tables: []schema.Table{withoutUnique}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.DropUnique); !ok {
			t.Errorf("ops[0] = %T, want DropUnique", ops[0])
		}
	})

	withFK := schema.Table{Name: "posts", ForeignKeys: []schema.ForeignKey{{
		Name: "fk_posts_author_id", Columns: []string{"author_id"},
		ReferencedTable: "users", ReferencedColumns: []string{"id"},
	}}}
	withoutFK := schema.Table{Name: "posts"}

	t.Run("add foreign key on existing table", func(t *testing.T) {
		ops := migrate.Diff(schema.Schema{Tables: []schema.Table{withoutFK}}, schema.Schema{Tables: []schema.Table{withFK}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.AddForeignKey); !ok {
			t.Errorf("ops[0] = %T, want AddForeignKey", ops[0])
		}
	})
	t.Run("drop foreign key on existing table", func(t *testing.T) {
		ops := migrate.Diff(schema.Schema{Tables: []schema.Table{withFK}}, schema.Schema{Tables: []schema.Table{withoutFK}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.DropForeignKey); !ok {
			t.Errorf("ops[0] = %T, want DropForeignKey", ops[0])
		}
	})
}

// TestDiff_LegacyUnnamedConstraint proves a live constraint that already
// covers the desired columns, but under a different (e.g. Postgres
// auto-generated) name, is left alone rather than proposed as a rename.
func TestDiff_LegacyUnnamedConstraint(t *testing.T) {
	current := schema.Table{
		Name:       "users",
		PrimaryKey: &schema.PrimaryKey{Name: "users_pkey", Columns: []string{"id"}},
		Uniques:    []schema.UniqueConstraint{{Name: "users_email_key", Columns: []string{"email"}}},
	}
	desired := schema.Table{
		Name:       "users",
		PrimaryKey: &schema.PrimaryKey{Name: "pk_users", Columns: []string{"id"}},
		Uniques:    []schema.UniqueConstraint{{Name: "uq_users_email", Columns: []string{"email"}}},
	}

	ops := migrate.Diff(schema.Schema{Tables: []schema.Table{current}}, schema.Schema{Tables: []schema.Table{desired}})
	if len(ops) != 0 {
		t.Errorf("Diff() = %+v, want no operations (same columns, different names)", ops)
	}
}

// TestDiff_OperationOrdering builds a diff touching many operation kinds
// at once and asserts they come back in the twelve-phase order.
func TestDiff_OperationOrdering(t *testing.T) {
	current := schema.Schema{Tables: []schema.Table{
		{
			Name: "dropped_table",
			ForeignKeys: []schema.ForeignKey{{
				Name: "fk_dropped_table_x", Columns: []string{"x"},
				ReferencedTable: "other", ReferencedColumns: []string{"id"},
			}},
		},
		{
			Name: "users",
			Columns: []schema.Column{
				{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true},
				{Name: "old_col", Type: schema.ColumnType{Kind: schema.KindText}},
			},
			PrimaryKey: &schema.PrimaryKey{Name: "pk_users", Columns: []string{"id"}},
			Uniques:    []schema.UniqueConstraint{{Name: "uq_users_email", Columns: []string{"email"}}},
		},
	}}
	desired := schema.Schema{Tables: []schema.Table{
		{
			Name: "users",
			Columns: []schema.Column{
				{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true},
				{Name: "new_col", Type: schema.ColumnType{Kind: schema.KindText}},
			},
			PrimaryKey: &schema.PrimaryKey{Name: "pk_users", Columns: []string{"id"}},
		},
		{
			Name:    "new_table",
			Columns: []schema.Column{{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
			ForeignKeys: []schema.ForeignKey{{
				Name: "fk_new_table_users", Columns: []string{"id"},
				ReferencedTable: "users", ReferencedColumns: []string{"id"},
			}},
		},
	}}

	ops := migrate.Diff(current, desired)
	if len(ops) == 0 {
		t.Fatal("expected a non-empty diff")
	}

	phaseOrder := map[string]int{
		"migrate.DropForeignKey":         0,
		"migrate.DropUnique":             1,
		"migrate.DropPrimaryKey":         2,
		"migrate.DropColumn":             3,
		"migrate.DropTable":              4,
		"migrate.CreateTable":            5,
		"migrate.AddColumn":              6,
		"migrate.AlterColumnType":        7,
		"migrate.AlterColumnNullability": 8,
		"migrate.AddPrimaryKey":          9,
		"migrate.AddUnique":              10,
		"migrate.AddForeignKey":          11,
	}

	last := -1
	for _, op := range ops {
		phase, ok := phaseOrder[typeName(op)]
		if !ok {
			t.Fatalf("unknown operation type %T", op)
		}
		if phase < last {
			t.Fatalf("operation %+v (phase %d) came after phase %d, ops out of order: %+v", op, phase, last, ops)
		}
		last = phase
	}
}

// TestDiff_SortsWithinPhaseByTableThenName proves multiple operations in
// the same phase come back sorted by table name, then by column or
// constraint name, regardless of input order.
func TestDiff_SortsWithinPhaseByTableThenName(t *testing.T) {
	current := schema.Schema{Tables: []schema.Table{
		{Name: "zebra"}, {Name: "apple"}, {Name: "mango"},
	}}
	desired := schema.Schema{Tables: []schema.Table{
		{Name: "zebra", Columns: []schema.Column{
			{Name: "z_col", Type: schema.ColumnType{Kind: schema.KindInteger}},
			{Name: "a_col", Type: schema.ColumnType{Kind: schema.KindInteger}},
		}},
		{Name: "apple", Columns: []schema.Column{{Name: "x", Type: schema.ColumnType{Kind: schema.KindInteger}}}},
		{Name: "mango", Columns: []schema.Column{{Name: "y", Type: schema.ColumnType{Kind: schema.KindInteger}}}},
	}}

	ops := migrate.Diff(current, desired)
	if len(ops) != 4 {
		t.Fatalf("got %d ops, want 4: %+v", len(ops), ops)
	}

	var got []string
	for _, op := range ops {
		ac, ok := op.(migrate.AddColumn)
		if !ok {
			t.Fatalf("op %+v is %T, want AddColumn", op, op)
		}
		got = append(got, ac.Table+"."+ac.Column.Name)
	}
	want := []string{"apple.x", "mango.y", "zebra.a_col", "zebra.z_col"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ops[%d] = %s, want %s (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestDiff_ReverseIsCorrectlyOrderedInverse proves that generating down
// SQL by swapping Diff's arguments produces the structural inverse: every
// add in the forward diff has a matching drop in the reverse diff and
// vice versa, and the reverse diff is itself phase-ordered.
func TestDiff_ReverseIsCorrectlyOrderedInverse(t *testing.T) {
	before := schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
	}}}
	after := schema.Schema{Tables: []schema.Table{
		{
			Name: "users",
			Columns: []schema.Column{
				{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true},
				{Name: "email", Type: schema.ColumnType{Kind: schema.KindText}},
			},
		},
		{
			Name:    "posts",
			Columns: []schema.Column{{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
		},
	}}

	up := migrate.Diff(before, after)
	down := migrate.Diff(after, before)

	if len(up) != len(down) {
		t.Fatalf("len(up)=%d, len(down)=%d, want equal (same changes, reversed)", len(up), len(down))
	}

	upHasCreatePosts, downHasDropPosts := false, false
	upHasAddEmail, downHasDropEmail := false, false
	for _, op := range up {
		if ct, ok := op.(migrate.CreateTable); ok && ct.Table.Name == "posts" {
			upHasCreatePosts = true
		}
		if ac, ok := op.(migrate.AddColumn); ok && ac.Column.Name == "email" {
			upHasAddEmail = true
		}
	}
	for _, op := range down {
		if dt, ok := op.(migrate.DropTable); ok && dt.Table == "posts" {
			downHasDropPosts = true
		}
		if dc, ok := op.(migrate.DropColumn); ok && dc.Column == "email" {
			downHasDropEmail = true
		}
	}
	if !upHasCreatePosts || !downHasDropPosts {
		t.Errorf("expected up to CreateTable posts and down to DropTable posts: up=%+v down=%+v", up, down)
	}
	if !upHasAddEmail || !downHasDropEmail {
		t.Errorf("expected up to AddColumn email and down to DropColumn email: up=%+v down=%+v", up, down)
	}
}

func typeName(op migrate.Operation) string {
	switch op.(type) {
	case migrate.CreateTable:
		return "migrate.CreateTable"
	case migrate.DropTable:
		return "migrate.DropTable"
	case migrate.AddColumn:
		return "migrate.AddColumn"
	case migrate.DropColumn:
		return "migrate.DropColumn"
	case migrate.AlterColumnType:
		return "migrate.AlterColumnType"
	case migrate.AlterColumnNullability:
		return "migrate.AlterColumnNullability"
	case migrate.AddPrimaryKey:
		return "migrate.AddPrimaryKey"
	case migrate.DropPrimaryKey:
		return "migrate.DropPrimaryKey"
	case migrate.AddUnique:
		return "migrate.AddUnique"
	case migrate.DropUnique:
		return "migrate.DropUnique"
	case migrate.AddForeignKey:
		return "migrate.AddForeignKey"
	case migrate.DropForeignKey:
		return "migrate.DropForeignKey"
	default:
		return "unknown"
	}
}
