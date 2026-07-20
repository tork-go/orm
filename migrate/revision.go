package migrate

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// noneRevision is the literal down_revision value for a migration with no
// parent. It is always written, never omitted, so a migration file's
// header always has exactly two lines.
const noneRevision = "none"

// NewRevisionID generates a random 12 character hex revision id, e.g.
// "1975ea83b712".
func NewRevisionID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("migrate: generating revision id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Head returns the one revision that is nobody's down_revision: the tip
// of the migration chain. Zero migrations returns noneRevision.
func Head(migrations []Migration) (string, error) {
	if len(migrations) == 0 {
		return noneRevision, nil
	}

	revisions := make(map[string]bool, len(migrations))
	isParent := make(map[string]bool, len(migrations))
	for _, m := range migrations {
		revisions[m.Revision] = true
		if m.DownRevision != noneRevision {
			isParent[m.DownRevision] = true
		}
	}

	var heads []string
	for rev := range revisions {
		if !isParent[rev] {
			heads = append(heads, rev)
		}
	}

	switch len(heads) {
	case 1:
		return heads[0], nil
	case 0:
		return "", fmt.Errorf("migrate: no head found, the revision chain has a cycle")
	default:
		return "", fmt.Errorf("migrate: multiple heads found, the revision chain has branched: %v", heads)
	}
}

// chainOrder returns migrations sorted oldest to newest by following
// down_revision links from noneRevision. It errors if the chain is
// broken, branched, or cyclic (some migrations can't be reached).
func chainOrder(migrations []Migration) ([]Migration, error) {
	byRevision := make(map[string]Migration, len(migrations))
	childOf := make(map[string]string, len(migrations))
	for _, m := range migrations {
		byRevision[m.Revision] = m
		childOf[m.DownRevision] = m.Revision
	}

	var ordered []Migration
	visited := make(map[string]bool, len(migrations))
	cur := noneRevision
	for {
		next, ok := childOf[cur]
		if !ok {
			break
		}
		if visited[next] {
			return nil, fmt.Errorf("migrate: revision chain has a cycle at %s", next)
		}
		visited[next] = true
		m, ok := byRevision[next]
		if !ok {
			return nil, fmt.Errorf("migrate: revision %s has no matching migration file", next)
		}
		ordered = append(ordered, m)
		cur = next
	}

	if len(ordered) != len(migrations) {
		return nil, fmt.Errorf("migrate: revision chain is broken, branched, or cyclic (found %d of %d migrations)", len(ordered), len(migrations))
	}
	return ordered, nil
}
