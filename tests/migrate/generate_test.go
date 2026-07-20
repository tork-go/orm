package migrate_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
	"github.com/tork-go/orm/tests/fakedriver"
)

func TestGenerate_DispatchesEachOperationKind(t *testing.T) {
	ops := []migrate.Operation{
		migrate.CreateTable{Table: schema.Table{Name: "t"}},
		migrate.DropTable{Table: "t"},
		migrate.AddColumn{Table: "t", Column: schema.Column{Name: "c"}},
		migrate.DropColumn{Table: "t", Column: "c"},
		migrate.AlterColumnType{Table: "t", Column: schema.Column{Name: "c"}},
		migrate.AlterColumnNullability{Table: "t", Column: "c", NotNull: true},
		migrate.AddPrimaryKey{Table: "t", PrimaryKey: schema.PrimaryKey{Name: "pk_t"}},
		migrate.DropPrimaryKey{Table: "t", Name: "pk_t"},
		migrate.AddUnique{Table: "t", Unique: schema.UniqueConstraint{Name: "uq_t"}},
		migrate.DropUnique{Table: "t", Name: "uq_t"},
		migrate.AddIndex{Table: "t", Index: schema.Index{Name: "ix_t"}},
		migrate.DropIndex{Table: "t", Name: "ix_t"},
		migrate.AddCheck{Table: "t", Check: schema.Check{Name: "ck_t"}},
		migrate.DropCheck{Table: "t", Name: "ck_t"},
		migrate.AddForeignKey{Table: "t", ForeignKey: schema.ForeignKey{Name: "fk_t"}},
		migrate.DropForeignKey{Table: "t", Name: "fk_t"},
		migrate.CreateEnumType{Enum: schema.EnumType{Name: "order_status"}},
		migrate.DropEnumType{Name: "order_status"},
		migrate.AddEnumValue{Name: "order_status", Value: "cancelled"},
	}

	sql, err := migrate.Generate(fakedriver.NewDialect(), ops)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	wantFragments := []string{
		"CREATE TABLE t", "DROP TABLE t", "ADD COLUMN t.c", "DROP COLUMN t.c",
		"ALTER COLUMN TYPE t.c", "ALTER COLUMN NULLABILITY t.c true",
		"ADD PRIMARY KEY t pk_t", "DROP PRIMARY KEY pk_t",
		"ADD UNIQUE uq_t", "DROP UNIQUE uq_t",
		"ADD INDEX ix_t", "DROP INDEX ix_t",
		"ADD CHECK ck_t", "DROP CHECK ck_t",
		"ADD FOREIGN KEY fk_t", "DROP FOREIGN KEY fk_t",
		"CREATE ENUM TYPE order_status", "DROP ENUM TYPE order_status",
		"ADD ENUM VALUE order_status.cancelled",
	}
	for _, want := range wantFragments {
		if !strings.Contains(sql, want) {
			t.Errorf("Generate() output missing %q; got:\n%s", want, sql)
		}
	}

	if got, want := strings.Count(sql, ";"), len(ops); got != want {
		t.Errorf("Generate() produced %d statements, want %d: %s", got, want, sql)
	}
}

// TestGenerate_PropagatesRenderErrors covers the three Render* methods
// that can fail (CreateTable, AddColumn, AlterColumnType, all of which map
// a schema.Kind to a type string). AddIndex/DropIndex, like DropTable and
// DropColumn, are pure formatting and never return an error, so they need
// no case here.
func TestGenerate_PropagatesRenderErrors(t *testing.T) {
	dialect := fakedriver.NewDialect()
	dialect.FailRender = true

	tests := []struct {
		name string
		op   migrate.Operation
	}{
		{name: "CreateTable", op: migrate.CreateTable{Table: schema.Table{Name: "t"}}},
		{name: "AddColumn", op: migrate.AddColumn{Table: "t", Column: schema.Column{Name: "c"}}},
		{name: "AlterColumnType", op: migrate.AlterColumnType{Table: "t", Column: schema.Column{Name: "c"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := migrate.Generate(dialect, []migrate.Operation{tt.op}); err == nil {
				t.Fatalf("Generate(%s) succeeded, want the simulated render error", tt.name)
			}
		})
	}
}

func TestGenerate_EmptyOperations(t *testing.T) {
	sql, err := migrate.Generate(fakedriver.NewDialect(), nil)
	if err != nil {
		t.Fatalf("Generate(nil) failed: %v", err)
	}
	if sql != "" {
		t.Errorf("Generate(nil) = %q, want empty string", sql)
	}
}
