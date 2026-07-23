package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/tork-go/orm/gen/diag"
	"github.com/tork-go/orm/gen/format"
)

// runFormat rewrites the schema files in canonical form, or with list
// set only names the ones that are not canonical, which is the shape a
// check step wants. Formatting works file by file and never analyzes,
// so a schema that does not yet make sense can still be tidied.
func runFormat(out, errOut io.Writer, cfg Config, list bool) int {
	files, err := readSchema(cfg.SchemaDir)
	if err != nil {
		fmt.Fprintln(errOut, "fmt:", err)
		return 1
	}
	var diags []diag.Diagnostic
	changed := 0
	for _, f := range files {
		formatted, ds := format.Source(f.name, f.src)
		diags = append(diags, ds...)
		if bytes.Equal(formatted, f.src) {
			continue
		}
		changed++
		if list {
			fmt.Fprintln(out, f.path)
			continue
		}
		if err := os.WriteFile(f.path, formatted, 0o644); err != nil {
			fmt.Fprintf(errOut, "fmt: gen: writing %q: %v\n", f.path, err)
			return 1
		}
		fmt.Fprintf(out, "Formatted %s\n", f.path)
	}
	// Files with syntax errors were left alone by the formatter; the
	// report explains why they did not change.
	if report(errOut, diags) {
		return 1
	}
	if changed == 0 && !list {
		fmt.Fprintf(out, "%s: %d %s already canonical\n", cfg.SchemaDir, len(files), plural(len(files), "file"))
	}
	return 0
}
