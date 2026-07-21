package orm

import (
	"strings"
	"unicode"
)

// snakeCase converts a Go field name to the column name it maps to by
// default: ID becomes id, AuthorID becomes author_id, CreatedAt becomes
// created_at, HTTPServer becomes http_server.
//
// An underscore goes before an upper-case rune that either follows a
// non-upper-case one, or is the last of a run of upper-case runes before a
// lower-case one. That second clause is what keeps acronyms together,
// since without it HTTPServer would come out h_t_t_p_server.
//
// This lives in orm rather than in schema/naming.go because it names
// columns from Go fields, whereas that file names database constraints
// from columns. They answer different questions and are free to diverge.
func snakeCase(s string) string {
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(rs) + 4)

	for i, r := range rs {
		if !unicode.IsUpper(r) {
			b.WriteRune(r)
			continue
		}
		if i > 0 {
			prevIsUpper := unicode.IsUpper(rs[i-1])
			nextIsLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if !prevIsUpper || nextIsLower {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
