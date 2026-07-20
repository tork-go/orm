package orm_test

import (
	"reflect"
	"testing"

	"github.com/tork-go/orm"
)

func TestColumn_FreshState(t *testing.T) {
	c := orm.NewColumn[int]("id")

	if got := c.Name(); got != "id" {
		t.Errorf("Name() = %q, want %q", got, "id")
	}
	if c.IsPrimaryKey() {
		t.Error("IsPrimaryKey() = true on a fresh column, want false")
	}
	if c.IsUnique() {
		t.Error("IsUnique() = true on a fresh column, want false")
	}
	if c.IsNotNull() {
		t.Error("IsNotNull() = true on a fresh column, want false")
	}
	if n, ok := c.MaxLength(); ok {
		t.Errorf("MaxLength() = (%d, %v), want ok=false on a fresh column", n, ok)
	}
}

func TestColumn_BuilderMethods(t *testing.T) {
	tests := []struct {
		name  string
		build func(c *orm.Column[string]) *orm.Column[string]
		check func(t *testing.T, c *orm.Column[string])
	}{
		{
			name:  "PrimaryKey sets only primary key",
			build: func(c *orm.Column[string]) *orm.Column[string] { return c.PrimaryKey() },
			check: func(t *testing.T, c *orm.Column[string]) {
				if !c.IsPrimaryKey() {
					t.Error("IsPrimaryKey() = false, want true")
				}
				if c.IsUnique() || c.IsNotNull() {
					t.Error("PrimaryKey() unexpectedly set Unique or NotNull")
				}
			},
		},
		{
			name:  "Unique sets only unique",
			build: func(c *orm.Column[string]) *orm.Column[string] { return c.Unique() },
			check: func(t *testing.T, c *orm.Column[string]) {
				if !c.IsUnique() {
					t.Error("IsUnique() = false, want true")
				}
				if c.IsPrimaryKey() || c.IsNotNull() {
					t.Error("Unique() unexpectedly set PrimaryKey or NotNull")
				}
			},
		},
		{
			name:  "NotNull sets only not null",
			build: func(c *orm.Column[string]) *orm.Column[string] { return c.NotNull() },
			check: func(t *testing.T, c *orm.Column[string]) {
				if !c.IsNotNull() {
					t.Error("IsNotNull() = false, want true")
				}
				if c.IsPrimaryKey() || c.IsUnique() {
					t.Error("NotNull() unexpectedly set PrimaryKey or Unique")
				}
			},
		},
		{
			name:  "MaxLen sets only max length",
			build: func(c *orm.Column[string]) *orm.Column[string] { return c.MaxLen(30) },
			check: func(t *testing.T, c *orm.Column[string]) {
				n, ok := c.MaxLength()
				if !ok || n != 30 {
					t.Errorf("MaxLength() = (%d, %v), want (30, true)", n, ok)
				}
				if c.IsPrimaryKey() || c.IsUnique() || c.IsNotNull() {
					t.Error("MaxLen() unexpectedly set another constraint")
				}
			},
		},
		{
			name: "all builders combined",
			build: func(c *orm.Column[string]) *orm.Column[string] {
				return c.PrimaryKey().Unique().NotNull().MaxLen(30)
			},
			check: func(t *testing.T, c *orm.Column[string]) {
				n, ok := c.MaxLength()
				if !c.IsPrimaryKey() || !c.IsUnique() || !c.IsNotNull() || !ok || n != 30 {
					t.Errorf("combined builders did not set all constraints: pk=%v unique=%v notnull=%v maxlen=(%d,%v)",
						c.IsPrimaryKey(), c.IsUnique(), c.IsNotNull(), n, ok)
				}
			},
		},
		{
			name: "calling a builder twice is idempotent",
			build: func(c *orm.Column[string]) *orm.Column[string] {
				return c.PrimaryKey().PrimaryKey()
			},
			check: func(t *testing.T, c *orm.Column[string]) {
				if !c.IsPrimaryKey() {
					t.Error("IsPrimaryKey() = false after calling PrimaryKey() twice, want true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.build(orm.NewColumn[string]("col"))
			tt.check(t, c)
		})
	}
}

// TestColumn_ChainOrderIndependence proves the resulting metadata is the
// same regardless of the order builder methods are called in, since they
// mutate independent fields.
func TestColumn_ChainOrderIndependence(t *testing.T) {
	forward := orm.NewColumn[string]("username").Unique().NotNull().MaxLen(30)
	reversed := orm.NewColumn[string]("username").MaxLen(30).NotNull().Unique()

	if forward.IsUnique() != reversed.IsUnique() ||
		forward.IsNotNull() != reversed.IsNotNull() {
		t.Fatal("chain order affected Unique/NotNull flags")
	}
	fn, fok := forward.MaxLength()
	rn, rok := reversed.MaxLength()
	if fn != rn || fok != rok {
		t.Fatalf("chain order affected MaxLength: forward=(%d,%v) reversed=(%d,%v)", fn, fok, rn, rok)
	}
}

func TestColumn_MaxLength_SetVsUnset(t *testing.T) {
	tests := []struct {
		name    string
		build   func() *orm.Column[string]
		wantN   int
		wantOK  bool
	}{
		{
			name:   "never called",
			build:  func() *orm.Column[string] { return orm.NewColumn[string]("c") },
			wantN:  0,
			wantOK: false,
		},
		{
			name:   "explicitly set to 0",
			build:  func() *orm.Column[string] { return orm.NewColumn[string]("c").MaxLen(0) },
			wantN:  0,
			wantOK: true,
		},
		{
			name:   "set to a positive value",
			build:  func() *orm.Column[string] { return orm.NewColumn[string]("c").MaxLen(255) },
			wantN:  255,
			wantOK: true,
		},
		{
			name:   "set to a negative value",
			build:  func() *orm.Column[string] { return orm.NewColumn[string]("c").MaxLen(-1) },
			wantN:  -1,
			wantOK: true,
		},
		{
			name:   "overwritten by a second call",
			build:  func() *orm.Column[string] { return orm.NewColumn[string]("c").MaxLen(10).MaxLen(20) },
			wantN:  20,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, ok := tt.build().MaxLength()
			if n != tt.wantN || ok != tt.wantOK {
				t.Errorf("MaxLength() = (%d, %v), want (%d, %v)", n, ok, tt.wantN, tt.wantOK)
			}
		})
	}
}

type namedStruct struct{ X int }

func TestColumn_GoTypeAndNullability(t *testing.T) {
	// Each case is its own subtest instantiating Column[T] for a distinct T,
	// since T must be chosen at compile time and cannot be parameterized
	// over a runtime table in Go.
	t.Run("int is not nullable", func(t *testing.T) {
		c := orm.NewColumn[int]("n")
		if c.GoType() != reflect.TypeFor[int]() {
			t.Errorf("GoType() = %v, want %v", c.GoType(), reflect.TypeFor[int]())
		}
		if c.IsNullable() {
			t.Error("IsNullable() = true for Column[int], want false")
		}
	})

	t.Run("string is not nullable", func(t *testing.T) {
		c := orm.NewColumn[string]("s")
		if c.GoType() != reflect.TypeFor[string]() {
			t.Errorf("GoType() = %v, want %v", c.GoType(), reflect.TypeFor[string]())
		}
		if c.IsNullable() {
			t.Error("IsNullable() = true for Column[string], want false")
		}
	})

	t.Run("bool is not nullable", func(t *testing.T) {
		c := orm.NewColumn[bool]("b")
		if c.IsNullable() {
			t.Error("IsNullable() = true for Column[bool], want false")
		}
	})

	t.Run("named struct is not nullable", func(t *testing.T) {
		c := orm.NewColumn[namedStruct]("st")
		if c.GoType() != reflect.TypeFor[namedStruct]() {
			t.Errorf("GoType() = %v, want %v", c.GoType(), reflect.TypeFor[namedStruct]())
		}
		if c.IsNullable() {
			t.Error("IsNullable() = true for Column[namedStruct], want false")
		}
	})

	t.Run("*string is nullable", func(t *testing.T) {
		c := orm.NewColumn[*string]("s")
		if c.GoType() != reflect.TypeFor[*string]() {
			t.Errorf("GoType() = %v, want %v", c.GoType(), reflect.TypeFor[*string]())
		}
		if !c.IsNullable() {
			t.Error("IsNullable() = false for Column[*string], want true")
		}
	})

	t.Run("*int is nullable", func(t *testing.T) {
		c := orm.NewColumn[*int]("n")
		if !c.IsNullable() {
			t.Error("IsNullable() = false for Column[*int], want true")
		}
	})

	t.Run("**int (double pointer) is nullable", func(t *testing.T) {
		c := orm.NewColumn[**int]("n")
		if !c.IsNullable() {
			t.Error("IsNullable() = false for Column[**int], want true")
		}
	})
}

func BenchmarkColumnBuilderChain(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = orm.NewColumn[string]("username").Unique().NotNull().MaxLen(30)
	}
}

func BenchmarkColumn_GoType(b *testing.B) {
	c := orm.NewColumn[string]("username")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = c.GoType()
	}
}

func BenchmarkColumn_IsNullable(b *testing.B) {
	c := orm.NewColumn[*string]("email")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = c.IsNullable()
	}
}
