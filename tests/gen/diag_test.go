package gen_test

import (
	"testing"

	"github.com/tork-go/orm/gen/diag"
	"github.com/tork-go/orm/gen/token"
)

func spanAt(offset, line, col int) token.Span {
	return token.Span{
		Start: token.Pos{Offset: offset, Line: line, Col: col},
		End:   token.Pos{Offset: offset + 1, Line: line, Col: col + 1},
	}
}

func TestDiagnosticString_RendersFileLineColumn(t *testing.T) {
	d := diag.Errorf("schema.tork", spanAt(10, 2, 5), "model %q redeclared", "User")
	if want := `schema.tork:2:5: model "User" redeclared`; d.String() != want {
		t.Errorf("String() = %s\nwant     = %s", d.String(), want)
	}
}

func TestDiagnosticString_PrefixesWarnings(t *testing.T) {
	d := diag.Warningf("a.tork", spanAt(0, 1, 1), "referenced columns are not unique")
	if want := "a.tork:1:1: warning: referenced columns are not unique"; d.String() != want {
		t.Errorf("String() = %s\nwant     = %s", d.String(), want)
	}
}

func TestErrorf_BuildsErrorSeverity(t *testing.T) {
	d := diag.Errorf("a.tork", spanAt(0, 1, 1), "unknown type %q", "Strng")
	if d.Severity != diag.Error {
		t.Errorf("Severity = %v, want %v", d.Severity, diag.Error)
	}
	if want := `unknown type "Strng"`; d.Message != want {
		t.Errorf("Message = %s, want %s", d.Message, want)
	}
}

func TestWarningf_BuildsWarningSeverity(t *testing.T) {
	d := diag.Warningf("a.tork", spanAt(0, 1, 1), "suspicious")
	if d.Severity != diag.Warning {
		t.Errorf("Severity = %v, want %v", d.Severity, diag.Warning)
	}
}

func TestSort_OrdersByFileThenOffset(t *testing.T) {
	ds := []diag.Diagnostic{
		diag.Errorf("b.tork", spanAt(5, 1, 6), "third"),
		diag.Errorf("a.tork", spanAt(9, 2, 1), "second"),
		diag.Errorf("b.tork", spanAt(0, 1, 1), "first in b"),
		diag.Errorf("a.tork", spanAt(0, 1, 1), "first in a"),
	}
	diag.Sort(ds)
	want := []string{"first in a", "second", "first in b", "third"}
	for i, w := range want {
		if ds[i].Message != w {
			t.Errorf("ds[%d].Message = %s, want %s", i, ds[i].Message, w)
		}
	}
}

func TestSort_IsStableAtEqualPositions(t *testing.T) {
	ds := []diag.Diagnostic{
		diag.Errorf("a.tork", spanAt(3, 1, 4), "cause"),
		diag.Errorf("a.tork", spanAt(3, 1, 4), "consequence"),
	}
	diag.Sort(ds)
	if ds[0].Message != "cause" || ds[1].Message != "consequence" {
		t.Errorf("Sort reordered equal positions: got %s then %s", ds[0].Message, ds[1].Message)
	}
}

func TestHasErrors_DistinguishesWarningsFromErrors(t *testing.T) {
	tests := map[string]struct {
		ds   []diag.Diagnostic
		want bool
	}{
		"empty": {nil, false},
		"warnings only": {[]diag.Diagnostic{
			diag.Warningf("a.tork", spanAt(0, 1, 1), "w"),
		}, false},
		"error among warnings": {[]diag.Diagnostic{
			diag.Warningf("a.tork", spanAt(0, 1, 1), "w"),
			diag.Errorf("a.tork", spanAt(2, 1, 3), "e"),
		}, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := diag.HasErrors(tt.ds); got != tt.want {
				t.Errorf("HasErrors = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSeverityString_NamesBothLevels(t *testing.T) {
	if got := diag.Error.String(); got != "error" {
		t.Errorf("Error.String() = %s, want error", got)
	}
	if got := diag.Warning.String(); got != "warning" {
		t.Errorf("Warning.String() = %s, want warning", got)
	}
	if got := diag.Severity(9).String(); got != "Severity(9)" {
		t.Errorf("Severity(9).String() = %s, want Severity(9)", got)
	}
}
