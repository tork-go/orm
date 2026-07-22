package orm

import "fmt"

// joinKind distinguishes Join from LeftJoin.
type joinKind int

const (
	joinInner joinKind = iota
	joinLeft
)

// joinSpec is one Join, LeftJoin or JoinOn call: which relationship, which
// kind, and any extra conditions JoinOn ANDs onto the ON clause.
type joinSpec struct {
	kind  joinKind
	rel   *relation
	extra []Predicate
}

// Join adds an INNER JOIN to the query, correlated on the relationship's
// own foreign key — the same correlation Has already uses, rendered as a
// real join instead of a correlated EXISTS.
//
//	Users.With(db).Join(Users.Posts).Where(Posts.Title.Contains("go")).All(ctx)
//	// SELECT "users".* FROM "users" JOIN "posts" ON "posts"."author_id" = "users"."id"
//	// WHERE "posts"."title" LIKE $1
//
// A many to many relationship is rejected: it needs two joins through a
// join table, which multiplies rows the way a join always does, and
// Has/HasNone already answer the question a many to many join usually
// means to ask, without that multiplication.
//
// Select stays scoped to this query's own row type, so reading a joined
// table's columns back needs SelectAs, not Select. Where, OrderBy and
// GroupBy all accept a joined column, the same way Where already accepts
// one after this call: Join only widens which tables a column may belong
// to, not which clauses may name one.
func (f *Filtered[E]) Join(rel Relationship) *Filtered[E] { return f.join(rel, joinInner, nil) }

// LeftJoin is Join, keeping a primary row that has no matching related row
// rather than dropping it.
//
//	Users.With(db).LeftJoin(Users.Posts).Where(Posts.ID.IsNull()).All(ctx)  // users with no posts
func (f *Filtered[E]) LeftJoin(rel Relationship) *Filtered[E] { return f.join(rel, joinLeft, nil) }

// JoinOn is Join, ANDing extra conditions onto the ON clause rather than
// the WHERE clause.
//
// The difference matters most for a left join, which is what LeftJoinOn is
// for: a condition on the joined table added to WHERE is checked after the
// join runs, so it also drops every primary row that had no matching
// related row at all — silently turning the left join back into an inner
// one. A condition ANDed onto ON instead is checked as part of matching
// related rows, so a primary row with none still comes back, with the
// related columns NULL.
func (f *Filtered[E]) JoinOn(rel Relationship, preds ...Predicate) *Filtered[E] {
	return f.join(rel, joinInner, preds)
}

// LeftJoinOn is LeftJoin, ANDing extra conditions onto the ON clause rather
// than the WHERE clause — JoinOn's own reasoning applied to the join kind
// that reasoning is actually about: a LeftJoin's whole point is a primary
// row that keeps coming back with no matching related row, and a condition
// on the joined table checked as part of the join, rather than after it,
// is what keeps that true.
func (f *Filtered[E]) LeftJoinOn(rel Relationship, preds ...Predicate) *Filtered[E] {
	return f.join(rel, joinLeft, preds)
}

func (f *Filtered[E]) join(rel Relationship, kind joinKind, extra []Predicate) *Filtered[E] {
	out := f.clone()
	if err := f.noDerived("Join"); err != nil {
		out.fail(err)
		return out
	}
	if rel == nil {
		out.fail(fmt.Errorf("orm: table %q: Join was given no relationship", f.tableName()))
		return out
	}
	out.joins = append(out.joins, joinSpec{
		kind:  kind,
		rel:   rel.relationOf(),
		extra: append([]Predicate(nil), extra...),
	})
	return out
}

// Join is Filtered.Join, off an unfiltered query.
func (q *Query[E]) Join(rel Relationship) *Filtered[E] { return q.filtered().Join(rel) }

// LeftJoin is Filtered.LeftJoin, off an unfiltered query.
func (q *Query[E]) LeftJoin(rel Relationship) *Filtered[E] { return q.filtered().LeftJoin(rel) }

// JoinOn is Filtered.JoinOn, off an unfiltered query.
func (q *Query[E]) JoinOn(rel Relationship, preds ...Predicate) *Filtered[E] {
	return q.filtered().JoinOn(rel, preds...)
}

// LeftJoinOn is Filtered.LeftJoinOn, off an unfiltered query.
func (q *Query[E]) LeftJoinOn(rel Relationship, preds ...Predicate) *Filtered[E] {
	return q.filtered().LeftJoinOn(rel, preds...)
}
