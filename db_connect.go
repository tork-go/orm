package orm

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Connecting to a database goes through this package rather than through a
// driver, so a caller names the database they want in data rather than in
// types:
//
//	import (
//	    "github.com/tork-go/orm"
//	    _ "github.com/tork-go/orm/driver/postgres"
//	)
//
//	db, err := orm.Connect(ctx, "postgres://tork:tork@localhost:5432/app")
//
// The blank import is what makes "postgres" a name this package knows. Go
// cannot load a package by name at run time, and this package must not import
// a driver itself: doing so would link every driver's database client into
// every program, whichever database it actually talks to. So a driver
// registers itself when it is linked in, which is the same arrangement
// database/sql uses and the same one that keeps `go list -deps
// github.com/tork-go/orm` free of pgx.
//
// Forgetting the blank import is the one mistake this design allows, so the
// error names the exact line to add.

// Driver is what a driver package registers: how to write this database's SQL,
// and how to open a connection to it.
//
// It is deliberately smaller than the Dialect a migration needs. Connecting
// and querying are all an application does, and a driver that could do only
// that would still be worth having.
type Driver interface {
	QueryDialect

	// Open connects using cfg. A driver reads the fields it understands and
	// ignores the rest, since what identifies a database differs between them.
	Open(ctx context.Context, cfg Config) (Conn, error)
}

// Config says which database to connect to, and how.
//
// Either give URL, or give the parts. The parts exist because a connection
// string assembled by hand from configuration is a common source of quoting
// mistakes, particularly around a password containing punctuation.
type Config struct {
	// Driver is the registered name, "postgres" for example. It may be left
	// empty when URL is set, since the URL's scheme names it.
	Driver string

	// URL is a complete connection string. When set, every field below except
	// the pool settings is ignored, and the driver is free to read parts of it
	// this package does not model.
	URL string

	Host     string
	Port     int
	User     string
	Password string
	Database string

	// Options are the driver specific settings that would otherwise be a
	// connection string's query parameters, such as sslmode for Postgres.
	Options map[string]string

	// MaxConns caps the pool. Zero leaves the driver's own default, which is
	// what most callers want.
	MaxConns int

	// MinConns keeps this many connections open even while idle, so a burst
	// after a quiet period does not pay to establish all of them.
	MinConns int

	// MaxConnLifetime and MaxConnIdleTime retire a connection by age and by
	// idleness. Both zero leaves the driver's defaults.
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// registry maps a driver name to its implementation.
//
// It is separate from the table registry in model_registry.go, which is keyed
// by row type and answers a different question entirely.
var drivers = struct {
	sync.RWMutex
	byName map[string]Driver
}{byName: map[string]Driver{}}

// Register makes a driver available under name, and under any aliases.
//
// A driver calls this from an init function, so linking the package in is what
// registers it. Registering the same name twice panics: two drivers answering
// to one name would make which database a program talked to depend on import
// order, which is not something a caller could debug.
//
//	func init() { orm.Register(Dialect{}, "postgres", "postgresql") }
func Register(d Driver, name string, aliases ...string) {
	if d == nil {
		panic("orm: Register was given a nil driver")
	}
	drivers.Lock()
	defer drivers.Unlock()
	for _, n := range append([]string{name}, aliases...) {
		if n == "" {
			panic("orm: Register was given an empty driver name")
		}
		if _, taken := drivers.byName[n]; taken {
			panic(fmt.Sprintf("orm: a driver named %q is already registered", n))
		}
		drivers.byName[n] = d
	}
}

// LookupDriver returns the driver registered under name.
func LookupDriver(name string) (Driver, bool) {
	drivers.RLock()
	defer drivers.RUnlock()
	d, ok := drivers.byName[name]
	return d, ok
}

// Drivers returns every registered name, sorted. It exists so an error can
// say what *is* available, which is usually enough to spot a missing import.
func Drivers() []string {
	drivers.RLock()
	defer drivers.RUnlock()
	names := make([]string, 0, len(drivers.byName))
	for n := range drivers.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Connect opens a database from a connection string.
//
//	db, err := orm.Connect(ctx, "postgres://tork:tork@localhost:5432/app")
//
// The scheme names the driver, so nothing else has to.
func Connect(ctx context.Context, connString string) (*DB, error) {
	return Open(ctx, Config{URL: connString})
}

// Open opens a database from a Config, for callers holding the parts
// separately rather than a string.
//
//	db, err := orm.Open(ctx, orm.Config{
//	    Driver:   "postgres",
//	    Host:     "localhost",
//	    Port:     5432,
//	    User:     "tork",
//	    Password: os.Getenv("DB_PASSWORD"),
//	    Database: "app",
//	    Options:  map[string]string{"sslmode": "require"},
//	    MaxConns: 20,
//	})
func Open(ctx context.Context, cfg Config) (*DB, error) {
	name, err := cfg.driverName()
	if err != nil {
		return nil, err
	}
	d, ok := LookupDriver(name)
	if !ok {
		return nil, unknownDriver(name)
	}
	conn, err := d.Open(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("orm: connecting to %s: %w", name, err)
	}
	return NewDB(conn, d), nil
}

// driverName works out which driver a Config asks for: the field if it is set,
// the URL's scheme otherwise.
func (c Config) driverName() (string, error) {
	if c.Driver != "" {
		return c.Driver, nil
	}
	if c.URL == "" {
		return "", fmt.Errorf("orm: Config names no driver and has no URL; " +
			"set Driver, or give a URL whose scheme names one")
	}
	scheme, _, ok := strings.Cut(c.URL, "://")
	if !ok || scheme == "" {
		return "", fmt.Errorf("orm: cannot tell which driver %q wants; "+
			"a connection string starts with a scheme, as postgres://…, "+
			"or set Config.Driver", redactURL(c.URL))
	}
	return scheme, nil
}

// unknownDriver reports a name nothing registered, and says how to fix it.
//
// The cause is almost always a missing blank import rather than a typo, so the
// message leads with the import line rather than with the name.
func unknownDriver(name string) error {
	available := Drivers()
	if len(available) == 0 {
		return fmt.Errorf("orm: no driver named %q, and no drivers are registered at all; "+
			"add the blank import that links one in, as\n"+
			"\t_ \"github.com/tork-go/orm/driver/%s\"", name, name)
	}
	return fmt.Errorf("orm: no driver named %q; registered: %s. "+
		"If %[1]q is the one you want, add\n\t_ \"github.com/tork-go/orm/driver/%[1]s\"",
		name, strings.Join(available, ", "))
}

// BuildURL assembles a connection string from a Config's parts, for a driver
// whose client takes a URL.
//
// It is exported because every driver needs it and none should write its own:
// the escaping is the part that goes wrong, and a password containing an @ or
// a slash is not unusual. A driver whose client wants some other format is free
// to ignore this and read the fields itself.
//
// When URL is set it is returned unchanged, since a caller who supplied a
// whole connection string means it.
func BuildURL(cfg Config, scheme string, defaultPort int) string {
	if cfg.URL != "" {
		return cfg.URL
	}

	u := url.URL{Scheme: scheme, Path: "/" + cfg.Database}
	switch {
	case cfg.User != "" && cfg.Password != "":
		u.User = url.UserPassword(cfg.User, cfg.Password)
	case cfg.User != "":
		u.User = url.User(cfg.User)
	}

	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	port := cfg.Port
	if port == 0 {
		port = defaultPort
	}
	u.Host = host + ":" + strconv.Itoa(port)

	if len(cfg.Options) > 0 {
		q := url.Values{}
		// Sorted, so the same Config always produces the same string, which is
		// what makes one comparable in a test.
		keys := make([]string, 0, len(cfg.Options))
		for k := range cfg.Options {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			q.Set(k, cfg.Options[k])
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// redactURL removes the password from a connection string so an error can
// quote it. An error is a thing that gets logged, and a logged password is a
// leaked one.
func redactURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.User == nil {
		return s
	}
	if _, hasPassword := u.User.Password(); !hasPassword {
		return s
	}
	u.User = url.UserPassword(u.User.Username(), "xxxxx")
	return u.String()
}
