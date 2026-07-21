package orm

import (
	"time"

	"github.com/google/uuid"
)

// BoolColumn is a non-nullable bool column.
//
// It embeds no ordered mixin: `active > false` is legal SQL but never
// something a caller means, so Gt and friends are left off.
type BoolColumn struct {
	chain[bool, *BoolColumn]
	equatable[bool]
	assignable[bool]
	sortable
}

var _ ColumnMeta = (*BoolColumn)(nil)

// NewBoolColumn declares a non-nullable bool column named name.
func NewBoolColumn(name string) *BoolColumn {
	x := &BoolColumn{}
	x.chain = newChain[bool](name, x)
	c := x.chain.c
	x.equatable = equatable[bool]{c: c}
	x.assignable = assignable[bool]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableBoolColumn is a nullable bool column.
type NullableBoolColumn struct {
	chain[*bool, *NullableBoolColumn]
	nullEquatable[bool]
	nullAssignable[bool]
	nullness
	sortable
}

var _ ColumnMeta = (*NullableBoolColumn)(nil)

// NewNullableBoolColumn declares a nullable bool column named name.
func NewNullableBoolColumn(name string) *NullableBoolColumn {
	x := &NullableBoolColumn{}
	x.chain = newChain[*bool](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[bool]{c: c}
	x.nullAssignable = nullAssignable[bool]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// TimeColumn is a non-nullable timestamp column.
type TimeColumn struct {
	chain[time.Time, *TimeColumn]
	equatable[time.Time]
	ordered[time.Time]
	assignable[time.Time]
	sortable
}

var _ ColumnMeta = (*TimeColumn)(nil)

// NewTimeColumn declares a non-nullable timestamp column named name.
func NewTimeColumn(name string) *TimeColumn {
	x := &TimeColumn{}
	x.chain = newChain[time.Time](name, x)
	c := x.chain.c
	x.equatable = equatable[time.Time]{c: c}
	x.ordered = ordered[time.Time]{c: c}
	x.assignable = assignable[time.Time]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableTimeColumn is a nullable timestamp column.
type NullableTimeColumn struct {
	chain[*time.Time, *NullableTimeColumn]
	nullEquatable[time.Time]
	nullOrdered[time.Time]
	nullAssignable[time.Time]
	nullness
	sortable
}

var _ ColumnMeta = (*NullableTimeColumn)(nil)

// NewNullableTimeColumn declares a nullable timestamp column named name.
func NewNullableTimeColumn(name string) *NullableTimeColumn {
	x := &NullableTimeColumn{}
	x.chain = newChain[*time.Time](name, x)
	c := x.chain.c
	x.nullEquatable = nullEquatable[time.Time]{c: c}
	x.nullOrdered = nullOrdered[time.Time]{c: c}
	x.nullAssignable = nullAssignable[time.Time]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}

// UUIDColumn is a non-nullable UUID column.
//
// Like BoolColumn it embeds no ordered mixin. UUIDs do compare bytewise,
// but ordering by one is meaningful only for the time-ordered variants, so
// the inequalities are left off rather than offered misleadingly.
type UUIDColumn struct {
	chain[uuid.UUID, *UUIDColumn]
	refBuilder[uuid.UUID, uuid.UUID, *UUIDColumn]
	equatable[uuid.UUID]
	assignable[uuid.UUID]
	sortable
}

var _ ColumnMeta = (*UUIDColumn)(nil)

// NewUUIDColumn declares a non-nullable UUID column named name. Pair it
// with GeneratedByClient(uuid.New) or ServerDefault("gen_random_uuid()")
// to have values filled in automatically.
func NewUUIDColumn(name string) *UUIDColumn {
	x := &UUIDColumn{}
	x.chain = newChain[uuid.UUID](name, x)
	c := x.chain.c
	x.refBuilder = refBuilder[uuid.UUID, uuid.UUID, *UUIDColumn]{c: c, self: x}
	x.equatable = equatable[uuid.UUID]{c: c}
	x.assignable = assignable[uuid.UUID]{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableUUIDColumn is a nullable UUID column.
type NullableUUIDColumn struct {
	chain[*uuid.UUID, *NullableUUIDColumn]
	refBuilder[*uuid.UUID, uuid.UUID, *NullableUUIDColumn]
	nullEquatable[uuid.UUID]
	nullAssignable[uuid.UUID]
	nullness
	sortable
}

var _ ColumnMeta = (*NullableUUIDColumn)(nil)

// NewNullableUUIDColumn declares a nullable UUID column named name.
func NewNullableUUIDColumn(name string) *NullableUUIDColumn {
	x := &NullableUUIDColumn{}
	x.chain = newChain[*uuid.UUID](name, x)
	c := x.chain.c
	x.refBuilder = refBuilder[*uuid.UUID, uuid.UUID, *NullableUUIDColumn]{c: c, self: x}
	x.nullEquatable = nullEquatable[uuid.UUID]{c: c}
	x.nullAssignable = nullAssignable[uuid.UUID]{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}
