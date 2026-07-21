package orm

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// ValueCodec is the type erased half of a column's value handling.
//
// Generator and Serializer both mention T, so neither can be part of
// ColumnMeta, which exists precisely so every Column[T] has the same
// method set whatever T is. Anything working through ColumnMeta therefore
// cannot reach them, which is a problem for the code that has to put a Go
// value into a statement or take one back out of a row. The three methods
// here say the same things without naming T.
//
// It is kept off ColumnMeta rather than folded into it so that interface
// stays a pure description of a column, the same way Indexer and Checker
// are optional interfaces on a model rather than methods on Model. Every
// Column[T] satisfies it, so a type assertion never fails in practice, but
// asserting states plainly which of the two jobs the caller is doing.
type ValueCodec interface {
	// GenerateAny returns a freshly generated value for the column and
	// true, or false if the column has no client side generator.
	GenerateAny() (any, bool)

	// MarshalAny encodes v for storage in a document column.
	MarshalAny(v any) ([]byte, error)

	// UnmarshalAny decodes b into the column's Go type.
	UnmarshalAny(b []byte) (any, error)
}

// GenerateAny returns a value from the generator set by GeneratedByClient,
// or false if there is none.
func (c *Column[T]) GenerateAny() (any, bool) {
	if c.generator == nil {
		return nil, false
	}
	return c.generator(), true
}

// MarshalAny encodes v with the pair set by Serialize, or with
// encoding/json when none was set.
//
// The default matters: a column can be JSONB without anyone calling
// Serialize, and before this there was nothing defining how such a column
// was meant to be encoded.
func (c *Column[T]) MarshalAny(v any) ([]byte, error) {
	tv, ok := v.(T)
	if !ok {
		return nil, fmt.Errorf("orm: column %q: cannot encode %T, want %s",
			c.name, v, reflect.TypeFor[T]())
	}
	if c.marshal != nil {
		return c.marshal(tv)
	}
	return json.Marshal(tv)
}

// UnmarshalAny decodes b with the pair set by Serialize, or with
// encoding/json when none was set. The returned value is always a T.
func (c *Column[T]) UnmarshalAny(b []byte) (any, error) {
	v, err := c.unmarshalTyped(b)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// unmarshalTyped is UnmarshalAny without the type erasure.
//
// It exists for the callers that already know the column's T, which is every
// caller reaching a column through Ref[T] rather than through ColumnMeta.
// Those would otherwise have to assert the any back to a T and handle a
// failure that cannot happen, since this is what produced the value.
func (c *Column[T]) unmarshalTyped(b []byte) (T, error) {
	var out T
	if c.unmarshal != nil {
		v, err := c.unmarshal(b)
		if err != nil {
			return out, fmt.Errorf("orm: column %q: %w", c.name, err)
		}
		return v, nil
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("orm: column %q: %w", c.name, err)
	}
	return out, nil
}
