// Package noregistry_test exists only to be a test binary in which no driver
// has ever been registered.
//
// The registry is process-global, so the "you have not linked any driver in"
// message — the one a fresh user is likeliest to see — cannot be reached from
// any package that registers something, which every other test package does.
// A package of its own is the only way to observe it.
//
// It therefore imports orm and driver, and deliberately not driver/postgres or
// tests/fakedriver: linking either would register a driver and defeat the
// point.
package noregistry_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tork-go/orm"
	"github.com/tork-go/orm/driver"
)

func TestConnect_WithNoDriversLinkedIn(t *testing.T) {
	if got := orm.Drivers(); len(got) != 0 {
		t.Fatalf("this package registered %v; it must register nothing", got)
	}

	_, err := orm.Connect(context.Background(), "postgres://localhost/app")
	if err == nil {
		t.Fatal("Connect() error = nil, want the missing driver reported")
	}
	for _, want := range []string{
		"no drivers are registered at all",
		`_ "github.com/tork-go/orm/driver/postgres"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestDriverFor_WithNoDriversLinkedIn(t *testing.T) {
	_, err := driver.For("postgres://localhost/app")
	if err == nil {
		t.Fatal("For() error = nil, want the missing driver reported")
	}
	for _, want := range []string{
		"none are registered at all",
		`_ "github.com/tork-go/orm/driver/postgres"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
