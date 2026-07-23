package analyze

import "strings"

// parseGoRef splits a schema's reference to Go code into import path
// and identifier. Three spellings are accepted:
//
//	"Profile"                          a name in the generated package
//	"myapp/models.Profile"             a name in another package
//	"gopkg.in/yaml.v3.Node"            dots in the path's last element
//
// The identifier is whatever follows the last dot after the last
// slash, because import path elements may themselves contain dots but
// a Go identifier never does.
func parseGoRef(s string) (GoTypeRef, bool) {
	slash := strings.LastIndexByte(s, '/')
	rest := s[slash+1:]
	dot := strings.LastIndexByte(rest, '.')
	if dot < 0 {
		if slash >= 0 {
			return GoTypeRef{}, false
		}
		if !isGoIdent(s) {
			return GoTypeRef{}, false
		}
		return GoTypeRef{Name: s}, true
	}
	path := s[:slash+1] + rest[:dot]
	name := rest[dot+1:]
	if !isGoIdent(name) || !isImportPath(path) {
		return GoTypeRef{}, false
	}
	return GoTypeRef{ImportPath: path, Name: name}, true
}

// isImportPath is a light plausibility check, not a full validation;
// the user's compiler is the authority. It exists to catch quoting
// accidents ("models Profile", trailing slashes) at schema check time.
func isImportPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			return false
		}
		for i := 0; i < len(seg); i++ {
			c := seg[i]
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			case c == '.', c == '-', c == '_', c == '~':
			default:
				return false
			}
		}
	}
	return true
}
