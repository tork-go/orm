package analyze

import "strings"

// initialisms are the words rendered in full caps in Go identifiers.
// The set is fixed and small on purpose: it covers what schema fields
// actually contain, and growing it later only changes cosmetics of
// newly generated code, never its behavior, because generated row
// structs always carry explicit db tags.
var initialisms = map[string]string{
	"id":   "ID",
	"url":  "URL",
	"uuid": "UUID",
	"api":  "API",
	"http": "HTTP",
	"sql":  "SQL",
	"json": "JSON",
	"ip":   "IP",
}

// splitWords cuts an identifier into its words, understanding all three
// spellings that show up in schemas: camelCase, snake_case, and
// uppercase runs like "HTMLParser" (which splits into HTML and Parser,
// the same rule go/lint applied). Digits stick to the word before them.
func splitWords(name string) []string {
	var words []string
	var cur []byte
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = nil
		}
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_':
			flush()
		case c >= 'A' && c <= 'Z':
			prevLower := i > 0 && name[i-1] >= 'a' && name[i-1] <= 'z'
			nextLower := i+1 < len(name) && name[i+1] >= 'a' && name[i+1] <= 'z'
			prevUpper := i > 0 && name[i-1] >= 'A' && name[i-1] <= 'Z'
			if prevLower || (prevUpper && nextLower) {
				flush()
			}
			cur = append(cur, c)
		default:
			cur = append(cur, c)
		}
	}
	flush()
	return words
}

// GoName renders a schema field name as the exported Go identifier the
// generated structs use: authorId becomes AuthorID, created_at becomes
// CreatedAt.
func GoName(name string) string {
	var b strings.Builder
	for _, w := range splitWords(name) {
		lower := strings.ToLower(w)
		if full, ok := initialisms[lower]; ok {
			b.WriteString(full)
			continue
		}
		b.WriteString(strings.ToUpper(w[:1]))
		b.WriteString(lower[1:])
	}
	return b.String()
}

// SnakeCase renders a schema name as a SQL identifier: authorId becomes
// author_id. Generated code never relies on the ORM guessing this back;
// every row struct field carries an explicit db tag.
func SnakeCase(name string) string {
	words := splitWords(name)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, "_")
}

// pluralize turns a singular table base name into the plural table
// name, on the last word only: user becomes users, category becomes
// categories, post_tag becomes post_tags. The rules are deliberately
// naive; an irregular plural is what @@map is for, and a collision the
// pluralizer causes is diagnosed rather than silently repaired.
func pluralize(snake string) string {
	switch {
	case strings.HasSuffix(snake, "s"), strings.HasSuffix(snake, "x"),
		strings.HasSuffix(snake, "z"), strings.HasSuffix(snake, "ch"),
		strings.HasSuffix(snake, "sh"):
		return snake + "es"
	case strings.HasSuffix(snake, "y") && len(snake) > 1 && !isVowel(snake[len(snake)-2]):
		return snake[:len(snake)-1] + "ies"
	default:
		return snake + "s"
	}
}

func isVowel(c byte) bool {
	switch c {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}

// isSQLIdent reports whether a name can be used verbatim as a quoted
// SQL identifier target for @map and @@map: letters, digits, and
// underscores, not starting with a digit.
func isSQLIdent(name string) bool {
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

// isGoIdent reports whether a name is a legal Go identifier, the
// validation behind @go.type and @default(go(...)).
func isGoIdent(name string) bool {
	return isSQLIdent(name)
}
