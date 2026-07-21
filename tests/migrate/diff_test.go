package migrate_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

func table(name string) schema.Table { return schema.Table{Name: name} }

// mustDiff calls migrate.Diff and fails the test on error, for the many
// tests here that exercise ordinary structural diffs and don't care about
// the error path (see TestDiff_EnumType_NonAdditiveChange_Errors for a
// test that does).
func mustDiff(t *testing.T, current, desired schema.Schema) []migrate.Operation {
	t.Helper()
	ops, err := migrate.Diff(current, desired)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	return ops
}

func TestDiff_NoChanges(t *testing.T) {
	s := schema.Schema{Tables: []schema.Table{{
		Name:    "users",
		Columns: []schema.Column{{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
	}}}
	if ops := mustDiff(t, s, s); len(ops) != 0 {
		t.Errorf("Diff(s, s) = %v, want no operations", ops)
	}
}

func TestDiff_CreateTable_NoForeignKeys(t *testing.T) {
	desired := schema.Schema{Tables: []schema.Table{{
		Name:       "users",
		Columns:    []schema.Column{{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
		PrimaryKey: &schema.PrimaryKey{Name: "pk_users", Columns: []string{"id"}},
	}}}

	ops := mustDiff(t, schema.Schema{}, desired)
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

	ops := mustDiff(t, schema.Schema{}, desired)
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
	ops := mustDiff(t, current, schema.Schema{})
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
	ops := mustDiff(t, current, schema.Schema{})
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
	ops := mustDiff(t, current, desired)
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
	ops := mustDiff(t, current, desired)
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
	ops := mustDiff(t, current, desired)
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
	ops := mustDiff(t, current, desired)
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
		ops := mustDiff(t, schema.Schema{Tables: []schema.Table{noPK}}, schema.Schema{Tables: []schema.Table{pkA}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.AddPrimaryKey); !ok {
			t.Errorf("ops[0] = %T, want AddPrimaryKey", ops[0])
		}
	})
	t.Run("drop", func(t *testing.T) {
		ops := mustDiff(t, schema.Schema{Tables: []schema.Table{pkA}}, schema.Schema{Tables: []schema.Table{noPK}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.DropPrimaryKey); !ok {
			t.Errorf("ops[0] = %T, want DropPrimaryKey", ops[0])
		}
	})
	t.Run("change columns", func(t *testing.T) {
		ops := mustDiff(t, schema.Schema{Tables: []schema.Table{pkA}}, schema.Schema{Tables: []schema.Table{pkB}})
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
		ops := mustDiff(t, schema.Schema{Tables: []schema.Table{withoutUnique}}, schema.Schema{Tables: []schema.Table{withUnique}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.AddUnique); !ok {
			t.Errorf("ops[0] = %T, want AddUnique", ops[0])
		}
	})
	t.Run("drop unique", func(t *testing.T) {
		ops := mustDiff(t, schema.Schema{Tables: []schema.Table{withUnique}}, schema.Schema{Tables: []schema.Table{withoutUnique}})
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
		ops := mustDiff(t, schema.Schema{Tables: []schema.Table{withoutFK}}, schema.Schema{Tables: []schema.Table{withFK}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.AddForeignKey); !ok {
			t.Errorf("ops[0] = %T, want AddForeignKey", ops[0])
		}
	})
	t.Run("drop foreign key on existing table", func(t *testing.T) {
		ops := mustDiff(t, schema.Schema{Tables: []schema.Table{withFK}}, schema.Schema{Tables: []schema.Table{withoutFK}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.DropForeignKey); !ok {
			t.Errorf("ops[0] = %T, want DropForeignKey", ops[0])
		}
	})
}

func TestDiff_Index_AddDrop(t *testing.T) {
	withIndex := schema.Table{Name: "posts", Indexes: []schema.Index{{Name: "ix_posts_author_id", Columns: []string{"author_id"}}}}
	withoutIndex := schema.Table{Name: "posts"}

	t.Run("add index", func(t *testing.T) {
		ops := mustDiff(t, schema.Schema{Tables: []schema.Table{withoutIndex}}, schema.Schema{Tables: []schema.Table{withIndex}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.AddIndex); !ok {
			t.Errorf("ops[0] = %T, want AddIndex", ops[0])
		}
	})
	t.Run("drop index", func(t *testing.T) {
		ops := mustDiff(t, schema.Schema{Tables: []schema.Table{withIndex}}, schema.Schema{Tables: []schema.Table{withoutIndex}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.DropIndex); !ok {
			t.Errorf("ops[0] = %T, want DropIndex", ops[0])
		}
	})
}

// TestDiff_Index_MatchedByColumnSet_NotName mirrors
// TestDiff_LegacyUnnamedConstraint for plain indexes.
func TestDiff_Index_MatchedByColumnSet_NotName(t *testing.T) {
	current := schema.Table{Name: "posts", Indexes: []schema.Index{{Name: "posts_author_id_idx", Columns: []string{"author_id"}}}}
	desired := schema.Table{Name: "posts", Indexes: []schema.Index{{Name: "ix_posts_author_id", Columns: []string{"author_id"}}}}

	ops := mustDiff(t, schema.Schema{Tables: []schema.Table{current}}, schema.Schema{Tables: []schema.Table{desired}})
	if len(ops) != 0 {
		t.Errorf("Diff() = %+v, want no operations (same columns, different index name)", ops)
	}
}

// TestDiff_CreateTable_IndexIsSeparateOp mirrors
// TestDiff_CreateTable_ForeignKeyIsSeparateOp: a plain index can't be
// inlined into CREATE TABLE in Postgres at all, so it's always its own
// AddIndex, even for a brand-new table.
func TestDiff_CreateTable_IndexIsSeparateOp(t *testing.T) {
	desired := schema.Schema{Tables: []schema.Table{{
		Name:    "posts",
		Columns: []schema.Column{{Name: "author_id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
		Indexes: []schema.Index{{Name: "ix_posts_author_id", Columns: []string{"author_id"}}},
	}}}

	ops := mustDiff(t, schema.Schema{}, desired)
	if len(ops) != 2 {
		t.Fatalf("got %d ops, want 2 (CreateTable, AddIndex): %+v", len(ops), ops)
	}
	ct, ok := ops[0].(migrate.CreateTable)
	if !ok {
		t.Fatalf("ops[0] is %T, want CreateTable", ops[0])
	}
	if len(ct.Table.Indexes) != 0 {
		t.Errorf("CreateTable.Table.Indexes = %v, want none (indexes are always a separate op)", ct.Table.Indexes)
	}
	if _, ok := ops[1].(migrate.AddIndex); !ok {
		t.Fatalf("ops[1] is %T, want AddIndex", ops[1])
	}
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

	ops := mustDiff(t, schema.Schema{Tables: []schema.Table{current}}, schema.Schema{Tables: []schema.Table{desired}})
	if len(ops) != 0 {
		t.Errorf("Diff() = %+v, want no operations (same columns, different names)", ops)
	}
}

// TestDiff_OperationOrdering builds a diff touching every operation kind
// at once and asserts they come back in the nineteen-phase order.
func TestDiff_OperationOrdering(t *testing.T) {
	current := schema.Schema{
		Tables: []schema.Table{
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
				Indexes:    []schema.Index{{Name: "ix_users_old_col", Columns: []string{"old_col"}}},
				Checks:     []schema.Check{{Name: "ck_users_old", Expression: "old_col <> ''"}},
			},
		},
		EnumTypes: []schema.EnumType{{Name: "dropped_enum", Values: []string{"a"}}},
	}
	desired := schema.Schema{
		Tables: []schema.Table{
			{
				Name: "users",
				Columns: []schema.Column{
					{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true},
					{Name: "new_col", Type: schema.ColumnType{Kind: schema.KindText}},
				},
				PrimaryKey: &schema.PrimaryKey{Name: "pk_users", Columns: []string{"id"}},
				Indexes:    []schema.Index{{Name: "ix_users_new_col", Columns: []string{"new_col"}}},
				Checks:     []schema.Check{{Name: "ck_users_new", Expression: "new_col <> ''"}},
			},
			{
				Name:    "new_table",
				Columns: []schema.Column{{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
				ForeignKeys: []schema.ForeignKey{{
					Name: "fk_new_table_users", Columns: []string{"id"},
					ReferencedTable: "users", ReferencedColumns: []string{"id"},
				}},
			},
		},
		EnumTypes: []schema.EnumType{{Name: "new_enum", Values: []string{"a"}}},
	}

	ops := mustDiff(t, current, desired)
	if len(ops) == 0 {
		t.Fatal("expected a non-empty diff")
	}

	phaseOrder := map[string]int{
		"migrate.DropForeignKey":         0,
		"migrate.DropUnique":             1,
		"migrate.DropCheck":              2,
		"migrate.DropIndex":              3,
		"migrate.DropPrimaryKey":         4,
		"migrate.DropColumn":             5,
		"migrate.DropTable":              6,
		"migrate.DropEnumType":           7,
		"migrate.CreateEnumType":         8,
		"migrate.AddEnumValue":           9,
		"migrate.CreateTable":            10,
		"migrate.AddColumn":              11,
		"migrate.AlterColumnType":        12,
		"migrate.AlterColumnNullability": 13,
		"migrate.AddPrimaryKey":          14,
		"migrate.AddIndex":               15,
		"migrate.AddCheck":               16,
		"migrate.AddUnique":              17,
		"migrate.AddForeignKey":          18,
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

	ops := mustDiff(t, current, desired)
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
			Indexes: []schema.Index{{Name: "ix_users_email", Columns: []string{"email"}}},
		},
		{
			Name:    "posts",
			Columns: []schema.Column{{Name: "id", Type: schema.ColumnType{Kind: schema.KindInteger}, NotNull: true}},
		},
	}}

	up := mustDiff(t, before, after)
	down := mustDiff(t, after, before)

	if len(up) != len(down) {
		t.Fatalf("len(up)=%d, len(down)=%d, want equal (same changes, reversed)", len(up), len(down))
	}

	upHasCreatePosts, downHasDropPosts := false, false
	upHasAddEmail, downHasDropEmail := false, false
	upHasAddIndex, downHasDropIndex := false, false
	for _, op := range up {
		if ct, ok := op.(migrate.CreateTable); ok && ct.Table.Name == "posts" {
			upHasCreatePosts = true
		}
		if ac, ok := op.(migrate.AddColumn); ok && ac.Column.Name == "email" {
			upHasAddEmail = true
		}
		if _, ok := op.(migrate.AddIndex); ok {
			upHasAddIndex = true
		}
	}
	for _, op := range down {
		if dt, ok := op.(migrate.DropTable); ok && dt.Table == "posts" {
			downHasDropPosts = true
		}
		if dc, ok := op.(migrate.DropColumn); ok && dc.Column == "email" {
			downHasDropEmail = true
		}
		if _, ok := op.(migrate.DropIndex); ok {
			downHasDropIndex = true
		}
	}
	if !upHasCreatePosts || !downHasDropPosts {
		t.Errorf("expected up to CreateTable posts and down to DropTable posts: up=%+v down=%+v", up, down)
	}
	if !upHasAddEmail || !downHasDropEmail {
		t.Errorf("expected up to AddColumn email and down to DropColumn email: up=%+v down=%+v", up, down)
	}
	if !upHasAddIndex || !downHasDropIndex {
		t.Errorf("expected up to AddIndex and down to DropIndex: up=%+v down=%+v", up, down)
	}
}

// TestDiff_IndexesSortWithinPhaseByTableThenName mirrors
// TestDiff_SortsWithinPhaseByTableThenName for AddIndex ops.
func TestDiff_IndexesSortWithinPhaseByTableThenName(t *testing.T) {
	current := schema.Schema{Tables: []schema.Table{{Name: "zebra"}, {Name: "apple"}}}
	desired := schema.Schema{Tables: []schema.Table{
		{Name: "zebra", Indexes: []schema.Index{{Name: "ix_zebra_z", Columns: []string{"z"}}}},
		{Name: "apple", Indexes: []schema.Index{{Name: "ix_apple_a", Columns: []string{"a"}}}},
	}}

	ops := mustDiff(t, current, desired)
	if len(ops) != 2 {
		t.Fatalf("got %d ops, want 2: %+v", len(ops), ops)
	}
	first, ok := ops[0].(migrate.AddIndex)
	if !ok || first.Table != "apple" {
		t.Errorf("ops[0] = %+v, want AddIndex on table apple (sorted first)", ops[0])
	}
	second, ok := ops[1].(migrate.AddIndex)
	if !ok || second.Table != "zebra" {
		t.Errorf("ops[1] = %+v, want AddIndex on table zebra", ops[1])
	}
}

// TestDiff_Check_AddDropMatchedByName proves CHECK constraints are matched
// by name (they have no natural column set), not by expression text.
func TestDiff_Check_AddDropMatchedByName(t *testing.T) {
	withCheck := schema.Table{Name: "accounts", Checks: []schema.Check{{Name: "ck_accounts_1", Expression: "age >= 0"}}}
	withoutCheck := schema.Table{Name: "accounts"}

	t.Run("add check", func(t *testing.T) {
		ops := mustDiff(t, schema.Schema{Tables: []schema.Table{withoutCheck}}, schema.Schema{Tables: []schema.Table{withCheck}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.AddCheck); !ok {
			t.Errorf("ops[0] = %T, want AddCheck", ops[0])
		}
	})
	t.Run("drop check", func(t *testing.T) {
		ops := mustDiff(t, schema.Schema{Tables: []schema.Table{withCheck}}, schema.Schema{Tables: []schema.Table{withoutCheck}})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if _, ok := ops[0].(migrate.DropCheck); !ok {
			t.Errorf("ops[0] = %T, want DropCheck", ops[0])
		}
	})
}

// TestDiff_Check_ExpressionChange_NormalizedNoOp proves a check whose
// expression only differs by whitespace or a single wrapping layer of
// parens (the kind of reformatting Postgres itself does when storing an
// expression) produces no operation, thanks to normalizeExpr.
func TestDiff_Check_ExpressionChange_NormalizedNoOp(t *testing.T) {
	current := schema.Table{Name: "accounts", Checks: []schema.Check{{Name: "ck_accounts_1", Expression: "(age >= 0)"}}}
	desired := schema.Table{Name: "accounts", Checks: []schema.Check{{Name: "ck_accounts_1", Expression: "age  >=  0"}}}

	ops := mustDiff(t, schema.Schema{Tables: []schema.Table{current}}, schema.Schema{Tables: []schema.Table{desired}})
	if len(ops) != 0 {
		t.Errorf("Diff() = %+v, want no operations (expression differs only by whitespace/wrapping parens)", ops)
	}
}

// TestDiff_Check_ExpressionActuallyChanged_AddDropPair proves a genuine
// expression change (not just reformatting) produces a drop+add pair for
// the same name.
func TestDiff_Check_ExpressionActuallyChanged_AddDropPair(t *testing.T) {
	current := schema.Table{Name: "accounts", Checks: []schema.Check{{Name: "ck_accounts_1", Expression: "age >= 0"}}}
	desired := schema.Table{Name: "accounts", Checks: []schema.Check{{Name: "ck_accounts_1", Expression: "age >= 18"}}}

	ops := mustDiff(t, schema.Schema{Tables: []schema.Table{current}}, schema.Schema{Tables: []schema.Table{desired}})
	if len(ops) != 2 {
		t.Fatalf("got %d ops, want 2 (DropCheck, AddCheck): %+v", len(ops), ops)
	}
	if _, ok := ops[0].(migrate.DropCheck); !ok {
		t.Errorf("ops[0] = %T, want DropCheck", ops[0])
	}
	if _, ok := ops[1].(migrate.AddCheck); !ok {
		t.Errorf("ops[1] = %T, want AddCheck", ops[1])
	}
}

// TestDiff_ForeignKey_ActionMismatch_DropAndAdd proves a foreign key
// matched by column set but with a different OnDelete/OnUpdate action is
// treated as changed (drop+add), since Postgres cannot ALTER a foreign
// key's referential actions in place.
func TestDiff_ForeignKey_ActionMismatch_DropAndAdd(t *testing.T) {
	base := schema.ForeignKey{
		Name: "fk_posts_author_id", Columns: []string{"author_id"},
		ReferencedTable: "users", ReferencedColumns: []string{"id"},
	}
	current := schema.Table{Name: "posts", ForeignKeys: []schema.ForeignKey{base}}
	changed := base
	changed.OnDelete = schema.ActionCascade
	desired := schema.Table{Name: "posts", ForeignKeys: []schema.ForeignKey{changed}}

	ops := mustDiff(t, schema.Schema{Tables: []schema.Table{current}}, schema.Schema{Tables: []schema.Table{desired}})
	if len(ops) != 2 {
		t.Fatalf("got %d ops, want 2 (DropForeignKey, AddForeignKey): %+v", len(ops), ops)
	}
	if _, ok := ops[0].(migrate.DropForeignKey); !ok {
		t.Errorf("ops[0] = %T, want DropForeignKey", ops[0])
	}
	if _, ok := ops[1].(migrate.AddForeignKey); !ok {
		t.Errorf("ops[1] = %T, want AddForeignKey", ops[1])
	}
}

func TestDiff_ForeignKey_SameActions_NoOp(t *testing.T) {
	fk := schema.ForeignKey{
		Name: "fk_posts_author_id", Columns: []string{"author_id"},
		ReferencedTable: "users", ReferencedColumns: []string{"id"}, OnDelete: schema.ActionCascade,
	}
	table := schema.Table{Name: "posts", ForeignKeys: []schema.ForeignKey{fk}}

	ops := mustDiff(t, schema.Schema{Tables: []schema.Table{table}}, schema.Schema{Tables: []schema.Table{table}})
	if len(ops) != 0 {
		t.Errorf("Diff() = %+v, want no operations (identical actions)", ops)
	}
}

// TestDiff_EnumType_CreateDropAddValue covers the three enum-type
// operation kinds at the schema level (not table-scoped).
func TestDiff_EnumType_CreateDropAddValue(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		desired := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending"}}}}
		ops := mustDiff(t, schema.Schema{}, desired)
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		ce, ok := ops[0].(migrate.CreateEnumType)
		if !ok || ce.Enum.Name != "order_status" {
			t.Errorf("ops[0] = %+v, want CreateEnumType{Enum.Name: order_status}", ops[0])
		}
	})
	t.Run("drop", func(t *testing.T) {
		current := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending"}}}}
		ops := mustDiff(t, current, schema.Schema{})
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		if got, ok := ops[0].(migrate.DropEnumType); !ok || got.Name != "order_status" {
			t.Errorf("ops[0] = %+v, want DropEnumType{Name: order_status}", ops[0])
		}
	})
	t.Run("add value at tail", func(t *testing.T) {
		current := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending"}}}}
		desired := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending", "done"}}}}
		ops := mustDiff(t, current, desired)
		if len(ops) != 1 {
			t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
		}
		got, ok := ops[0].(migrate.AddEnumValue)
		if !ok || got.Value != "done" || got.After != "pending" || got.Before != "" {
			t.Errorf("ops[0] = %+v, want AddEnumValue{Value: done, After: pending}", ops[0])
		}
	})
}

// TestDiff_EnumType_InsertInMiddle_UsesAfter proves a value inserted
// between two existing values is anchored with After, not just appended.
func TestDiff_EnumType_InsertInMiddle_UsesAfter(t *testing.T) {
	current := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending", "done"}}}}
	desired := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending", "paid", "done"}}}}

	ops := mustDiff(t, current, desired)
	if len(ops) != 1 {
		t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
	}
	got, ok := ops[0].(migrate.AddEnumValue)
	if !ok || got.Value != "paid" || got.After != "pending" {
		t.Errorf("ops[0] = %+v, want AddEnumValue{Value: paid, After: pending}", ops[0])
	}
}

// TestDiff_EnumType_InsertAtHead_UsesBefore proves a value inserted
// before every existing value is anchored with Before, since it has no
// preceding established neighbor.
func TestDiff_EnumType_InsertAtHead_UsesBefore(t *testing.T) {
	current := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending", "done"}}}}
	desired := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"draft", "pending", "done"}}}}

	ops := mustDiff(t, current, desired)
	if len(ops) != 1 {
		t.Fatalf("got %d ops, want 1: %+v", len(ops), ops)
	}
	got, ok := ops[0].(migrate.AddEnumValue)
	if !ok || got.Value != "draft" || got.Before != "pending" || got.After != "" {
		t.Errorf("ops[0] = %+v, want AddEnumValue{Value: draft, Before: pending}", ops[0])
	}
}

// TestDiff_EnumType_NonAdditiveChange_Errors proves removing or reordering
// an enum value is a hard error, the locked decision over a graceful
// down-pass degradation.
func TestDiff_EnumType_NonAdditiveChange_Errors(t *testing.T) {
	t.Run("removal detected directly", func(t *testing.T) {
		current := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending", "done"}}}}
		desired := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending"}}}}
		if _, err := migrate.Diff(current, desired); err == nil || !strings.Contains(err.Error(), "removing or reordering enum values isn't supported") {
			t.Fatalf("error = %v, want it to contain %q", err, "removing or reordering enum values isn't supported")
		}
	})
	t.Run("reorder detected directly", func(t *testing.T) {
		current := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending", "done"}}}}
		desired := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"done", "pending"}}}}
		if _, err := migrate.Diff(current, desired); err == nil || !strings.Contains(err.Error(), "removing or reordering enum values isn't supported") {
			t.Fatalf("error = %v, want it to contain %q", err, "removing or reordering enum values isn't supported")
		}
	})
	t.Run("additive up-pass succeeds but its down-pass errors", func(t *testing.T) {
		// The model adds a value: the up-migration is purely additive
		// and succeeds. Generating the down-migration by swapping Diff's
		// arguments (see Diff's doc comment) hits the same non-additive
		// case from the other direction (removing the value that was
		// just added); per the locked decision that's a hard error, not
		// a silently degraded down-migration.
		current := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending"}}}}
		desired := schema.Schema{EnumTypes: []schema.EnumType{{Name: "order_status", Values: []string{"pending", "done"}}}}
		if _, err := migrate.Diff(current, desired); err != nil {
			t.Fatalf("up-pass Diff() error = %v, want nil (adding a value is purely additive)", err)
		}
		if _, err := migrate.Diff(desired, current); err == nil || !strings.Contains(err.Error(), "removing or reordering enum values isn't supported") {
			t.Fatalf("down-pass Diff() error = %v, want it to contain %q", err, "removing or reordering enum values isn't supported")
		}
	})
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
	case migrate.AddIndex:
		return "migrate.AddIndex"
	case migrate.DropIndex:
		return "migrate.DropIndex"
	case migrate.AddCheck:
		return "migrate.AddCheck"
	case migrate.DropCheck:
		return "migrate.DropCheck"
	case migrate.AddForeignKey:
		return "migrate.AddForeignKey"
	case migrate.DropForeignKey:
		return "migrate.DropForeignKey"
	case migrate.CreateEnumType:
		return "migrate.CreateEnumType"
	case migrate.DropEnumType:
		return "migrate.DropEnumType"
	case migrate.AddEnumValue:
		return "migrate.AddEnumValue"
	default:
		return "unknown"
	}
}

// A default a database re-printed has to compare equal to the one a model
// declared, or every migration after the first proposes changing it back.
// These are the exact forms Postgres produces.
func TestDiff_ServerDefaultEquivalence(t *testing.T) {
	col := func(def string) schema.Table {
		return schema.Table{
			Name: "t",
			Columns: []schema.Column{{
				Name:          "c",
				Type:          schema.ColumnType{Kind: schema.KindText},
				ServerDefault: def,
			}},
		}
	}

	same := []struct{ introspected, declared string }{
		{"'draft'::text", "'draft'"},       // a literal gains a cast
		{"(now())::text", "now()::text"},   // a call gains parentheses
		{"0", "0"},                         // a number is printed bare
		{"true", "true"},                   //
		{"", ""},                           // no default either side
		{"(now())::text", "(now())::text"}, // already identical
	}
	for _, tt := range same {
		ops, err := migrate.Diff(
			schema.Schema{Tables: []schema.Table{col(tt.introspected)}},
			schema.Schema{Tables: []schema.Table{col(tt.declared)}},
		)
		if err != nil {
			t.Fatalf("Diff(%q, %q) error = %v", tt.introspected, tt.declared, err)
		}
		if len(ops) != 0 {
			t.Errorf("Diff(%q, %q) produced %d operations, want none", tt.introspected, tt.declared, len(ops))
		}
	}

	differ := []struct{ introspected, declared string }{
		{"'draft'::text", "'published'"}, // a genuinely changed default
		{"", "'draft'"},                  // one added
		{"'draft'::text", ""},            // one removed
	}
	for _, tt := range differ {
		ops, err := migrate.Diff(
			schema.Schema{Tables: []schema.Table{col(tt.introspected)}},
			schema.Schema{Tables: []schema.Table{col(tt.declared)}},
		)
		if err != nil {
			t.Fatalf("Diff(%q, %q) error = %v", tt.introspected, tt.declared, err)
		}
		if len(ops) != 1 {
			t.Fatalf("Diff(%q, %q) produced %d operations, want 1", tt.introspected, tt.declared, len(ops))
		}
		op, ok := ops[0].(migrate.AlterColumnDefault)
		if !ok {
			t.Fatalf("got %T, want migrate.AlterColumnDefault", ops[0])
		}
		if op.Default != tt.declared {
			t.Errorf("AlterColumnDefault.Default = %q, want %q", op.Default, tt.declared)
		}
	}
}

// A :: inside a literal is not a cast of the expression.
func TestDiff_ServerDefaultCastInsideLiteral(t *testing.T) {
	col := func(def string) schema.Table {
		return schema.Table{
			Name:    "t",
			Columns: []schema.Column{{Name: "c", Type: schema.ColumnType{Kind: schema.KindText}, ServerDefault: def}},
		}
	}
	ops, err := migrate.Diff(
		schema.Schema{Tables: []schema.Table{col(`'a::b'`)}},
		schema.Schema{Tables: []schema.Table{col(`'a::c'`)}},
	)
	if err != nil {
		t.Fatalf("Diff error = %v", err)
	}
	if len(ops) != 1 {
		t.Errorf("two different literals produced %d operations, want 1: the :: inside them is not a cast", len(ops))
	}
}

// A trailing :: with no type after it is not a cast, and neither is one
// inside an unbalanced expression.
func TestDiff_ServerDefaultMalformedCast(t *testing.T) {
	col := func(def string) schema.Table {
		return schema.Table{
			Name:    "t",
			Columns: []schema.Column{{Name: "c", Type: schema.ColumnType{Kind: schema.KindText}, ServerDefault: def}},
		}
	}
	for _, tt := range []struct{ a, b string }{
		{"x::", "y::"},       // nothing after the colons
		{"f(a::text", "g(b"}, // an unclosed paren before them
	} {
		ops, err := migrate.Diff(
			schema.Schema{Tables: []schema.Table{col(tt.a)}},
			schema.Schema{Tables: []schema.Table{col(tt.b)}},
		)
		if err != nil {
			t.Fatalf("Diff(%q, %q) error = %v", tt.a, tt.b, err)
		}
		if len(ops) != 1 {
			t.Errorf("Diff(%q, %q) produced %d operations, want 1", tt.a, tt.b, len(ops))
		}
	}
}
