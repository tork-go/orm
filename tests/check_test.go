package orm_test

import (
	"testing"

	"github.com/tork-go/orm"
)

func TestNewCheckDef_Defaults(t *testing.T) {
	d := orm.NewCheckDef("age >= 0")

	if got := d.Name(); got != "" {
		t.Errorf("Name() = %q, want empty (auto-generate)", got)
	}
	if got, want := d.Expression(), "age >= 0"; got != want {
		t.Errorf("Expression() = %q, want %q", got, want)
	}
}

func TestCheckDef_Named(t *testing.T) {
	d := orm.NewCheckDef("age >= 0").Named("ck_accounts_age_non_negative")

	if got, want := d.Name(), "ck_accounts_age_non_negative"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got, want := d.Expression(), "age >= 0"; got != want {
		t.Errorf("Named() unexpectedly changed Expression() to %q, want %q", got, want)
	}
}

// TestCheckDef_ChainOrderIndependence mirrors TestIndexDef_ChainOrderIndependence.
func TestCheckDef_ChainOrderIndependence(t *testing.T) {
	forward := orm.NewCheckDef("age >= 0").Named("x")
	reversed := orm.NewCheckDef("age >= 0")
	reversed = reversed.Named("x")

	if forward.Name() != reversed.Name() || forward.Expression() != reversed.Expression() {
		t.Fatal("chain order affected Name/Expression")
	}
}

// TestCheckDef_EmptyExpression proves NewCheckDef("") is constructible
// without panicking. Validation (a check needs a non-empty expression) is
// schema.ExtractSchema's job, not the constructor's, mirroring how
// NewIndexDef() with zero columns doesn't panic either.
func TestCheckDef_EmptyExpression(t *testing.T) {
	d := orm.NewCheckDef("")
	if got := d.Expression(); got != "" {
		t.Errorf("Expression() = %q, want empty", got)
	}
}

// checkerModel proves orm.Checker is satisfiable by an ordinary model.
type checkerModel struct {
	orm.Table
	Age *orm.Column[int]
}

func (m *checkerModel) Checks() []orm.CheckDef {
	return []orm.CheckDef{orm.NewCheckDef("age >= 0")}
}

func TestCheckerModel_SatisfiesChecker(t *testing.T) {
	m := &checkerModel{Table: orm.NewTable("accounts"), Age: orm.NewColumn[int]("age")}

	var checker orm.Checker = m
	defs := checker.Checks()
	if len(defs) != 1 || defs[0].Expression() != "age >= 0" {
		t.Errorf("Checks() = %+v, want one definition with expression \"age >= 0\"", defs)
	}
}

func BenchmarkCheckDefChain(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = orm.NewCheckDef("age >= 0").Named("ck_accounts_age_non_negative")
	}
}
