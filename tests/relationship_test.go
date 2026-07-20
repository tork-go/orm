package orm_test

import (
	"testing"

	"github.com/tork-go/orm"
)

type relatedModel struct{ orm.Table }

// TestHasMany_ZeroValueUsable proves HasMany[T] is safe to leave
// uninitialized in a model's composite literal, as in the target API's
// `Posts HasMany[PostModel]` field.
func TestHasMany_ZeroValueUsable(t *testing.T) {
	var h orm.HasMany[relatedModel]
	if h != (orm.HasMany[relatedModel]{}) {
		t.Error("zero-value HasMany is not equal to its own literal zero value")
	}
}

// TestBelongsTo_ZeroValueUsable mirrors TestHasMany_ZeroValueUsable for the
// inverse relationship marker.
func TestBelongsTo_ZeroValueUsable(t *testing.T) {
	var b orm.BelongsTo[relatedModel]
	if b != (orm.BelongsTo[relatedModel]{}) {
		t.Error("zero-value BelongsTo is not equal to its own literal zero value")
	}
}

// TestHasOne_ZeroValueUsable mirrors TestHasMany_ZeroValueUsable for the
// one-to-one, non-owning-side marker.
func TestHasOne_ZeroValueUsable(t *testing.T) {
	var h orm.HasOne[relatedModel]
	if h != (orm.HasOne[relatedModel]{}) {
		t.Error("zero-value HasOne is not equal to its own literal zero value")
	}
}

// TestManyToMany_ZeroValueUsable mirrors TestHasMany_ZeroValueUsable for
// the many-to-many marker.
func TestManyToMany_ZeroValueUsable(t *testing.T) {
	var m orm.ManyToMany[relatedModel]
	if m != (orm.ManyToMany[relatedModel]{}) {
		t.Error("zero-value ManyToMany is not equal to its own literal zero value")
	}
}

// TestRelationshipMarkers_InStruct proves all four marker types are
// usable, uninitialized, as fields in a model struct's composite literal,
// the same way the target API leaves Posts/Author unset.
func TestRelationshipMarkers_InStruct(t *testing.T) {
	type Model struct {
		orm.Table
		Children orm.HasMany[relatedModel]
		Parent   orm.BelongsTo[relatedModel]
		Profile  orm.HasOne[relatedModel]
		Tags     orm.ManyToMany[relatedModel]
	}

	m := &Model{Table: orm.NewTable("models")}

	if m.Children != (orm.HasMany[relatedModel]{}) {
		t.Error("uninitialized Children field is not the zero value")
	}
	if m.Parent != (orm.BelongsTo[relatedModel]{}) {
		t.Error("uninitialized Parent field is not the zero value")
	}
	if m.Profile != (orm.HasOne[relatedModel]{}) {
		t.Error("uninitialized Profile field is not the zero value")
	}
	if m.Tags != (orm.ManyToMany[relatedModel]{}) {
		t.Error("uninitialized Tags field is not the zero value")
	}
}
