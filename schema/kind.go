package schema

import (
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
)

// KindForGoType maps a Go type to its default column Kind. Leading pointer
// indirection is unwrapped first, so *string and **string map the same as
// string. string always maps to KindText here; ExtractSchema upgrades it
// to KindVarchar when a column's MaxLen was set, since that information
// isn't part of a reflect.Type.
func KindForGoType(t reflect.Type) (Kind, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t {
	case reflect.TypeFor[bool]():
		return KindBoolean, nil
	case reflect.TypeFor[int](), reflect.TypeFor[int32]():
		return KindInteger, nil
	case reflect.TypeFor[int64]():
		return KindBigInteger, nil
	case reflect.TypeFor[float32]():
		return KindFloat, nil
	case reflect.TypeFor[float64]():
		return KindDouble, nil
	case reflect.TypeFor[string]():
		return KindText, nil
	case reflect.TypeFor[time.Time]():
		return KindTimestamp, nil
	case reflect.TypeFor[uuid.UUID]():
		return KindUUID, nil
	default:
		return 0, fmt.Errorf("schema: no column kind for Go type %s", t)
	}
}
