package gen_test

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tork-go/orm/gen/analyze"
	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/codegen"
	"github.com/tork-go/orm/gen/diag"
	"github.com/tork-go/orm/gen/parser"
)

// update rewrites the golden files instead of comparing against them,
// through `go test ./tests/gen/ -run Golden -update` (or `make
// golden-update`). Reviewing the resulting diff is how a codegen change
// gets checked; the test itself never decides that output is fine.
var update = flag.Bool("update", false, "rewrite the golden files under testdata")

// generateCase parses, analyzes, and generates one testdata case,
// failing on any diagnostic so a golden comparison can never quietly
// bless a broken schema.
func generateCase(t *testing.T, dir, pkg string) []codegen.File {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("testdata", dir, "schema"))
	if err != nil {
		t.Fatalf("reading schema directory: %v", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tork") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var files []*ast.File
	var diags []diag.Diagnostic
	for _, name := range names {
		path := filepath.Join("testdata", dir, "schema", name)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		f, ds := parser.Parse(name, src)
		files = append(files, f)
		diags = append(diags, ds...)
	}
	s, analyzeDiags := analyze.Analyze(files)
	diags = append(diags, analyzeDiags...)
	if len(diags) != 0 {
		t.Fatalf("%s reported diagnostics:\n%s", dir, strings.Join(diagStrings(diags), "\n"))
	}
	out, err := codegen.Generate(s, codegen.Options{Package: pkg})
	if err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	return out
}

// assertGolden compares generated files against a golden directory,
// or rewrites it under -update. Extra golden files count as failures
// too, so a model deleted from a schema cannot leave a stale
// expectation behind. Only .gen.go files are considered, which is what
// lets a golden directory double as a real package holding handwritten
// code alongside the generated files.
func assertGolden(t *testing.T, wantDir string, files []codegen.File) {
	t.Helper()
	existing, err := filepath.Glob(filepath.Join(wantDir, "*.gen.go"))
	if err != nil {
		t.Fatalf("listing golden files: %v", err)
	}
	if *update {
		for _, path := range existing {
			if err := os.Remove(path); err != nil {
				t.Fatalf("removing stale golden file: %v", err)
			}
		}
		if err := os.MkdirAll(wantDir, 0o755); err != nil {
			t.Fatalf("creating golden directory: %v", err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(wantDir, f.Name), f.Content, 0o644); err != nil {
				t.Fatalf("writing golden file: %v", err)
			}
		}
		return
	}
	got := map[string][]byte{}
	for _, f := range files {
		got[f.Name] = f.Content
	}
	if len(existing) == 0 {
		t.Fatalf("no golden files under %s (run with -update to create them)", wantDir)
	}
	for _, path := range existing {
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading golden file: %v", err)
		}
		name := filepath.Base(path)
		content, ok := got[name]
		if !ok {
			t.Errorf("%s was not generated, but a golden file exists", name)
			continue
		}
		if !bytes.Equal(content, want) {
			t.Errorf("%s does not match its golden file\ngot:\n%s\nwant:\n%s", name, content, want)
		}
		delete(got, name)
	}
	for name := range got {
		t.Errorf("%s was generated but has no golden file (run with -update)", name)
	}
}

func TestGenerate_GoldenBlog(t *testing.T) {
	assertGolden(t, filepath.Join("testdata", "blog", "want"), generateCase(t, "blog", "models"))
}

// TestGenerate_GoldenKitchen holds the generator to the committed
// tests/genfixtures package. That package is not testdata: the Go
// toolchain compiles it, so a drift here and a compile failure there
// are two views of the same regression.
func TestGenerate_GoldenKitchen(t *testing.T) {
	assertGolden(t, filepath.Join("..", "genfixtures"), generateCase(t, "kitchen", "genfixtures"))
}

// TestGenerate_FileLayoutDoesNotAffectOutput is the determinism proof:
// the same models split across three files, declared in a different
// order, must generate byte identical code.
func TestGenerate_FileLayoutDoesNotAffectOutput(t *testing.T) {
	single := generateCase(t, "kitchen", "genfixtures")
	split := generateCase(t, "kitchen-split", "genfixtures")
	if len(single) != len(split) {
		t.Fatalf("file counts differ: %d vs %d", len(single), len(split))
	}
	for i := range single {
		if single[i].Name != split[i].Name {
			t.Fatalf("file %d = %s, want %s", i, split[i].Name, single[i].Name)
		}
		if !bytes.Equal(single[i].Content, split[i].Content) {
			t.Errorf("%s differs between the single file and split schemas\ngot:\n%s\nwant:\n%s",
				single[i].Name, split[i].Content, single[i].Content)
		}
	}
}

func TestGenerate_RejectsAnInvalidPackageName(t *testing.T) {
	s, diags := analyzeOne(t, dsLine+"\nmodel A {\nid String @id\n}\n")
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diagStrings(diags))
	}
	for _, name := range []string{"not a package", "", "1models", "func"} {
		t.Run(name, func(t *testing.T) {
			_, err := codegen.Generate(s, codegen.Options{Package: name})
			if err == nil {
				t.Fatalf("expected Generate to reject the package name %q", name)
			}
			want := fmt.Sprintf("codegen: package name %q is not a valid Go identifier", name)
			if err.Error() != want {
				t.Errorf("error = %v\nwant  = %s", err, want)
			}
		})
	}
}

// TestGenerate_ReportsUnparseableOutput covers the one thing codegen
// cannot validate for itself: a schema may bind a Json column to any
// Go name at all, including one that is not usable as a type. Parsing
// the result is the backstop, and its message must point back at the
// schema rather than blaming the printer.
func TestGenerate_ReportsUnparseableOutput(t *testing.T) {
	s, diags := analyzeOne(t, dsLine+"\nmodel A {\nid String @id\ndata Json @go.type(\"range\")\n}\n")
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diagStrings(diags))
	}
	_, err := codegen.Generate(s, codegen.Options{Package: "models"})
	if err == nil {
		t.Fatal("expected Generate to reject output that does not parse")
	}
	if !strings.HasPrefix(err.Error(), "codegen: generated a.gen.go does not parse:") {
		t.Errorf("error = %v, want it to name the file that failed to parse", err)
	}
	if !strings.Contains(err.Error(), "@go.type") {
		t.Errorf("error = %v, want it to point at the schema attribute responsible", err)
	}
}

func TestGenerate_RejectsModelsSharingAFileName(t *testing.T) {
	s, diags := analyzeOne(t, dsLine+"\nmodel ABc {\nid String @id\n@@map(\"t1\")\n}\nmodel A_Bc {\nid String @id\n@@map(\"t2\")\n}\n")
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diagStrings(diags))
	}
	_, err := codegen.Generate(s, codegen.Options{Package: "models"})
	if err == nil {
		t.Fatal("expected Generate to reject two models generating one file")
	}
	if want := `codegen: models "ABc" and "A_Bc" both generate the file "a_bc.gen.go"; rename one`; err.Error() != want {
		t.Errorf("error = %v\nwant  = %s", err, want)
	}
}

func TestGenerate_EmptySchemaStillGeneratesARegistry(t *testing.T) {
	s, _ := analyze.Analyze(nil)
	files, err := codegen.Generate(s, codegen.Options{Package: "models"})
	if err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	if len(files) != 1 || files[0].Name != "registry.gen.go" {
		t.Fatalf("files = %v, want just registry.gen.go", files)
	}
	if !strings.Contains(string(files[0].Content), "return nil") {
		t.Errorf("empty registry should return nil:\n%s", files[0].Content)
	}
}
