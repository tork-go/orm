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
		{
			name: "same numeric precision and scale",
			a:    schema.ColumnType{Kind: schema.KindNumeric, Precision: 10, Scale: 2},
			b:    schema.ColumnType{Kind: schema.KindNumeric, Precision: 10, Scale: 2},
			want: true,
		},
		{
			name: "different numeric precision",
			a:    schema.ColumnType{Kind: schema.KindNumeric, Precision: 10, Scale: 2},
			b:    schema.ColumnType{Kind: schema.KindNumeric, Precision: 12, Scale: 2},
			want: false,
		},
		{
			name: "different numeric scale",
			a:    schema.ColumnType{Kind: schema.KindNumeric, Precision: 10, Scale: 2},
			b:    schema.ColumnType{Kind: schema.KindNumeric, Precision: 10, Scale: 4},
			want: false,
		},
		{
			name: "same enum type name",
			a:    schema.ColumnType{Kind: schema.KindEnum, TypeName: "order_status"},
			b:    schema.ColumnType{Kind: schema.KindEnum, TypeName: "order_status"},
			want: true,
		},
		{
			name: "different enum type name",
			a:    schema.ColumnType{Kind: schema.KindEnum, TypeName: "order_status"},
			b:    schema.ColumnType{Kind: schema.KindEnum, TypeName: "payment_status"},
			want: false,
		},
		{
			name: "same array element kind",
			a:    schema.ColumnType{Kind: schema.KindArray, Elem: &schema.ColumnType{Kind: schema.KindText}},
			b:    schema.ColumnType{Kind: schema.KindArray, Elem: &schema.ColumnType{Kind: schema.KindText}},
			want: true,
		},
		{
			name: "different array element kind",
			a:    schema.ColumnType{Kind: schema.KindArray, Elem: &schema.ColumnType{Kind: schema.KindText}},
			b:    schema.ColumnType{Kind: schema.KindArray, Elem: &schema.ColumnType{Kind: schema.KindInteger}},
			want: false,
		},
		{
			name: "different array element varchar length",
			a:    schema.ColumnType{Kind: schema.KindArray, Elem: &schema.ColumnType{Kind: schema.KindVarchar, Length: 10}},
			b:    schema.ColumnType{Kind: schema.KindArray, Elem: &schema.ColumnType{Kind: schema.KindVarchar, Length: 20}},
			want: false,
		},
		{
			name: "json and jsonb are different kinds",
			a:    schema.ColumnType{Kind: schema.KindJSON},
			b:    schema.ColumnType{Kind: schema.KindJSONB},
			want: false,
		},
		{
			name: "both array types with nil element are equal",
			a:    schema.ColumnType{Kind: schema.KindArray},
			b:    schema.ColumnType{Kind: schema.KindArray},
			want: true,
		},
		{
			name: "nil element vs non-nil element are unequal",
			a:    schema.ColumnType{Kind: schema.KindArray},
			b:    schema.ColumnType{Kind: schema.KindArray, Elem: &schema.ColumnType{Kind: schema.KindText}},
			want: false,
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
