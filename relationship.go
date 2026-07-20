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
