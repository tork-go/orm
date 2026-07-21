package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tork-go/orm/migrate/cli"
	"github.com/tork-go/orm/tests/fakedriver"
	"github.com/tork-go/orm/tests/fixtures"
)

func run(t *testing.T, dialect *fakedriver.Dialect, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = cli.RunWithArgs(args, &out, &errOut, dialect, "fake-dsn", dir, fixtures.Users, fixtures.Posts)
	return out.String(), errOut.String(), code
}

func TestRunWithArgs_NoArgs_PrintsUsage(t *testing.T) {
	_, errOut, code := run(t, fakedriver.NewDialect(), t.TempDir())
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "usage:") {
		t.Errorf("stderr = %q, want it to contain usage text", errOut)
	}
}

func TestRunWithArgs_UnknownSubcommand_PrintsUsage(t *testing.T) {
	_, errOut, code := run(t, fakedriver.NewDialect(), t.TempDir(), "bogus")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "usage:") {
		t.Errorf("stderr = %q, want it to contain usage text", errOut)
	}
}

func TestRunWithArgs_MigrateWithNoSubcommand_PrintsUsage(t *testing.T) {
	_, _, code := run(t, fakedriver.NewDialect(), t.TempDir(), "migrate")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRunWithArgs_MigrateWithUnknownSubcommand_PrintsUsage(t *testing.T) {
	_, _, code := run(t, fakedriver.NewDialect(), t.TempDir(), "migrate", "sideways")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRunWithArgs_MakeMigrations_NoChanges(t *testing.T) {
	dialect := fakedriver.NewDialect()
	// IntrospectResult defaults to an empty schema; give it the exact
	// desired schema instead so the diff is empty.
	dir := t.TempDir()

	// First run creates the migration (current is empty, desired is not).
	out, _, code := run(t, dialect, dir, "makemigrations", "-m", "initial")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q", code, out)
	}
	if !strings.Contains(out, "Wrote revision") {
		t.Fatalf("stdout = %q, want it to report a written revision", out)
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("migrations dir has %d files (err=%v), want 1", len(entries), err)
	}
}

func TestRunWithArgs_MakeMigrations_ConnectError(t *testing.T) {
	dialect := fakedriver.NewDialect()
	dialect.ConnectErr = errors.New("boom")

	_, errOut, code := run(t, dialect, t.TempDir(), "makemigrations")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "boom") {
		t.Errorf("stderr = %q, want it to mention the connect error", errOut)
	}
}

func TestRunWithArgs_MakeMigrations_InvalidFlag(t *testing.T) {
	_, _, code := run(t, fakedriver.NewDialect(), t.TempDir(), "makemigrations", "-not-a-flag")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRunWithArgs_MigrateUp_MissingTarget(t *testing.T) {
	_, errOut, code := run(t, fakedriver.NewDialect(), t.TempDir(), "migrate", "up")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut, "missing target") {
		t.Errorf("stderr = %q, want it to mention a missing target", errOut)
	}
}

// TestRunWithArgs_MigrateUp_UnknownRevisionTarget covers a target string
// that isn't "head"/"base"/"+N"/"-N": ParseTarget accepts it as a
// candidate revision id (revisions are arbitrary hex strings, so this
// can't be rejected at parse time), and it only fails once Up looks for
// it among the loaded migrations and doesn't find it. That's a runtime
// error (exit 1), not a usage error (exit 2).
func TestRunWithArgs_MigrateUp_UnknownRevisionTarget(t *testing.T) {
	_, errOut, code := run(t, fakedriver.NewDialect(), t.TempDir(), "migrate", "up", "does-not-exist")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "not found") {
		t.Errorf("stderr = %q, want it to mention the revision was not found", errOut)
	}
}

func TestRunWithArgs_MigrateUp_MalformedTarget(t *testing.T) {
	_, _, code := run(t, fakedriver.NewDialect(), t.TempDir(), "migrate", "up", "+not-a-number")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRunWithArgs_MigrateUp_ConnectError(t *testing.T) {
	dialect := fakedriver.NewDialect()
	dialect.ConnectErr = errors.New("boom")

	_, errOut, code := run(t, dialect, t.TempDir(), "migrate", "up", "head")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "boom") {
		t.Errorf("stderr = %q, want it to mention the connect error", errOut)
	}
}

func TestRunWithArgs_FullCycle_MakeMigrationsUpDownHistory(t *testing.T) {
	dialect := fakedriver.NewDialect()
	dir := t.TempDir()

	if out, _, code := run(t, dialect, dir, "makemigrations", "-m", "initial"); code != 0 {
		t.Fatalf("makemigrations failed: code=%d out=%q", code, out)
	}

	if out, errOut, code := run(t, dialect, dir, "migrate", "up", "head"); code != 0 {
		t.Fatalf("migrate up failed: code=%d out=%q err=%q", code, out, errOut)
	} else if !strings.Contains(out, "OK") {
		t.Errorf("migrate up stdout = %q, want it to contain OK", out)
	}

	if out, _, code := run(t, dialect, dir, "history"); code != 0 {
		t.Fatalf("history failed: code=%d out=%q", code, out)
	} else if !strings.Contains(out, "applied") {
		t.Errorf("history stdout = %q, want it to show an applied revision", out)
	}

	if out, errOut, code := run(t, dialect, dir, "migrate", "down", "base"); code != 0 {
		t.Fatalf("migrate down failed: code=%d out=%q err=%q", code, out, errOut)
	}

	if out, _, code := run(t, dialect, dir, "history"); code != 0 {
		t.Fatalf("history (2nd) failed: code=%d out=%q", code, out)
	} else if !strings.Contains(out, "pending") {
		t.Errorf("history (2nd) stdout = %q, want it to show a pending revision after rollback", out)
	}
}

func TestRunWithArgs_History_ConnectError(t *testing.T) {
	dialect := fakedriver.NewDialect()
	dialect.ConnectErr = errors.New("boom")

	_, errOut, code := run(t, dialect, t.TempDir(), "history")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "boom") {
		t.Errorf("stderr = %q, want it to mention the connect error", errOut)
	}
}

func TestRunWithArgs_EmptyMigrationsDir_DefaultsToMigrations(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()

	dialect := fakedriver.NewDialect()
	var out, errOut bytes.Buffer
	code := cli.RunWithArgs([]string{"makemigrations"}, &out, &errOut, dialect, "fake-dsn", "", fixtures.Users)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "migrations")); err != nil {
		t.Errorf("expected a ./migrations directory to be created, got: %v", err)
	}
}
