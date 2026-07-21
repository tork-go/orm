package query_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver/postgres"
	"github.com/tork-go/orm/tests/fakedriver"
)

// slugify is the shape the whole hook feature exists for: a value derived
// from another before the row is written.
func slugify(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), " ", "-"))
}

type Post struct {
	ID    int
	Title string
	Slug  string

	// fired records which hooks ran, in order, so a test can assert both
	// that a hook ran and that it ran on the right side of the statement.
	fired []string
}

type PostModel struct {
	orm.Table[Post]
	ID    *orm.IntColumn
	Title *orm.StringColumn
	Slug  *orm.StringColumn
}

var Posts = orm.DefineTable[Post]("posts", func(t *orm.TableBuilder[Post]) *PostModel {
	return &PostModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Title: t.String("title").NotNull(),
		Slug:  t.String("slug").NotNull(),
	}
})

func (p *Post) BeforeCreate(context.Context) error {
	p.Slug = slugify(p.Title)
	p.fired = append(p.fired, "BeforeCreate")
	return nil
}
func (p *Post) AfterCreate(context.Context) error {
	p.fired = append(p.fired, "AfterCreate")
	return nil
}
func (p *Post) BeforeUpdate(context.Context) error {
	p.Slug = slugify(p.Title)
	p.fired = append(p.fired, "BeforeUpdate")
	return nil
}
func (p *Post) AfterUpdate(context.Context) error {
	p.fired = append(p.fired, "AfterUpdate")
	return nil
}
func (p *Post) BeforeDelete(context.Context) error {
	p.fired = append(p.fired, "BeforeDelete")
	return nil
}
func (p *Post) AfterDelete(context.Context) error {
	p.fired = append(p.fired, "AfterDelete")
	return nil
}
func (p *Post) AfterLoad(context.Context) error {
	p.fired = append(p.fired, "AfterLoad")
	return nil
}

// The request the whole query API was built for: a slug derived from a
// title, computed before the insert and written as part of it.
func TestBeforeCreate_DerivesAValueThatIsWritten(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1})
	db := orm.NewDB(c, postgres.Dialect{})

	p := &Post{Title: "Hello There World"}
	if err := Posts.With(db).Insert(context.Background(), p); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if p.Slug != "hello-there-world" {
		t.Fatalf("Slug = %q, want the derived slug", p.Slug)
	}
	// The point is not only that the field changed, but that the change
	// reached the statement: the hook has to run before the values are
	// bound, not after.
	args := c.QueryArgs(0)
	if len(args) != 2 || args[1] != "hello-there-world" {
		t.Errorf("Insert bound %v, want the derived slug", args)
	}
}

func TestHooks_FireAroundEachOperation(t *testing.T) {
	tests := []struct {
		name string
		run  func(*orm.DB, *Post) error
		want []string
	}{
		{"insert", func(db *orm.DB, p *Post) error {
			return Posts.With(db).Insert(context.Background(), p)
		}, []string{"BeforeCreate", "AfterCreate"}},
		{"update", func(db *orm.DB, p *Post) error {
			return Posts.With(db).Update(context.Background(), p)
		}, []string{"BeforeUpdate", "AfterUpdate"}},
		{"delete", func(db *orm.DB, p *Post) error {
			return Posts.With(db).Delete(context.Background(), p)
		}, []string{"BeforeDelete", "AfterDelete"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fakedriver.NewConn()
			c.RowsAffected = 1
			c.QueueRows([]any{1})
			db := orm.NewDB(c, postgres.Dialect{})

			p := &Post{ID: 1, Title: "x"}
			if err := tt.run(db, p); err != nil {
				t.Fatalf("%s error = %v", tt.name, err)
			}
			if strings.Join(p.fired, ",") != strings.Join(tt.want, ",") {
				t.Errorf("fired %v, want %v", p.fired, tt.want)
			}
		})
	}
}

// Save dispatches to one operation and fires that operation's pair, rather
// than telling the row two things happened.
func TestSave_FiresOnlyTheOperationItRan(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{5})
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	p := &Post{Title: "x"}
	if err := Posts.With(db).Save(context.Background(), p); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if strings.Join(p.fired, ",") != "BeforeCreate,AfterCreate" {
		t.Errorf("a new row fired %v, want the create pair only", p.fired)
	}

	p.fired = nil
	if err := Posts.With(db).Save(context.Background(), p); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if strings.Join(p.fired, ",") != "BeforeUpdate,AfterUpdate" {
		t.Errorf("a stored row fired %v, want the update pair only", p.fired)
	}
}

func TestAfterLoad_FiresPerRow(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "a", "a"}, []any{2, "b", "b"})
	db := orm.NewDB(c, postgres.Dialect{})

	posts, err := Posts.With(db).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("All() returned %d rows, want 2", len(posts))
	}
	for i, p := range posts {
		if strings.Join(p.fired, ",") != "AfterLoad" {
			t.Errorf("row %d fired %v, want AfterLoad once", i, p.fired)
		}
	}
}

var errRefused = errors.New("the hook refused")

type guarded struct {
	ID   int
	Name string

	refuseBefore bool
	refuseAfter  bool
}

type guardedModel struct {
	orm.Table[guarded]
	ID   *orm.IntColumn
	Name *orm.StringColumn
}

var Guarded = orm.DefineTable[guarded]("guarded", func(t *orm.TableBuilder[guarded]) *guardedModel {
	return &guardedModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
	}
})

func (g *guarded) BeforeCreate(context.Context) error {
	if g.refuseBefore {
		return errRefused
	}
	return nil
}
func (g *guarded) AfterCreate(context.Context) error {
	if g.refuseAfter {
		return errRefused
	}
	return nil
}
func (g *guarded) AfterLoad(context.Context) error {
	if g.refuseAfter {
		return errRefused
	}
	return nil
}

// A Before hook that refuses stops the operation before any SQL runs,
// which is what makes it usable for validation.
func TestBeforeHook_AbortsBeforeAnySQL(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, postgres.Dialect{})

	err := Guarded.With(db).Insert(context.Background(), &guarded{Name: "x", refuseBefore: true})
	if !errors.Is(err, errRefused) {
		t.Fatalf("Insert() error = %v, want the hook's error", err)
	}
	if !strings.Contains(err.Error(), "BeforeCreate") {
		t.Errorf("error %q does not name the hook it came from", err)
	}
	if len(c.ExecCalls()) != 0 || len(c.QueryCalls()) != 0 {
		t.Error("a refused Before hook still ran a statement")
	}
}

// An After hook runs once the row is written, so its error reaches the
// caller but the write has already happened.
func TestAfterHook_ErrorReachesTheCaller(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1})
	db := orm.NewDB(c, postgres.Dialect{})

	err := Guarded.With(db).Insert(context.Background(), &guarded{Name: "x", refuseAfter: true})
	if !errors.Is(err, errRefused) {
		t.Fatalf("Insert() error = %v, want the hook's error", err)
	}
	if !strings.Contains(err.Error(), "AfterCreate") {
		t.Errorf("error %q does not name the hook", err)
	}
	if len(c.QueryCalls()) != 1 {
		t.Error("the insert did not run, but an After hook should follow one that did")
	}
}

// A row type with no hooks is not asked for any, which is the ordinary
// case and must cost nothing.
func TestHooks_OptionalPerRowType(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "alice", nil, 30, nil, time.Time{}})
	db := orm.NewDB(c, postgres.Dialect{})

	if _, err := Users.With(db).All(context.Background()); err != nil {
		t.Errorf("All() on a row type with no hooks error = %v", err)
	}
}

// A hook on a value receiver still satisfies the interface through a
// pointer, so it would run against a copy and quietly discard whatever it
// changed. That is exactly the slug case failing silently, so it is
// rejected where it is written.
func TestDefineTable_ValueReceiverHook_Panics(t *testing.T) {
	got := mustPanicQuery(t, func() {
		orm.DefineTable[valueHookRow]("value_hook", func(b *orm.TableBuilder[valueHookRow]) *valueHookModel {
			return &valueHookModel{
				Table: b.Table(),
				ID:    b.Int("id").PrimaryKey(),
				Slug:  b.String("slug").NotNull(),
			}
		})
	})
	for _, want := range []string{"BeforeCreate", "pointer receiver", "discarded"} {
		if !strings.Contains(got, want) {
			t.Errorf("panic message %q does not mention %q", got, want)
		}
	}
}

type valueHookRow struct {
	ID   int
	Slug string
}

// Deliberately a value receiver: the mistake under test.
func (v valueHookRow) BeforeCreate(context.Context) error { return nil }

type valueHookModel struct {
	orm.Table[valueHookRow]
	ID   *orm.IntColumn
	Slug *orm.StringColumn
}

func mustPanicQuery(t *testing.T, fn func()) string {
	t.Helper()
	var got string
	func() {
		defer func() {
			if r := recover(); r != nil {
				got, _ = r.(string)
			}
		}()
		fn()
		t.Fatal("did not panic, want a panic")
	}()
	return got
}

// refusing is a row type whose hooks all refuse, so each After path can be
// reached. It is separate from guarded because AfterLoad has to refuse for
// a row that was scanned rather than one the test constructed.
type refusing struct {
	ID   int
	Name string
}

type refusingModel struct {
	orm.Table[refusing]
	ID   *orm.IntColumn
	Name *orm.StringColumn
}

var Refusing = orm.DefineTable[refusing]("refusing", func(t *orm.TableBuilder[refusing]) *refusingModel {
	return &refusingModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
	}
})

func (r *refusing) AfterLoad(context.Context) error   { return errRefused }
func (r *refusing) AfterUpdate(context.Context) error { return errRefused }
func (r *refusing) AfterDelete(context.Context) error { return errRefused }
func (r *refusing) BeforeUpdate(context.Context) error {
	if r.Name == "refuse" {
		return errRefused
	}
	return nil
}
func (r *refusing) BeforeDelete(context.Context) error {
	if r.Name == "refuse" {
		return errRefused
	}
	return nil
}

// A row that refuses on load stops the read, rather than being handed back
// as though it had loaded cleanly.
func TestAfterLoad_ErrorStopsTheRead(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "x"})
	db := orm.NewDB(c, postgres.Dialect{})

	_, err := Refusing.With(db).All(context.Background())
	if !errors.Is(err, errRefused) {
		t.Fatalf("All() error = %v, want the hook's error", err)
	}
	if !strings.Contains(err.Error(), "AfterLoad") {
		t.Errorf("error %q does not name the hook", err)
	}
}

func TestAfterHooks_OnUpdateAndDelete(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	err := Refusing.With(db).Update(context.Background(), &refusing{ID: 1, Name: "x"})
	if !errors.Is(err, errRefused) || !strings.Contains(err.Error(), "AfterUpdate") {
		t.Errorf("Update() error = %v, want the AfterUpdate refusal", err)
	}

	err = Refusing.With(db).Delete(context.Background(), &refusing{ID: 1, Name: "x"})
	if !errors.Is(err, errRefused) || !strings.Contains(err.Error(), "AfterDelete") {
		t.Errorf("Delete() error = %v, want the AfterDelete refusal", err)
	}
}

func TestBeforeHooks_AbortUpdateAndDelete(t *testing.T) {
	c := fakedriver.NewConn()
	c.RowsAffected = 1
	db := orm.NewDB(c, postgres.Dialect{})

	err := Refusing.With(db).Update(context.Background(), &refusing{ID: 1, Name: "refuse"})
	if !errors.Is(err, errRefused) || !strings.Contains(err.Error(), "BeforeUpdate") {
		t.Errorf("Update() error = %v, want the BeforeUpdate refusal", err)
	}
	err = Refusing.With(db).Delete(context.Background(), &refusing{ID: 1, Name: "refuse"})
	if !errors.Is(err, errRefused) || !strings.Contains(err.Error(), "BeforeDelete") {
		t.Errorf("Delete() error = %v, want the BeforeDelete refusal", err)
	}
	if len(c.ExecCalls()) != 0 {
		t.Error("a refused Before hook still ran a statement")
	}
}

// mutating is a row type whose AfterLoad changes the row, so a test can
// see that each row's hook ran against its own row rather than a shared or
// reused one.
type mutating struct {
	ID   int
	Name string
}

type mutatingModel struct {
	orm.Table[mutating]
	ID   *orm.IntColumn
	Name *orm.StringColumn
}

var Mutating = orm.DefineTable[mutating]("mutating", func(t *orm.TableBuilder[mutating]) *mutatingModel {
	return &mutatingModel{
		Table: t.Table(),
		ID:    t.Int("id").PrimaryKey(),
		Name:  t.String("name").NotNull(),
	}
})

func (m *mutating) AfterLoad(context.Context) error {
	m.Name = "seen:" + m.Name
	return nil
}

// Each row's hook runs against that row. Rows are allocated individually
// for exactly this reason, so a mutation in one cannot appear in another.
func TestAfterLoad_MutatesEachRowIndependently(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "a"}, []any{2, "b"}, []any{3, "c"})
	db := orm.NewDB(c, postgres.Dialect{})

	rows, err := Mutating.With(db).All(context.Background())
	if err != nil {
		t.Fatalf("All() error = %v", err)
	}
	want := []string{"seen:a", "seen:b", "seen:c"}
	if len(rows) != len(want) {
		t.Fatalf("All() returned %d rows, want %d", len(rows), len(want))
	}
	for i, r := range rows {
		if r.Name != want[i] {
			t.Errorf("row %d name = %q, want %q", i, r.Name, want[i])
		}
		if r.ID != i+1 {
			t.Errorf("row %d id = %d, want %d", i, r.ID, i+1)
		}
	}
}

// A hook that refuses partway through a result set stops the read, rather
// than handing back the rows that happened to load before it.
func TestAfterLoad_RefusalDiscardsThePartialResult(t *testing.T) {
	c := fakedriver.NewConn()
	c.QueueRows([]any{1, "a"}, []any{2, "b"})
	db := orm.NewDB(c, postgres.Dialect{})

	rows, err := Refusing.With(db).All(context.Background())
	if err == nil {
		t.Fatal("All() error = nil, want the hook's refusal")
	}
	if rows != nil {
		t.Errorf("All() returned %d rows alongside the error, want none", len(rows))
	}
}
