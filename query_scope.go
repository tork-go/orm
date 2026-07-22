package orm

// Unscoped disables the table's default scope for this query: its Scoper
// predicate and/or its soft-delete "not yet deleted" filter. It also
// disables the scope everywhere this query reaches: eager Load/Preload
// sub-queries and Has/HasNone EXISTS subqueries still consult their own
// table's scope unless Unscoped was called on the query that reaches them
// too.
func (q *Query[E]) Unscoped() *Filtered[E] { return q.filtered().Unscoped() }

// Unscoped disables the table's default scope for this query. See
// Query.Unscoped.
func (f *Filtered[E]) Unscoped() *Filtered[E] {
	out := f.clone()
	out.unscoped = true
	return out
}

// effectivePreds is what a WHERE clause compiles from: the query's own
// conditions, plus the table's default scope, unless Unscoped was called.
//
// It builds a new slice rather than appending into preds, so nothing here
// ever mutates or aliases what a clone sees.
func (q queryState) effectivePreds() []Predicate {
	if q.unscoped || q.st == nil {
		return q.preds
	}
	scope := q.st.defaultScope()
	if scope == nil {
		return q.preds
	}
	out := make([]Predicate, 0, len(q.preds)+1)
	out = append(out, q.preds...)
	out = append(out, scope)
	return out
}
