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
	if c.IsIndexed() {
		t.Error("IsIndexed() = true on a fresh column, want false")
	}
	if expr, ok := c.ServerDefaultExpr(); ok {
		t.Errorf("ServerDefaultExpr() = (%q, %v), want ok=false on a fresh column", expr, ok)
	}
	if c.IsClientGenerated() {
		t.Error("IsClientGenerated() = true on a fresh column, want false")
	}
	if gen, ok := c.Generator(); ok || gen != nil {
		t.Errorf("Generator() ok=%v, isNil=%v, want ok=false, generator=nil on a fresh column", ok, gen == nil)
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
			name:  "Index sets only indexed",
			build: func(c *orm.Column[string]) *orm.Column[string] { return c.Index() },
			check: func(t *testing.T, c *orm.Column[string]) {
				if !c.IsIndexed() {
					t.Error("IsIndexed() = false, want true")
				}
				if c.IsPrimaryKey() || c.IsUnique() || c.IsNotNull() {
					t.Error("Index() unexpectedly set another constraint")
				}
			},
		},
		{
			name:  "ServerDefault sets only the server default",
			build: func(c *orm.Column[string]) *orm.Column[string] { return c.ServerDefault("now()") },
			check: func(t *testing.T, c *orm.Column[string]) {
				expr, ok := c.ServerDefaultExpr()
				if !ok || expr != "now()" {
					t.Errorf("ServerDefaultExpr() = (%q, %v), want (\"now()\", true)", expr, ok)
				}
				if c.IsPrimaryKey() || c.IsUnique() || c.IsNotNull() || c.IsIndexed() {
					t.Error("ServerDefault() unexpectedly set another constraint")
				}
			},
		},
		{
			name: "GeneratedByClient sets only the generator",
			build: func(c *orm.Column[string]) *orm.Column[string] {
				return c.GeneratedByClient(func() string { return "x" })
			},
			check: func(t *testing.T, c *orm.Column[string]) {
				if !c.IsClientGenerated() {
					t.Error("IsClientGenerated() = false, want true")
				}
				if c.IsPrimaryKey() || c.IsUnique() || c.IsNotNull() || c.IsIndexed() {
					t.Error("GeneratedByClient() unexpectedly set another constraint")
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
	forward := orm.NewColumn[string]("username").Unique().NotNull().MaxLen(30).Index().ServerDefault("x")
	reversed := orm.NewColumn[string]("username").ServerDefault("x").Index().MaxLen(30).NotNull().Unique()

	if forward.IsUnique() != reversed.IsUnique() ||
		forward.IsNotNull() != reversed.IsNotNull() ||
		forward.IsIndexed() != reversed.IsIndexed() {
		t.Fatal("chain order affected Unique/NotNull/Index flags")
	}
	fn, fok := forward.MaxLength()
	rn, rok := reversed.MaxLength()
	if fn != rn || fok != rok {
		t.Fatalf("chain order affected MaxLength: forward=(%d,%v) reversed=(%d,%v)", fn, fok, rn, rok)
	}
	fe, feok := forward.ServerDefaultExpr()
	re, reok := reversed.ServerDefaultExpr()
	if fe != re || feok != reok {
		t.Fatalf("chain order affected ServerDefaultExpr: forward=(%q,%v) reversed=(%q,%v)", fe, feok, re, reok)
	}
}

func TestColumn_MaxLength_SetVsUnset(t *testing.T) {
	tests := []struct {
		name   string
		build  func() *orm.Column[string]
		wantN  int
		wantOK bool
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

func TestColumn_ServerDefaultExpr_SetVsUnset(t *testing.T) {
	tests := []struct {
		name     string
		build    func() *orm.Column[string]
		wantExpr string
		wantOK   bool
	}{
		{
			name:     "never called",
			build:    func() *orm.Column[string] { return orm.NewColumn[string]("c") },
			wantExpr: "",
			wantOK:   false,
		},
		{
			name:     "explicitly set to empty string",
			build:    func() *orm.Column[string] { return orm.NewColumn[string]("c").ServerDefault("") },
			wantExpr: "",
			wantOK:   true,
		},
		{
			name:     "set to a non-empty expression",
			build:    func() *orm.Column[string] { return orm.NewColumn[string]("c").ServerDefault("now()") },
			wantExpr: "now()",
			wantOK:   true,
		},
		{
			name:     "overwritten by a second call",
			build:    func() *orm.Column[string] { return orm.NewColumn[string]("c").ServerDefault("a").ServerDefault("b") },
			wantExpr: "b",
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, ok := tt.build().ServerDefaultExpr()
			if expr != tt.wantExpr || ok != tt.wantOK {
				t.Errorf("ServerDefaultExpr() = (%q, %v), want (%q, %v)", expr, ok, tt.wantExpr, tt.wantOK)
			}
		})
	}
}

func TestColumn_GeneratedByClient_Generator(t *testing.T) {
	c := orm.NewColumn[int]("n").GeneratedByClient(func() int { return 7 })

	if !c.IsClientGenerated() {
		t.Error("IsClientGenerated() = false, want true")
	}
	fn, ok := c.Generator()
	if !ok {
		t.Fatal("Generator() ok = false, want true")
	}
	if got := fn(); got != 7 {
		t.Errorf("Generator()() = %d, want 7", got)
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
		_ = orm.NewColumn[string]("username").Unique().NotNull().MaxLen(30).Index().ServerDefault("x")
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
