package schema_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/tork-go/orm/schema"
)

type unsupportedStruct struct{ X int }

func TestKindForGoType(t *testing.T) {
	tests := []struct {
		name    string
		typ     reflect.Type
		want    schema.Kind
		wantErr bool
	}{
		{name: "bool", typ: reflect.TypeFor[bool](), want: schema.KindBoolean},
		{name: "int", typ: reflect.TypeFor[int](), want: schema.KindInteger},
		{name: "int32", typ: reflect.TypeFor[int32](), want: schema.KindInteger},
		{name: "int64", typ: reflect.TypeFor[int64](), want: schema.KindBigInteger},
		{name: "float32", typ: reflect.TypeFor[float32](), want: schema.KindFloat},
		{name: "float64", typ: reflect.TypeFor[float64](), want: schema.KindDouble},
		{name: "string", typ: reflect.TypeFor[string](), want: schema.KindText},
		{name: "time.Time", typ: reflect.TypeFor[time.Time](), want: schema.KindTimestamp},
		{name: "*string unwraps to string", typ: reflect.TypeFor[*string](), want: schema.KindText},
		{name: "*int unwraps to int", typ: reflect.TypeFor[*int](), want: schema.KindInteger},
		{name: "**int unwraps through two pointers", typ: reflect.TypeFor[**int](), want: schema.KindInteger},
		{name: "struct is unsupported", typ: reflect.TypeFor[unsupportedStruct](), wantErr: true},
		{name: "slice is unsupported", typ: reflect.TypeFor[[]int](), wantErr: true},
		{name: "map is unsupported", typ: reflect.TypeFor[map[string]int](), wantErr: true},
		{name: "chan is unsupported", typ: reflect.TypeFor[chan int](), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := schema.KindForGoType(tt.typ)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("KindForGoType(%s) = %v, nil, want an error", tt.typ, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("KindForGoType(%s) unexpected error: %v", tt.typ, err)
			}
			if got != tt.want {
				t.Errorf("KindForGoType(%s) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}
