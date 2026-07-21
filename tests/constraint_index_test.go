package orm_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/schema"
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

// A partial index covers only rows matching its predicate, and an
// expression index has keys that are not columns at all.
func TestIndexDef_WhereAndExpressions(t *testing.T) {
	a := orm.NewIntColumn("tenant")

	partial := orm.NewIndexDef(a).Where("deleted_at IS NULL")
	if got := partial.WherePredicate(); got != "deleted_at IS NULL" {
		t.Errorf("WherePredicate() = %q, want the predicate", got)
	}
	if len(partial.Columns()) != 1 {
		t.Errorf("Columns() = %v, want one", partial.Columns())
	}

	expr := orm.NewIndexDef().On("lower(email)").Named("ix_lower_email")
	if got := expr.Expressions(); len(got) != 1 || got[0] != "lower(email)" {
		t.Errorf("Expressions() = %v, want [lower(email)]", got)
	}
	if len(expr.Columns()) != 0 {
		t.Errorf("Columns() = %v, want none on an expression index", expr.Columns())
	}
}

func TestIndexDef_ChainOrderIndependence_WhereAndOn(t *testing.T) {
	a := orm.NewIntColumn("tenant")
	x := orm.NewIndexDef(a).Where("active").Named("ix_a")
	y := orm.NewIndexDef(a).Named("ix_a").Where("active")
	if x.WherePredicate() != y.WherePredicate() || x.Name() != y.Name() {
		t.Error("the builder is order dependent")
	}
}

// An index is over columns or over expressions, since nothing records
// where each key sat.
func TestExtractSchema_MixedIndexKeys(t *testing.T) {
	m := &mixedIndexModel{
		Table: orm.NewTable[orm.NoEntity]("mixed_idx"),
		A:     orm.NewIntColumn("a"),
	}
	_, err := schema.ExtractSchema(m)
	if err == nil {
		t.Fatal("ExtractSchema() error = nil, want a mixed keys error")
	}
	if !strings.Contains(err.Error(), "mixes column and expression keys") {
		t.Errorf("error %q does not report the mix", err)
	}
}

type mixedIndexModel struct {
	orm.Table[orm.NoEntity]
	A *orm.IntColumn
}

func (m *mixedIndexModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{orm.NewIndexDef(m.A).On("lower(b)")}
}

// An expression index has no column list to derive a name from.
func TestExtractSchema_UnnamedExpressionIndex(t *testing.T) {
	m := &unnamedExprModel{
		Table: orm.NewTable[orm.NoEntity]("unnamed_expr"),
		A:     orm.NewIntColumn("a"),
	}
	_, err := schema.ExtractSchema(m)
	if err == nil {
		t.Fatal("ExtractSchema() error = nil, want a missing name error")
	}
	if !strings.Contains(err.Error(), "Named") {
		t.Errorf("error %q does not point at Named", err)
	}
}

type unnamedExprModel struct {
	orm.Table[orm.NoEntity]
	A *orm.IntColumn
}

func (m *unnamedExprModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{orm.NewIndexDef().On("lower(a)")}
}

// A unique constraint has neither expression keys nor a predicate.
func TestExtractSchema_UniqueWithPredicate(t *testing.T) {
	m := &uniquePartialModel{
		Table: orm.NewTable[orm.NoEntity]("unique_partial"),
		A:     orm.NewIntColumn("a"),
	}
	_, err := schema.ExtractSchema(m)
	if err == nil {
		t.Fatal("ExtractSchema() error = nil, want a unique-with-predicate error")
	}
	if !strings.Contains(err.Error(), "plain index") {
		t.Errorf("error %q does not suggest a plain index", err)
	}
}

type uniquePartialModel struct {
	orm.Table[orm.NoEntity]
	A *orm.IntColumn
}

func (m *uniquePartialModel) Indexes() []orm.IndexDef {
	return []orm.IndexDef{orm.NewIndexDef(m.A).Unique().Where("active")}
}
