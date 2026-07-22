package orm_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/tests/fakedriver"
)

// stubDriver is the smallest thing orm.Driver accepts, and records the Config
// it was opened with so a test can assert what Connect passed on.
type stubDriver struct {
	opened []orm.Config
	err    error
}

func (*stubDriver) QuoteIdent(name string) string { return `"` + name + `"` }
func (*stubDriver) Placeholder(n int) string      { return "$" + strconv.Itoa(n) }
func (*stubDriver) RenderLike(col, mark string, ci bool) string {
	return col + " LIKE " + mark
}
func (*stubDriver) SupportsReturning() bool { return true }
func (*stubDriver) MaxBindParams() int      { return 0 }
func (*stubDriver) RenderUpsertDoNothing([]string) (string, error) {
	return "ON CONFLICT DO NOTHING", nil
}
func (*stubDriver) RenderUpsertDoUpdate(_, _ []string) (string, error) {
	return "ON CONFLICT DO UPDATE", nil
}
func (*stubDriver) RenderLock(orm.LockMode, orm.LockWait) (string, error) {
	return "FOR UPDATE", nil
}
func (*stubDriver) RenderJSONHasKey(col, key string) (string, error) {
	return col + " ? " + key, nil
}
func (*stubDriver) RenderJSONContains(col, val string) (string, error) {
	return col + " @> " + val, nil
}
func (*stubDriver) RenderJSONKey(col, key string, op orm.Operator, val string) (string, error) {
	return col + " ->> " + key + " " + op.String() + " " + val, nil
}
func (*stubDriver) RenderArrayContains(col, mark string) (string, error) {
	return col + " @> " + mark, nil
}
func (*stubDriver) RenderArrayOverlaps(col, mark string) (string, error) {
	return col + " && " + mark, nil
}
func (*stubDriver) RenderArrayLength(col string, op orm.Operator, mark string) (string, error) {
	return "cardinality(" + col + ") " + op.String() + " " + mark, nil
}
func (*stubDriver) RenderFullText(col, mark string) (string, error) {
	return "to_tsvector(" + col + ") @@ websearch_to_tsquery(" + mark + ")", nil
}

func (d *stubDriver) Open(_ context.Context, cfg orm.Config) (orm.Conn, error) {
	d.opened = append(d.opened, cfg)
	if d.err != nil {
		return nil, d.err
	}
	return fakedriver.NewConn(), nil
}

// register adds d under a name unique to this test, since orm.Register
// deliberately refuses a duplicate and the registry outlives any one test.
func register(t *testing.T, d *stubDriver) string {
	t.Helper()
	name := "stub" + strings.ToLower(t.Name())
	orm.Register(d, name)
	return name
}

// The scheme is what names the driver, so a caller says which database they
// want once, in the connection string.
func TestConnect_ResolvesTheDriverFromTheScheme(t *testing.T) {
	d := &stubDriver{}
	name := register(t, d)

	db, err := orm.Connect(context.Background(), name+"://user:pw@localhost:5432/app")
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if db == nil {
		t.Fatal("Connect() returned no handle")
	}
	if len(d.opened) != 1 {
		t.Fatalf("the driver was opened %d times, want 1", len(d.opened))
	}
	// A whole connection string is passed through untouched: the driver
	// understands parts of it this package does not model.
	if got := d.opened[0].URL; got != name+"://user:pw@localhost:5432/app" {
		t.Errorf("driver received URL %q, want the caller's own", got)
	}
}

// The parts exist so a password out of configuration never has to be pasted
// into a string by hand.
func TestOpen_FromConfigParts(t *testing.T) {
	d := &stubDriver{}
	name := register(t, d)

	_, err := orm.Open(context.Background(), orm.Config{
		Driver:   name,
		Host:     "db.internal",
		Port:     6432,
		User:     "tork",
		Password: "s3cret",
		Database: "app",
		Options:  map[string]string{"sslmode": "require"},
		MaxConns: 20,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	got := d.opened[0]
	if got.Host != "db.internal" || got.Port != 6432 || got.MaxConns != 20 {
		t.Errorf("driver received %+v, want the parts as given", got)
	}
}

// BuildURL is what every driver uses to turn the parts into a string, so the
// escaping lives in one place rather than in each of them.
func TestBuildURL(t *testing.T) {
	tests := []struct {
		name string
		cfg  orm.Config
		want string
	}{
		{
			name: "every part",
			cfg: orm.Config{
				Host: "db.internal", Port: 6432,
				User: "tork", Password: "pw", Database: "app",
			},
			want: "postgres://tork:pw@db.internal:6432/app",
		},
		{
			name: "host and port default",
			cfg:  orm.Config{User: "tork", Database: "app"},
			want: "postgres://tork@localhost:5432/app",
		},
		{
			// The case a hand-built string gets wrong: punctuation in a
			// password is not a URL delimiter just because it looks like one.
			name: "password needing escapes",
			cfg: orm.Config{
				User: "tork", Password: "p@ss/w:rd?", Database: "app",
			},
			want: "postgres://tork:p%40ss%2Fw%3Ard%3F@localhost:5432/app",
		},
		{
			name: "options are sorted, so one Config is one string",
			cfg: orm.Config{
				User: "tork", Database: "app",
				Options: map[string]string{"sslmode": "require", "application_name": "api"},
			},
			want: "postgres://tork@localhost:5432/app?application_name=api&sslmode=require",
		},
		{
			name: "a URL is returned as given",
			cfg:  orm.Config{URL: "postgres://elsewhere/db?whatever=1", Host: "ignored"},
			want: "postgres://elsewhere/db?whatever=1",
		},
		{
			name: "no user",
			cfg:  orm.Config{Host: "h", Port: 1, Database: "d"},
			want: "postgres://h:1/d",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := orm.BuildURL(tt.cfg, "postgres", 5432); got != tt.want {
				t.Errorf("BuildURL() = %s\nwant           %s", got, tt.want)
			}
		})
	}
}

// The likeliest mistake is a missing blank import, so the error names the line
// to add rather than only the name that was not found.
func TestConnect_UnknownDriverNamesTheImport(t *testing.T) {
	orm.Register(&stubDriver{}, "stubforunknown")

	_, err := orm.Connect(context.Background(), "nosuchdb://localhost/app")
	if err == nil {
		t.Fatal("Connect() error = nil, want the unknown driver reported")
	}
	for _, want := range []string{
		`no driver named "nosuchdb"`,
		`_ "github.com/tork-go/orm/driver/nosuchdb"`,
		"stubforunknown", // the registered names, so a typo is visible
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestConnect_MalformedConnectionString(t *testing.T) {
	_, err := orm.Connect(context.Background(), "just-a-string")
	if err == nil {
		t.Fatal("Connect() error = nil, want the missing scheme reported")
	}
	if !strings.Contains(err.Error(), "starts with a scheme") {
		t.Errorf("error %q does not say what a connection string looks like", err)
	}
}

// An error gets logged, and a logged password is a leaked one.
func TestConnect_ErrorRedactsThePassword(t *testing.T) {
	// No scheme, so the string is quoted back at the caller to show what was
	// wrong with it. It must not be quoted back in full.
	_, err := orm.Connect(context.Background(), "//user:hunter2@host/db")
	if err == nil {
		t.Fatal("Connect() error = nil, want the missing scheme reported")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error %q leaks the password", err)
	}

	// Nothing to redact is left alone rather than mangled.
	for _, s := range []string{"//user@host/db", "//ho st:pw@bad", "plain"} {
		_, err := orm.Connect(context.Background(), s)
		if err == nil {
			t.Errorf("Connect(%q) error = nil, want the missing scheme reported", s)
		}
	}
}

func TestOpen_ConfigWithNeitherDriverNorURL(t *testing.T) {
	_, err := orm.Open(context.Background(), orm.Config{Host: "localhost"})
	if err == nil {
		t.Fatal("Open() error = nil, want the missing driver reported")
	}
	if !strings.Contains(err.Error(), "names no driver") {
		t.Errorf("error %q does not name the problem", err)
	}
}

// A driver that cannot connect reports why, with the database named.
func TestConnect_DriverFailure(t *testing.T) {
	sentinel := errors.New("no route to host")
	d := &stubDriver{err: sentinel}
	name := register(t, d)

	_, err := orm.Connect(context.Background(), name+"://localhost/app")
	if !errors.Is(err, sentinel) {
		t.Fatalf("Connect() error = %v, want the driver's own", err)
	}
	if !strings.Contains(err.Error(), name) {
		t.Errorf("error %q does not name the database", err)
	}
}

// Two drivers answering to one name would make which database a program
// talked to depend on import order.
func TestRegister_RejectsDuplicatesAndNonsense(t *testing.T) {
	tests := map[string]func(){
		"duplicate name": func() {
			orm.Register(&stubDriver{}, "stubduplicate")
			orm.Register(&stubDriver{}, "stubduplicate")
		},
		"nil driver":   func() { orm.Register(nil, "stubnil") },
		"empty name":   func() { orm.Register(&stubDriver{}, "") },
		"empty alias":  func() { orm.Register(&stubDriver{}, "stubalias", "") },
		"alias taken":  func() { orm.Register(&stubDriver{}, "stubtaken2", "stubtaken") },
		"alias itself": func() { orm.Register(&stubDriver{}, "stubself", "stubself") },
	}
	orm.Register(&stubDriver{}, "stubtaken")

	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic, want the registration refused")
				}
			}()
			run()
		})
	}
}

// An alias is a second spelling of one driver, not a second driver.
func TestRegister_Aliases(t *testing.T) {
	d := &stubDriver{}
	orm.Register(d, "stubprimary", "stubalt")

	first, ok := orm.LookupDriver("stubprimary")
	if !ok {
		t.Fatal("the primary name was not registered")
	}
	second, ok := orm.LookupDriver("stubalt")
	if !ok {
		t.Fatal("the alias was not registered")
	}
	if first != second {
		t.Error("the alias resolves to a different driver than the name")
	}
	if names := orm.Drivers(); !slices.IsSorted(names) {
		t.Errorf("Drivers() = %v, want them sorted", names)
	}
}

func TestLookupDriver_Unknown(t *testing.T) {
	if _, ok := orm.LookupDriver("stubnothingregisteredhere"); ok {
		t.Error("LookupDriver found a driver nobody registered")
	}
}

// Closing releases the pool. A handle bound to a transaction has nothing of
// its own to close, and closing the connection under a running transaction
// would abandon it.
func TestDB_Close(t *testing.T) {
	c := fakedriver.NewConn()
	db := orm.NewDB(c, fakedriver.NewDialect())

	if err := db.Close(context.Background()); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	err := db.Transaction(context.Background(), func(tx *orm.DB) error {
		return tx.Close(context.Background())
	})
	if err == nil {
		t.Fatal("closing a transaction handle succeeded, want it refused")
	}
	if !strings.Contains(err.Error(), "transaction") {
		t.Errorf("error %q does not explain why", err)
	}
}

// The pool settings reach the driver as given, since a driver applies only
// what the caller actually chose and leaves the rest to its own defaults.
func TestOpen_PoolSettings(t *testing.T) {
	d := &stubDriver{}
	name := register(t, d)

	_, err := orm.Open(context.Background(), orm.Config{
		Driver:          name,
		Database:        "app",
		MaxConns:        30,
		MinConns:        5,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: time.Minute,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	got := d.opened[0]
	if got.MaxConns != 30 || got.MinConns != 5 ||
		got.MaxConnLifetime != time.Hour || got.MaxConnIdleTime != time.Minute {
		t.Errorf("driver received %+v, want the pool settings as given", got)
	}
}
