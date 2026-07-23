package analyze

// suggest returns the closest candidate to a misspelled name, or the
// empty string when nothing is close enough to be worth proposing. Two
// edits is the ceiling, and the distance must also stay small relative
// to the word: without that second bound every one letter name is
// "close" to every other and "did you mean" reads as a taunt rather
// than a fix.
// Callers only consult suggest after a lookup in the same candidate
// set failed, so got is never itself a candidate and needs no guard.
func suggest(got string, candidates []string) string {
	best := ""
	bestDist := 3
	for _, c := range candidates {
		if d := editDistance(got, c); d < bestDist && 2*d <= max(len(got), len(c)) {
			best, bestDist = c, d
		}
	}
	return best
}

// suggestion renders suggest's result as the parenthetical every
// diagnostic appends, so call sites stay one line.
func suggestion(got string, candidates []string) string {
	if s := suggest(got, candidates); s != "" {
		return " (did you mean " + quote(s) + "?)"
	}
	return ""
}

func quote(s string) string { return `"` + s + `"` }

// editDistance is plain Levenshtein over bytes. Schema identifiers are
// ASCII, and the two row rolling table keeps it allocation cheap for
// the sizes involved.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
