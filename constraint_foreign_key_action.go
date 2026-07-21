package orm

// ForeignKeyAction is a referential action Postgres runs when a
// referenced row is deleted or updated. The zero value, ActionNoAction,
// is Postgres's own default, so a ForeignKey that never calls
// OnDelete/OnUpdate renders no explicit clause.
type ForeignKeyAction int

const (
	ActionNoAction ForeignKeyAction = iota
	ActionCascade
	ActionSetNull
	ActionSetDefault
	ActionRestrict
)
