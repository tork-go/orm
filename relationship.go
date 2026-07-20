package orm

// HasMany marks a one-to-many relationship field, where T is the related
// model type:
//
//	type UserModel struct {
//	    Table
//	    ID    *Column[int]
//	    Posts HasMany[PostModel]
//	}
//
// It is intentionally empty and safe to leave zero-valued (uninitialized)
// in a model's composite literal. Relationship resolution and eager loading
// are added by the future query-building package, which will extend this
// type using the related model's Table/Column/ForeignKey metadata.
type HasMany[T any] struct{}

// BelongsTo marks the inverse, many-to-one side of a relationship declared
// with HasMany elsewhere, where T is the related model type. See HasMany
// for phasing notes.
type BelongsTo[T any] struct{}

// HasOne marks a one-to-one relationship field, where T is the related
// model type, on the side that does not own the foreign key. It is the
// one-to-one analogue of HasMany's non-owning side, kept as its own type
// rather than reusing HasMany so a model documents "at most one related
// row" rather than "many". See HasMany for phasing notes.
type HasOne[T any] struct{}

// ManyToMany marks a many-to-many relationship field, where T is the
// related model type. It intentionally carries no join-table metadata
// (table name, join column names) yet: that shape is deferred to the
// future query-building package that will actually consume it, rather
// than guessed at now and likely redesigned once real requirements exist.
// See HasMany for phasing notes.
type ManyToMany[T any] struct{}
