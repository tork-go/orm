package migrate_test

import (
	"testing"

	"github.com/tork-go/orm/migrate"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "head", input: "head"},
		{name: "base", input: "base"},
		{name: "plus steps", input: "+2"},
		{name: "minus steps", input: "-1"},
		{name: "bare revision", input: "1975ea83b712"},
		{name: "empty is an error", input: "", wantErr: true},
		{name: "plus with no digits is an error", input: "+", wantErr: true},
		{name: "plus zero is an error", input: "+0", wantErr: true},
		{name: "minus non-numeric is an error", input: "-abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := migrate.ParseTarget(tt.input)
			if tt.wantErr && err == nil {
				t.Fatalf("ParseTarget(%q) succeeded, want an error", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ParseTarget(%q) failed: %v", tt.input, err)
			}
		})
	}
}
