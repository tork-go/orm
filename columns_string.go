package orm

// StringColumn is a non-nullable string column.
//
// It is the only kind embedding textOps, so Contains, StartsWith,
// EndsWith, Like, and ILike exist here and nowhere else. It also embeds
// lengthBuilder, whose MaxLen decides TEXT versus VARCHAR(n).
//
// Native enums are string-backed but live in EnumColumn rather than here.
// Keeping them apart is what makes MaxLen and Enum mutually exclusive at
// compile time, since combining them is rejected during extraction.
type StringColumn struct {
	chain[string, *StringColumn]
	refBuilder[string, string, *StringColumn]
	lengthBuilder[string, *StringColumn]
	equatable[string]
	ordered[string]
	assignable[string]
	textOps
	sortable
}

var _ ColumnMeta = (*StringColumn)(nil)

// NewStringColumn declares a non-nullable string column named name.
// Without a MaxLen call it renders as TEXT.
func NewStringColumn(name string) *StringColumn {
	x := &StringColumn{}
	x.chain = newChain[string](name, x)
	c := x.chain.c
	x.refBuilder = refBuilder[string, string, *StringColumn]{c: c, self: x}
	x.lengthBuilder = lengthBuilder[string, *StringColumn]{c: c, self: x}
	x.equatable = equatable[string]{c: c}
	x.ordered = ordered[string]{c: c}
	x.assignable = assignable[string]{c: c}
	x.textOps = textOps{c: c}
	x.sortable = sortable{c: c}
	return x
}

// NullableStringColumn is a nullable string column, backed by
// Column[*string].
//
// Its comparison and assignment methods take string rather than *string,
// so callers write Email.Eq("alice@example.com") and Email.Set("new@x")
// directly; EqPtr, SetPtr, and SetNull cover the pointer and NULL cases.
// See nullEquatable for why.
type NullableStringColumn struct {
	chain[*string, *NullableStringColumn]
	refBuilder[*string, string, *NullableStringColumn]
	lengthBuilder[*string, *NullableStringColumn]
	nullEquatable[string]
	nullOrdered[string]
	nullAssignable[string]
	textOps
	nullness
	sortable
}

var _ ColumnMeta = (*NullableStringColumn)(nil)

// NewNullableStringColumn declares a nullable string column named name.
func NewNullableStringColumn(name string) *NullableStringColumn {
	x := &NullableStringColumn{}
	x.chain = newChain[*string](name, x)
	c := x.chain.c
	x.refBuilder = refBuilder[*string, string, *NullableStringColumn]{c: c, self: x}
	x.lengthBuilder = lengthBuilder[*string, *NullableStringColumn]{c: c, self: x}
	x.nullEquatable = nullEquatable[string]{c: c}
	x.nullOrdered = nullOrdered[string]{c: c}
	x.nullAssignable = nullAssignable[string]{c: c}
	x.textOps = textOps{c: c}
	x.nullness = nullness{c: c}
	x.sortable = sortable{c: c}
	return x
}
