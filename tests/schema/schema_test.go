package schema_test

import (
	"testing"

	"github.com/tork-go/orm/schema"
)

func TestColumnType_Equal(t *testing.T) {
	tests := []struct {
		name string
		a, b schema.ColumnType
		want bool
	}{
		{
			name: "same varchar kind and length",
			a:    schema.ColumnType{Kind: schema.KindVarchar, Length: 30},
			b:    schema.ColumnType{Kind: schema.KindVarchar, Length: 30},
			want: true,
		},
		{
			name: "different kind",
			a:    schema.ColumnType{Kind: schema.KindInteger},
			b:    schema.ColumnType{Kind: schema.KindBigInteger},
			want: false,
		},
		{
			name: "different varchar length",
			a:    schema.ColumnType{Kind: schema.KindVarchar, Length: 30},
			b:    schema.ColumnType{Kind: schema.KindVarchar, Length: 100},
			want: false,
		},
		{
			name: "length ignored for non-varchar kinds",
			a:    schema.ColumnType{Kind: schema.KindInteger, Length: 30},
			b:    schema.ColumnType{Kind: schema.KindInteger, Length: 0},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equal(tt.b); got != tt.want {
				t.Errorf("Equal() = %v, want %v", got, tt.want)
			}
			if got := tt.b.Equal(tt.a); got != tt.want {
				t.Errorf("Equal() is not symmetric: b.Equal(a) = %v, want %v", got, tt.want)
			}
		})
	}
}
