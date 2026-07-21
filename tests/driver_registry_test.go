package orm_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver"
	"github.com/tork-go/orm/tests/fakedriver"
)

// Migrations read the same registry connecting does, narrowed to the drivers
// that can also render schema. One registry, so the two can never disagree
// about which driver a name means.
func TestDriverFor_ResolvesFromTheScheme(t *testing.T) {
	fake := fakedriver.NewDialect()
	dsn := fakedriver.Register(fake)

	got, err := driver.For(dsn)
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}
	if got != driver.Dialect(fake) {
		t.Error("For() resolved to a different dialect than was registered")
	}

	scheme, _, _ := strings.Cut(dsn, "://")
	if _, ok := driver.Lookup(scheme); !ok {
		t.Errorf("Lookup(%q) found nothing", scheme)
	}
}

func TestDriverFor_MalformedConnectionString(t *testing.T) {
	if _, err := driver.For("no-scheme-here"); err == nil {
		t.Error("For() error = nil, want the missing scheme reported")
	} else if !strings.Contains(err.Error(), "starts with a scheme") {
		t.Errorf("error %q does not say what a connection string looks like", err)
	}
}

func TestDriverFor_UnknownNamesTheImport(t *testing.T) {
	_, err := driver.For("nosuchdb://localhost/app")
	if err == nil {
		t.Fatal("For() error = nil, want the unknown driver reported")
	}
	if !strings.Contains(err.Error(), `_ "github.com/tork-go/orm/driver/nosuchdb"`) {
		t.Errorf("error %q does not name the import to add", err)
	}
}

// queryOnly is a driver that can run statements but renders no schema, which
// orm.Driver allows and driver.Dialect does not. Reporting that plainly beats
// a nil dialect for the caller to trip over.
type queryOnly struct{ stubDriver }

func TestDriverFor_RegisteredButCannotMigrate(t *testing.T) {
	orm.Register(&queryOnly{}, "queryonlydriver")

	_, err := driver.For("queryonlydriver://localhost/app")
	if err == nil {
		t.Fatal("For() error = nil, want the missing schema support reported")
	}
	for _, want := range []string{"queries but not migrations", "queryonlydriver"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestDriverLookup_Unknown(t *testing.T) {
	if _, ok := driver.Lookup("nothingregisteredunderthisname"); ok {
		t.Error("Lookup found a driver nobody registered")
	}
}
