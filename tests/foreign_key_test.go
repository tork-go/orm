package orm_test

import (
	"reflect"
	"testing"

	"github.com/tork-go/orm"
)

// TestNewForeignKey_TypeInference proves T is inferred from the refColumn
// argument with no explicit type argument at the call site, exactly as
// used in the target model-declaration API:
//
//	AuthorID := NewForeignKey("author_id", User.TableName(), User.ID)
func TestNewForeignKey_TypeInference(t *testing.T) {
	id := orm.NewColumn[int]("id")

	// No explicit [int] type argument below; assigning to a *ForeignKey[int]
	// variable would fail to compile if inference didn't work.
	var fk *orm.ForeignKey[int] = orm.NewForeignKey("author_id", "users", id)

	if got, want := fk.Name(), "author_id"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestForeignKey_ReferencedTableAndColumn(t *testing.T) {
	id := orm.NewColumn[int]("id")
	fk := orm.NewForeignKey("author_id", "users", id)

	if got, want := fk.ReferencedTable(), "users"; got != want {
		t.Errorf("ReferencedTable() = %q, want %q", got, want)
	}
	if got, want := fk.ReferencedColumn(), "id"; got != want {
		t.Errorf("ReferencedColumn() = %q, want %q", got, want)
	}
	if got, want := fk.Name(), "author_id"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

// TestForeignKey_FreshState proves a fresh foreign key starts with no
// constraints set, mirroring Column's fresh state.
func TestForeignKey_FreshState(t *testing.T) {
	fk := orm.NewForeignKey("author_id", "users", orm.NewColumn[int]("id"))

	if fk.IsPrimaryKey() || fk.IsUnique() || fk.IsNotNull() {
		t.Error("fresh ForeignKey has a constraint set, want none")
	}
	if _, ok := fk.MaxLength(); ok {
		t.Error("fresh ForeignKey has MaxLength set, want unset")
	}
}

func TestForeignKey_BuilderMethods(t *testing.T) {
	tests := []struct {
		name  string
		build func(fk *orm.ForeignKey[int]) *orm.ForeignKey[int]
		check func(t *testing.T, fk *orm.ForeignKey[int])
	}{
		{
			name:  "PrimaryKey",
			build: func(fk *orm.ForeignKey[int]) *orm.ForeignKey[int] { return fk.PrimaryKey() },
			check: func(t *testing.T, fk *orm.ForeignKey[int]) {
				if !fk.IsPrimaryKey() {
					t.Error("IsPrimaryKey() = false, want true")
				}
			},
		},
		{
			name:  "Unique",
			build: func(fk *orm.ForeignKey[int]) *orm.ForeignKey[int] { return fk.Unique() },
			check: func(t *testing.T, fk *orm.ForeignKey[int]) {
				if !fk.IsUnique() {
					t.Error("IsUnique() = false, want true")
				}
			},
		},
		{
			name:  "NotNull",
			build: func(fk *orm.ForeignKey[int]) *orm.ForeignKey[int] { return fk.NotNull() },
			check: func(t *testing.T, fk *orm.ForeignKey[int]) {
				if !fk.IsNotNull() {
					t.Error("IsNotNull() = false, want true")
				}
			},
		},
		{
			name:  "MaxLen",
			build: func(fk *orm.ForeignKey[int]) *orm.ForeignKey[int] { return fk.MaxLen(10) },
			check: func(t *testing.T, fk *orm.ForeignKey[int]) {
				n, ok := fk.MaxLength()
				if !ok || n != 10 {
					t.Errorf("MaxLength() = (%d, %v), want (10, true)", n, ok)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fk := tt.build(orm.NewForeignKey("author_id", "users", orm.NewColumn[int]("id")))
			tt.check(t, fk)
		})
	}
}

// TestForeignKey_ChainedBuilders proves all four builder overrides return
// *ForeignKey[T] (not the embedded *Column[T]) so calls chain together and
// ReferencedTable/ReferencedColumn remain callable afterward. This is the
// behavior that makes the overrides necessary in the first place.
func TestForeignKey_ChainedBuilders(t *testing.T) {
	fk := orm.NewForeignKey("author_id", "users", orm.NewColumn[int]("id")).
		NotNull().Unique().PrimaryKey().MaxLen(10)

	if !fk.IsNotNull() || !fk.IsUnique() || !fk.IsPrimaryKey() {
		t.Error("chained builders did not set all constraints")
	}
	n, ok := fk.MaxLength()
	if !ok || n != 10 {
		t.Errorf("MaxLength() = (%d, %v), want (10, true)", n, ok)
	}
	if got, want := fk.ReferencedTable(), "users"; got != want {
		t.Errorf("ReferencedTable() = %q, want %q after chaining", got, want)
	}
	if got, want := fk.ReferencedColumn(), "id"; got != want {
		t.Errorf("ReferencedColumn() = %q, want %q after chaining", got, want)
	}
}

// TestForeignKey_PromotedReadAccessors proves read-only accessors that
// don't need a covariant override (they don't return Self) still work via
// plain Go method promotion from the embedded Column[T].
func TestForeignKey_PromotedReadAccessors(t *testing.T) {
	fk := orm.NewForeignKey("author_id", "users", orm.NewColumn[int]("id"))

	if fk.GoType() != reflect.TypeFor[int]() {
		t.Errorf("GoType() = %v, want %v", fk.GoType(), reflect.TypeFor[int]())
	}
	if fk.IsNullable() {
		t.Error("IsNullable() = true for ForeignKey[int], want false")
	}
}

func TestForeignKey_InferenceAcrossTypes(t *testing.T) {
	t.Run("string referenced column", func(t *testing.T) {
		fk := orm.NewForeignKey("slug", "categories", orm.NewColumn[string]("slug"))
		if fk.GoType() != reflect.TypeFor[string]() {
			t.Errorf("GoType() = %v, want string", fk.GoType())
		}
	})

	t.Run("nullable (pointer) referenced column", func(t *testing.T) {
		fk := orm.NewForeignKey("parent_id", "categories", orm.NewColumn[*int]("id"))
		if !fk.IsNullable() {
			t.Error("IsNullable() = false for ForeignKey[*int], want true")
		}
	})
}

func BenchmarkForeignKeyBuilderChain(b *testing.B) {
	id := orm.NewColumn[int]("id")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = orm.NewForeignKey("author_id", "users", id).NotNull().Unique()
	}
}
