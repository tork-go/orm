package lsp

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/tork-go/orm/gen/analyze"
	"github.com/tork-go/orm/gen/ast"
	"github.com/tork-go/orm/gen/diag"
	"github.com/tork-go/orm/gen/parser"
)

// folder is one analyzed schema directory: the same unit the generator
// works on, so what the editor reports and what `go run
// ./cmd/generate` reports are the same findings.
type folder struct {
	dir    string
	names  []string             // base names, sorted
	docs   map[string]*document // base name to its text
	files  map[string]*ast.File // base name to its syntax
	schema *analyze.Schema
	diags  []diag.Diagnostic
}

// analyzeFolder reads every .tork file in a directory, letting the
// editor's unsaved buffers stand in for what is on disk, then parses
// and analyzes the lot. Unlike the batch tool this always analyzes,
// even when the parse had errors: half a schema still answers most of
// what an editor asks, and a user mid keystroke has a syntax error
// almost by definition.
func (s *server) analyzeFolder(dir string) *folder {
	f := &folder{
		dir:   dir,
		docs:  map[string]*document{},
		files: map[string]*ast.File{},
	}

	sources := map[string][]byte{}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".tork" {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if src, err := os.ReadFile(path); err == nil {
				sources[e.Name()] = src
			}
		}
	}
	// Open documents win over what is on disk, including ones the
	// editor has never saved, which is the whole point of overlays.
	for uri, text := range s.overlays {
		path := uriToPath(uri)
		if path == "" || filepath.Dir(path) != dir || filepath.Ext(path) != ".tork" {
			continue
		}
		sources[filepath.Base(path)] = []byte(text)
	}

	for name := range sources {
		f.names = append(f.names, name)
	}
	sort.Strings(f.names)

	parsed := make([]*ast.File, 0, len(f.names))
	for _, name := range f.names {
		src := sources[name]
		file, ds := parser.Parse(name, src)
		f.docs[name] = newDocument(string(src))
		f.files[name] = file
		parsed = append(parsed, file)
		f.diags = append(f.diags, ds...)
	}
	schema, analyzeDiags := analyze.Analyze(parsed)
	f.schema = schema
	f.diags = append(f.diags, analyzeDiags...)
	diag.Sort(f.diags)
	return f
}

// forURI analyzes the folder holding a document and returns it with
// that document's base name. An empty name means the URI names nothing
// this server can work with.
func (s *server) forURI(uri string) (*folder, string) {
	path := uriToPath(uri)
	if path == "" {
		return nil, ""
	}
	f := s.analyzeFolder(filepath.Dir(path))
	name := filepath.Base(path)
	if _, ok := f.docs[name]; !ok {
		return nil, ""
	}
	return f, name
}

// uriFor points at a file of this folder by base name.
func (f *folder) uriFor(name string) string {
	return pathToURI(filepath.Join(f.dir, name))
}

// dirOf is filepath.Dir under a name that reads at the call sites,
// where the question is always which schema a file belongs to.
func dirOf(path string) string { return filepath.Dir(path) }

// modelIn finds the model declared in a given file whose syntax spans
// the given offset, which is how a position turns into the model whose
// fields completion should offer.
func (f *folder) modelIn(name string, offset int) *analyze.Model {
	for _, m := range f.schema.Models {
		if m.File == name && m.Decl.Span.Start.Offset <= offset && offset <= m.Decl.Span.End.Offset {
			return m
		}
	}
	return nil
}
