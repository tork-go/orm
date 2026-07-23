package lsp

import (
	"github.com/tork-go/orm/gen/diag"
)

// publish reports findings for every file of the folder holding uri,
// not just that file. A schema is one namespace across its directory,
// so editing one file routinely fixes or breaks another, and a server
// that only ever republished the file being typed in would leave those
// stale errors on screen.
func (s *server) publish(uri string) error {
	path := uriToPath(uri)
	if path == "" {
		return nil
	}
	f, _ := s.forURI(uri)
	if f == nil {
		// The file is gone, or is not a .tork file. Clearing is the
		// only honest answer: whatever was reported about it before no
		// longer stands.
		return s.clear(uri)
	}

	byFile := map[string][]Diagnostic{}
	for _, name := range f.names {
		byFile[name] = []Diagnostic{}
	}
	// Every diagnostic names a file this folder parsed, so its
	// document is always in hand.
	for _, d := range f.diags {
		byFile[d.File] = append(byFile[d.File], Diagnostic{
			Range:    f.docs[d.File].rangeOf(d.Span),
			Severity: severity(d.Severity),
			Source:   "tork",
			Message:  d.Message,
		})
	}

	current := map[string]bool{}
	for name, ds := range byFile {
		fileURI := f.uriFor(name)
		current[fileURI] = true
		if err := s.send(fileURI, ds); err != nil {
			return err
		}
	}
	// A file that has left the folder keeps its diagnostics until they
	// are explicitly cleared, so sweep whatever was published before
	// and is no longer part of this schema.
	for prev := range s.published {
		if !current[prev] && sameFolder(prev, uri) {
			if err := s.clear(prev); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *server) send(uri string, ds []Diagnostic) error {
	if len(ds) == 0 && !s.published[uri] {
		// Never published anything here and have nothing to say, so
		// stay quiet rather than sending an empty list on every
		// keystroke for every clean file in the directory.
		return nil
	}
	s.published[uri] = len(ds) > 0
	return s.conn.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{
		URI:         uri,
		Diagnostics: ds,
	})
}

func (s *server) clear(uri string) error {
	if !s.published[uri] {
		return nil
	}
	delete(s.published, uri)
	return s.conn.notify("textDocument/publishDiagnostics", publishDiagnosticsParams{
		URI:         uri,
		Diagnostics: []Diagnostic{},
	})
}

// sameFolder reports whether two URIs name files in one directory,
// which bounds the sweep to the schema that was just analyzed.
func sameFolder(a, b string) bool {
	pa, pb := uriToPath(a), uriToPath(b)
	return pa != "" && pb != "" && dirOf(pa) == dirOf(pb)
}

func severity(s diag.Severity) int {
	if s == diag.Warning {
		return severityWarning
	}
	return severityError
}
