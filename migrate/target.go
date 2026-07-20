package migrate

import (
	"fmt"
	"strconv"
	"strings"
)

type targetKind int

const (
	targetHead targetKind = iota
	targetBase
	targetRevision
	targetSteps
)

// Target identifies how far Up or Down should go: to head (Up) or base
// (Down), a fixed number of steps, or a specific revision.
type Target struct {
	kind     targetKind
	revision string
	steps    int
}

// HeadTarget targets the latest migration: every pending migration
// applied. Only valid for Up.
func HeadTarget() Target { return Target{kind: targetHead} }

// BaseTarget targets the empty state: every applied migration rolled
// back. Only valid for Down.
func BaseTarget() Target { return Target{kind: targetBase} }

// RevisionTarget targets a specific migration by id.
func RevisionTarget(id string) Target { return Target{kind: targetRevision, revision: id} }

// StepsTarget targets n migrations forward (Up) or back (Down) from the
// current position. n must be positive.
func StepsTarget(n int) Target { return Target{kind: targetSteps, steps: n} }

// ParseTarget parses a CLI-style target argument: "head", "base", "+N",
// "-N", or a bare revision id.
func ParseTarget(s string) (Target, error) {
	switch {
	case s == "":
		return Target{}, fmt.Errorf("migrate: empty target")
	case s == "head":
		return HeadTarget(), nil
	case s == "base":
		return BaseTarget(), nil
	case strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-"):
		n, err := strconv.Atoi(s[1:])
		if err != nil || n <= 0 {
			return Target{}, fmt.Errorf("migrate: invalid step count %q", s)
		}
		return StepsTarget(n), nil
	default:
		return RevisionTarget(s), nil
	}
}
