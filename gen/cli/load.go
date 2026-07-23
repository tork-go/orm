package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/tork-go/orm/gen/analyze"
	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/diag"
	"github.com/tork-go/orm/gen/parser"
)

// schemaFile is one .tork source read off disk, kept with its contents
// so the formatter can rewrite it without a second read.
type schemaFile struct {
	path string
	name string
	src  []byte
}

// readSchema lists and reads a schema directory. The listing is not
// recursive: one directory is one schema, which is the same rule the
// language server applies to the folder holding the file being edited,
// so what an editor shows and what the generator produces can never
// disagree about which files are in play.
func readSchema(dir string) ([]schemaFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("gen: reading schema directory %q: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".tork" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("gen: no .tork files in %q; a schema is a directory of .tork files", dir)
	}
	files := make([]schemaFile, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("gen: reading %q: %w", path, err)
		}
		files = append(files, schemaFile{path: path, name: name, src: src})
	}
	return files, nil
}

// loadSchema reads, parses, and analyzes a whole schema directory.
// Diagnostics from every stage come back together, sorted, because a
// user fixing a schema wants the whole report rather than one finding
// per run.
func loadSchema(dir string) (*analyze.Schema, []schemaFile, []diag.Diagnostic, error) {
	files, err := readSchema(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	parsed := make([]*ast.File, 0, len(files))
	var diags []diag.Diagnostic
	for _, f := range files {
		file, ds := parser.Parse(f.name, f.src)
		parsed = append(parsed, file)
		diags = append(diags, ds...)
	}
	// Analysis of a tree with syntax errors would pile guesses on top
	// of a broken parse; the language server accepts that trade for
	// completion, a batch tool should not.
	if diag.HasErrors(diags) {
		diag.Sort(diags)
		return nil, files, diags, nil
	}
	s, analyzeDiags := analyze.Analyze(parsed)
	diags = append(diags, analyzeDiags...)
	diag.Sort(diags)
	return s, files, diags, nil
}

// report prints diagnostics one per line and says whether any of them
// blocks the run.
func report(errOut io.Writer, diags []diag.Diagnostic) bool {
	for _, d := range diags {
		fmt.Fprintln(errOut, d)
	}
	return diag.HasErrors(diags)
}
