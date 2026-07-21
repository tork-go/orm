package query_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/migrate"
	"github.com/tork-go/orm/schema"
)

// The identity rule is stated twice: here, over columns, to decide which
// column an insert must not name, and in driver/postgres, over the schema
// representation, to decide which column to render as GENERATED ALWAYS AS
// IDENTITY. They cannot share an implementation because they work from
// different types, so this asserts they agree.
//
// A disagreement is not a cosmetic one. If the insert compiler named a
// column Postgres had rendered as GENERATED ALWAYS, the statement would be
// rejected outright.
func TestIdentityColumn_AgreesWithTheRenderedDDL(t *testing.T) {
	tests := []struct {
		name  string
		model orm.Model
	}{
		{"sole int key", Users},
		{"composite key", Memberships},
		{"no key", Events},
		{"client generated uuid key", Keyed},
		{"sole int key with a defaulted column", Defaulted},
		{"string key", stringKeyed},
		{"bigint key", bigKeyed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, ok := orm.IdentityColumn(tt.model)

			s, err := schema.ExtractSchema(tt.model)
			if err != nil {
				t.Fatalf("ExtractSchema() error = %v", err)
			}
			ops, err := migrate.Diff(schema.Schema{}, s)
			if err != nil {
				t.Fatalf("Diff() error = %v", err)
			}
			ddl, err := migrate.Generate(postgres.Dialect{}, ops)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			rendered := strings.Contains(ddl, "GENERATED ALWAYS AS IDENTITY")
			if ok != rendered {
				t.Fatalf("IdentityColumn() = %v but the DDL renders an identity = %v\n%s",
					ok, rendered, ddl)
			}
			if ok {
				line := `"` + col.Name() + `"`
				if !strings.Contains(ddl, line+" INTEGER GENERATED ALWAYS") &&
					!strings.Contains(ddl, line+" BIGINT GENERATED ALWAYS") {
					t.Errorf("IdentityColumn() named %q but the DDL renders another column\n%s",
						col.Name(), ddl)
				}
			}
		})
	}
}

type stringKey struct {
	Code string
	Name string
}

type stringKeyModel struct {
	orm.Table[stringKey]
	Code *orm.StringColumn
	Name *orm.StringColumn
}

var stringKeyed = orm.DefineTable[stringKey]("string_keyed", func(t *orm.TableBuilder[stringKey]) *stringKeyModel {
	return &stringKeyModel{
		Table: t.Table(),
		Code:  t.String("code").PrimaryKey(),
		Name:  t.String("name").NotNull(),
	}
})

type bigKey struct {
	ID   int64
	Name string
}

type bigKeyModel struct {
	orm.Table[bigKey]
	ID   *orm.BigIntColumn
	Name *orm.StringColumn
}

var bigKeyed = orm.DefineTable[bigKey]("big_keyed", func(t *orm.TableBuilder[bigKey]) *bigKeyModel {
	return &bigKeyModel{
		Table: t.Table(),
		ID:    t.BigInt("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
	}
})

// A key the caller supplies is written like any other column.
func TestInsert_NamesANonGeneratedKey(t *testing.T) {
	if _, ok := orm.IdentityColumn(stringKeyed); ok {
		t.Fatal("a string key was treated as generated")
	}
	if _, ok := orm.IdentityColumn(Keyed); ok {
		t.Fatal("a client generated key was treated as database generated")
	}
	if _, ok := orm.IdentityColumn(bigKeyed); !ok {
		t.Error("a sole bigint key was not treated as generated")
	}
}
