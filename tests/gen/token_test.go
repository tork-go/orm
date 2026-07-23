package gen_test

import (
	"strings"
	"testing"

	"github.com/tork-go/orm/gen/token"
)

func TestKindString_NamesEveryKind(t *testing.T) {
	for k := token.KindEOF; k <= token.KindIllegal; k++ {
		name := k.String()
		if name == "" {
			t.Errorf("Kind(%d).String() is empty", int(k))
		}
		if strings.HasPrefix(name, "Kind(") {
			t.Errorf("Kind(%d).String() = %s, want a real name", int(k), name)
		}
	}
}

func TestKindString_FallsBackForUnknownValues(t *testing.T) {
	if got := token.Kind(99).String(); got != "Kind(99)" {
		t.Errorf("Kind(99).String() = %s, want Kind(99)", got)
	}
	if got := token.Kind(-1).String(); got != "Kind(-1)" {
		t.Errorf("Kind(-1).String() = %s, want Kind(-1)", got)
	}
}

func TestPosString_RendersLineColonColumn(t *testing.T) {
	p := token.Pos{Offset: 41, Line: 3, Col: 7}
	if got := p.String(); got != "3:7" {
		t.Errorf("Pos.String() = %s, want 3:7", got)
	}
}
