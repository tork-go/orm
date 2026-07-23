package diag

import (
	"fmt"
	"sort"

	"github.com/tork-go/orm/gen/token"
)

// Severity separates findings that must stop code generation from
// findings the user should merely see. There are exactly two levels
// because the toolchain only ever makes one decision with them: generate
// or refuse. Anything finer (hints, notes) would be precision nothing
// consumes.
type Severity int

const (
	// Error marks a schema the generator refuses to generate from.
	Error Severity = iota
	// Warning marks something suspicious that does not block
	// generation, such as a foreign key referencing columns that are
	// not unique on the target.
	Warning
)

// String names the severity the way diagnostics and editors spell it.
func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	default:
		return fmt.Sprintf("Severity(%d)", int(s))
	}
}

// Diagnostic is one finding in one file. File is the path exactly as the
// caller named it (relative stays relative), Span is what an editor
// underlines, and Message is complete on its own: it names the offending
// construct and, following the house rule for errors, includes the fix
// when one is obvious.
type Diagnostic struct {
	File     string
	Span     token.Span
	Severity Severity
	Message  string
}

// String renders the classic compiler line "file:line:col: message",
// with "warning: " inserted for warnings so the two levels cannot be
// confused when a report scrolls past.
func (d Diagnostic) String() string {
	if d.Severity == Warning {
		return fmt.Sprintf("%s:%s: warning: %s", d.File, d.Span.Start, d.Message)
	}
	return fmt.Sprintf("%s:%s: %s", d.File, d.Span.Start, d.Message)
}

// Errorf builds an Error severity diagnostic with a formatted message.
func Errorf(file string, span token.Span, format string, args ...any) Diagnostic {
	return Diagnostic{File: file, Span: span, Severity: Error, Message: fmt.Sprintf(format, args...)}
}

// Warningf builds a Warning severity diagnostic with a formatted message.
func Warningf(file string, span token.Span, format string, args ...any) Diagnostic {
	return Diagnostic{File: file, Span: span, Severity: Warning, Message: fmt.Sprintf(format, args...)}
}

// Sort orders diagnostics by file, then by position within the file. The
// sort is stable, so two findings at the same position keep the order
// they were reported in, which tends to read as cause before consequence.
func Sort(ds []Diagnostic) {
	sort.SliceStable(ds, func(i, j int) bool {
		if ds[i].File != ds[j].File {
			return ds[i].File < ds[j].File
		}
		return ds[i].Span.Start.Offset < ds[j].Span.Start.Offset
	})
}

// HasErrors reports whether any diagnostic is an Error. This is the
// single question the generator asks before deciding whether a schema is
// fit to generate from; warnings alone never block it.
func HasErrors(ds []Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == Error {
			return true
		}
	}
	return false
}
