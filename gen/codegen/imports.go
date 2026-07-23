package codegen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tork-go/orm/gen/analyze"
)

// imports collects one file's import set and hands out the package
// identifier each path is referred to by. Identifiers are derived from
// the last path element the way Go convention names packages, with an
// explicit alias emitted whenever the derivation had to guess (a
// versioned gopkg.in path, a dash), so the generated file compiles even
// when the guess and the real package name agree only by alias.
type imports struct {
	idents map[string]string // path -> identifier
	used   map[string]string // identifier -> path
}

func newImports() *imports {
	return &imports{idents: map[string]string{}, used: map[string]string{}}
}

// add registers a path and returns the identifier code should qualify
// it with. Registering twice is free and returns the same identifier.
func (s *imports) add(path string) string {
	if ident, ok := s.idents[path]; ok {
		return ident
	}
	ident := deriveIdent(path)
	for i := 2; ; i++ {
		if other, taken := s.used[ident]; !taken || other == path {
			break
		}
		ident = deriveIdent(path) + strconv.Itoa(i)
	}
	s.idents[path] = ident
	s.used[ident] = path
	return ident
}

// deriveIdent guesses the package identifier for an import path: the
// last element, cut at the first dot (gopkg.in/yaml.v3 is package
// yaml) and after the last dash (go-cmp is package cmp).
func deriveIdent(path string) string {
	base := path[strings.LastIndexByte(path, '/')+1:]
	ident := base
	if i := strings.IndexByte(ident, '.'); i >= 0 {
		ident = ident[:i]
	}
	if i := strings.LastIndexByte(ident, '-'); i >= 0 {
		ident = ident[i+1:]
	}
	if !isGoIdent(ident) {
		return "pkg"
	}
	return ident
}

// render prints the import declaration: standard library first, then a
// blank line, then everything else, each group sorted by path, with
// aliases only where the identifier is not the path's own last
// element. The set is never empty, since every file that renders
// imports at all imports the ORM.
func (s *imports) render() string {
	var std, ext []string
	for path := range s.idents {
		if strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
			ext = append(ext, path)
		} else {
			std = append(std, path)
		}
	}
	sort.Strings(std)
	sort.Strings(ext)
	line := func(path string) string {
		ident := s.idents[path]
		if base := path[strings.LastIndexByte(path, '/')+1:]; ident != base {
			return fmt.Sprintf("\t%s %q\n", ident, path)
		}
		return fmt.Sprintf("\t%q\n", path)
	}
	if len(std)+len(ext) == 1 {
		all := append(std, ext...)
		return "import " + strings.TrimSpace(strings.TrimPrefix(line(all[0]), "\t")) + "\n\n"
	}
	var b strings.Builder
	b.WriteString("import (\n")
	for _, p := range std {
		b.WriteString(line(p))
	}
	if len(std) > 0 && len(ext) > 0 {
		b.WriteString("\n")
	}
	for _, p := range ext {
		b.WriteString(line(p))
	}
	b.WriteString(")\n\n")
	return b.String()
}

// qualify renders a schema Go type reference in this file: bare for
// the generated package's own identifiers, package qualified with the
// import registered otherwise.
func (g *generator) qualify(imp *imports, ref analyze.GoTypeRef) string {
	if ref.ImportPath == "" {
		return ref.Name
	}
	return imp.add(ref.ImportPath) + "." + ref.Name
}

// isGoIdent mirrors the analyzer's spelling rule; it lives here too so
// codegen stays free of analyzer internals beyond the semantic model.
func isGoIdent(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
