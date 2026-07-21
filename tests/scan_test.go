package orm_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/tests/fakedriver"
)

type scanAudit struct {
	CreatedAt time.Time
}

// The row type mixes a direct field, one promoted from an embedded struct,
// a nullable field and a document field, so one scan exercises every path
// the mapping has to handle.
type scanRow struct {
	scanAudit
	ID    int
	Name  string
	Email *string
	Prefs prefs
}

type scanModel struct {
	orm.Table[scanRow]
	ID        *orm.IntColumn
	Name      *orm.StringColumn
	Email     *orm.NullableStringColumn
	Prefs     *orm.JSONColumn[prefs]
	CreatedAt *orm.TimeColumn
}

var scanTable = orm.DefineTable[scanRow]("scan_rows", func(t *orm.TableBuilder[scanRow]) *scanModel {
	return &scanModel{
		Table:     t.Table(),
		ID:        t.Int("id"),
		Name:      t.String("name"),
		Email:     t.NullableString("email"),
		Prefs:     orm.NewJSONColumn[prefs]("prefs"),
		CreatedAt: t.Time("created_at"),
	}
})

// Column order is what ties a scanned value back to its field, since the
// driver reports no column names.
func TestTable_ColumnsAreInDeclarationOrder(t *testing.T) {
	want := []string{"id", "name", "email", "prefs", "created_at"}
	got := scanTable.Columns()
	if len(got) != len(want) {
		t.Fatalf("Columns() returned %d columns, want %d", len(got), len(want))
	}
	for i, c := range got {
		if c.Name() != want[i] {
			t.Errorf("Columns()[%d] = %q, want %q", i, c.Name(), want[i])
		}
	}
}

// This is the first thing to actually use the index paths DefineTable
// resolves, and the embedded CreatedAt is the case a single level path
// would get wrong.
func TestScanRow_FillsEveryFieldShape(t *testing.T) {
	c := fakedriver.NewConn()
	email := "alice@example.com"
	created := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	c.QueueRows([]any{1, "alice", &email, []byte(`{"theme":"dark"}`), created})

	rows, err := c.Query(context.Background(), "SELECT ...")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("Next() = false, want the queued row")
	}

	got, err := scanTable.ScanRow(rows)
	if err != nil {
		t.Fatalf("ScanRow() error = %v", err)
	}

	if got.ID != 1 {
		t.Errorf("ID = %d, want 1", got.ID)
	}
	if got.Name != "alice" {
		t.Errorf("Name = %q, want alice", got.Name)
	}
	if got.Email == nil || *got.Email != email {
		t.Errorf("Email = %v, want %q", got.Email, email)
	}
	if got.Prefs.Theme != "dark" {
		t.Errorf("Prefs.Theme = %q, want dark: the document column did not decode", got.Prefs.Theme)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v: the embedded field was not filled", got.CreatedAt, created)
	}
}

func TestScanRow_NullsLeaveZeroValues(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{2, "bob", nil, nil, time.Time{}})

	rows, _ := c.Query(context.Background(), "SELECT ...")
	defer rows.Close()
	rows.Next()

	got, err := scanTable.ScanRow(rows)
	if err != nil {
		t.Fatalf("ScanRow() error = %v", err)
	}
	if got.Email != nil {
		t.Errorf("Email = %v, want nil for a NULL", got.Email)
	}
	if got.Prefs.Theme != "" {
		t.Errorf("Prefs = %+v, want the zero value for a NULL document", got.Prefs)
	}
}

// A custom codec has to apply on the way in as well as on the way out,
// which only holds if scanning goes through the column rather than
// straight to encoding/json.
func TestScanRow_UsesTheColumnsCodec(t *testing.T) {
	type row struct {
		ID    int
		Prefs prefs
	}
	type model struct {
		orm.Table[row]
		ID    *orm.IntColumn
		Prefs *orm.JSONColumn[prefs]
	}
	unmarshal := func(b []byte) (prefs, error) {
		return prefs{Theme: strings.TrimPrefix(string(b), "custom:")}, nil
	}
	tbl := orm.DefineTable[row]("custom_codec", func(t *orm.TableBuilder[row]) *model {
		return &model{
			Table: t.Table(),
			ID:    t.Int("id"),
			Prefs: orm.NewJSONColumn[prefs]("prefs").Serialize(
				func(p prefs) ([]byte, error) { return []byte("custom:" + p.Theme), nil },
				unmarshal,
			),
		}
	})

	c := fakedriver.NewConn()
	c.QueueRows([]any{1, []byte("custom:solarized")})
	rows, _ := c.Query(context.Background(), "SELECT ...")
	defer rows.Close()
	rows.Next()

	got, err := tbl.ScanRow(rows)
	if err != nil {
		t.Fatalf("ScanRow() error = %v", err)
	}
	if got.Prefs.Theme != "solarized" {
		t.Errorf("Prefs.Theme = %q, want solarized", got.Prefs.Theme)
	}
}

// A model built with NewTable has no entity mapping, so scanning has
// nothing to fill in and should say so rather than panic.
func TestScanRow_WithoutEntityMapping(t *testing.T) {
	type model struct {
		orm.Table[orm.NoEntity]
		ID *orm.IntColumn
	}
	m := &model{Table: orm.NewTable[orm.NoEntity]("legacy"), ID: orm.NewIntColumn("id")}

	c := fakedriver.NewConn()
	c.QueueRows([]any{1})
	rows, _ := c.Query(context.Background(), "SELECT ...")
	defer rows.Close()
	rows.Next()

	_, err := m.Table.ScanRow(rows)
	if err == nil {
		t.Fatal("ScanRow() error = nil, want a missing mapping error")
	}
	if !strings.Contains(err.Error(), "DefineTable") {
		t.Errorf("error %q does not point at DefineTable", err)
	}
}

func TestScanRow_ScanFailureIsNamed(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{"not an int", "bob", nil, nil, time.Time{}})

	rows, _ := c.Query(context.Background(), "SELECT ...")
	defer rows.Close()
	rows.Next()

	_, err := scanTable.ScanRow(rows)
	if err == nil {
		t.Fatal("ScanRow() error = nil, want a scan failure")
	}
	if !strings.Contains(err.Error(), "scan_rows") {
		t.Errorf("error %q does not name the table", err)
	}
}

func TestScanRow_DecodeFailureIsNamed(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "alice", nil, []byte("not json"), time.Time{}})

	rows, _ := c.Query(context.Background(), "SELECT ...")
	defer rows.Close()
	rows.Next()

	_, err := scanTable.ScanRow(rows)
	if err == nil {
		t.Fatal("ScanRow() error = nil, want a decode failure")
	}
	if !strings.Contains(err.Error(), `column "prefs"`) {
		t.Errorf("error %q does not name the column that failed to decode", err)
	}
}

func TestColumns_OnZeroTable(t *testing.T) {
	var tbl orm.Table[scanRow]
	if got := tbl.Columns(); got != nil {
		t.Errorf("Columns() = %v, want nil on a zero table", got)
	}
}
