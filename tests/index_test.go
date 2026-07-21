package orm_test

import (
	"testing"

	"github.com/tork-go/orm"
)

func TestNewIndexDef_Defaults(t *testing.T) {
	a := orm.NewColumn[int]("a")
	d := orm.NewIndexDef(a)

	if d.IsUnique() {
		t.Error("IsUnique() = true on a fresh IndexDef, want false")
	}
	if got := d.Name(); got != "" {
		t.Errorf("Name() = %q, want empty (auto-generate)", got)
	}
}

func TestIndexDef_Unique(t *testing.T) {
	a := orm.NewColumn[int]("a")
	d := orm.NewIndexDef(a).Unique()

	if !d.IsUnique() {
		t.Error("IsUnique() = false, want true")
	}
	if got := d.Name(); got != "" {
		t.Errorf("Unique() unexpectedly set Name() to %q", got)
	}
}

func TestIndexDef_Named(t *testing.T) {
	a := orm.NewColumn[int]("a")
	d := orm.NewIndexDef(a).Named("custom_name")

	if got, want := d.Name(), "custom_name"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if d.IsUnique() {
		t.Error("Named() unexpectedly set IsUnique()")
	}
}

func TestIndexDef_Columns_PreservesOrder(t *testing.T) {
	a := orm.NewColumn[int]("a")
	b := orm.NewColumn[int]("b")
	c := orm.NewColumn[int]("c")

	got := orm.NewIndexDef(c, a, b).Columns()
	if len(got) != 3 || got[0].Name() != "c" || got[1].Name() != "a" || got[2].Name() != "b" {
		names := make([]string, len(got))
		for i, col := range got {
			names[i] = col.Name()
		}
		t.Errorf("Columns() = %v, want [c a b]", names)
	}
}

// TestIndexDef_ChainOrderIndependence mirrors
// TestColumn_ChainOrderIndependence: the resulting metadata is the same
// regardless of the order Unique/Named are called in.
func TestIndexDef_ChainOrderIndependence(t *testing.T) {
	a := orm.NewColumn[int]("a")
	forward := orm.NewIndexDef(a).Unique().Named("x")
	reversed := orm.NewIndexDef(a).Named("x").Unique()

	if forward.IsUnique() != reversed.IsUnique() || forward.Name() != reversed.Name() {
		t.Fatal("chain order affected Unique/Named")
	}
}

// TestIndexDef_ZeroColumns proves NewIndexDef() is constructible without
// panicking. Validation (an index needs at least one column) is
// schema.ExtractSchema's job, not the constructor's, mirroring how
// Column[int]("n").MaxLen(-1) doesn't panic either.
func TestIndexDef_ZeroColumns(t *testing.T) {
	d := orm.NewIndexDef()
	if len(d.Columns()) != 0 {
		t.Errorf("Columns() = %v, want none", d.Columns())
	}
}

// indexerModel proves orm.Indexer is satisfiable by an ordinary model.
type indexerModel struct {
	orm.Table[orm.NoEntity]
	A *orm.Column[int]
	B *orm.Column[int]
}

func (m *indexerModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{orm.NewIndexDef(m.A, m.B)}
}

func TestIndexerModel_SatisfiesIndexer(t *testing.T) {
	m := &indexerModel{Table: orm.NewTable[orm.NoEntity]("t"), A: orm.NewColumn[int]("a"), B: orm.NewColumn[int]("b")}

	var indexer orm.Indexer = m
	defs := indexer.Indexes()
	if len(defs) != 1 || len(defs[0].Columns()) != 2 {
		t.Errorf("Indexes() = %+v, want one definition with 2 columns", defs)
	}
}

func BenchmarkIndexDefChain(b *testing.B) {
	a := orm.NewColumn[int]("a")
	c := orm.NewColumn[int]("c")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = orm.NewIndexDef(a, c).Unique().Named("uq_t_a_c")
	}
}
