package orm

import "fmt"

// joinKind distinguishes the four joins by which side's unmatched rows they
// keep: neither, the left, the right, or both.
type joinKind int

const (
	joinInner joinKind = iota
	joinLeft
	joinRight
	joinFull
)

// keyword returns the SQL each kind is written with. OUTER is left out of
// the two that allow it, since it is noise in every dialect: LEFT JOIN and
// LEFT OUTER JOIN are the same join, and writing one word fewer keeps the
// generated SQL readable.
func (k joinKind) keyword() string {
	switch k {
	case joinLeft:
		return " LEFT JOIN "
	case joinRight:
		return " RIGHT JOIN "
	case joinFull:
		return " FULL JOIN "
	}
	return " JOIN "
}

// keepsUnmatchedJoinedRows reports whether the kind can hand back a row the
// primary table has no match for, which is what makes a read into *E
// impossible. See Filtered.RightJoinTo.
func (k joinKind) keepsUnmatchedJoinedRows() bool {
	return k == joinRight || k == joinFull
}

// joinSpec is one join call: what is joined, which kind, and any extra
// conditions ANDed onto the ON clause.
//
// What is joined is one of two things. A relationship supplies the ON clause
// itself, from the foreign key it resolves to, and alias names the table the
// far side is rendered under when the caller gave one. A target table
// supplies no ON clause of its own, so extra is the whole of it; see
// Filtered.JoinTo.
type joinSpec struct {
	kind  joinKind
	rel   *relation
	alias *tableState
	to    *tableState
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
func (f *Filtered[E]) Join(rel Relationship) *Filtered[E] { return f.join(rel, nil, joinInner, nil) }

// LeftJoin is Join, keeping a primary row that has no matching related row
// rather than dropping it.
//
//	Users.With(db).LeftJoin(Users.Posts).Where(Posts.ID.IsNull()).All(ctx)  // users with no posts
func (f *Filtered[E]) LeftJoin(rel Relationship) *Filtered[E] {
	return f.join(rel, nil, joinLeft, nil)
}

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
	return f.join(rel, nil, joinInner, preds)
}

// LeftJoinOn is LeftJoin, ANDing extra conditions onto the ON clause rather
// than the WHERE clause — JoinOn's own reasoning applied to the join kind
// that reasoning is actually about: a LeftJoin's whole point is a primary
// row that keeps coming back with no matching related row, and a condition
// on the joined table checked as part of the join, rather than after it,
// is what keeps that true.
func (f *Filtered[E]) LeftJoinOn(rel Relationship, preds ...Predicate) *Filtered[E] {
	return f.join(rel, nil, joinLeft, preds)
}

// JoinAs is Join, rendering the joined table under a name of its own.
//
//	mgr := orm.Alias(Employees, "mgr")
//	Employees.With(db).JoinAs(Employees.Manager, mgr).Where(mgr.Name.Equals("Ada"))
//
//	// SELECT "employees".* FROM "employees"
//	// JOIN "employees" AS "mgr" ON "mgr"."id" = "employees"."manager_id"
//	// WHERE "mgr"."name" = $1
//
// A self join is what needs this: a relationship whose far side is the
// declaring table itself would otherwise name that table twice in one
// statement, which no database will resolve. It is equally the answer for a
// table joined twice for two different reasons.
//
// The relationship still supplies the keys the join matches on. The alias
// supplies only the name the far side is rendered under, and therefore which
// columns the caller writes conditions against: mgr's, not the stored
// table's. It must be an alias of the table the relationship names; an alias
// of some other table is reported rather than joined.
//
// See orm.Alias for what an alias is and what it cannot do.
func (f *Filtered[E]) JoinAs(rel Relationship, alias Model) *Filtered[E] {
	return f.joinAs(rel, alias, joinInner, nil)
}

// LeftJoinAs is JoinAs, keeping a primary row that has no matching related
// row — LeftJoin's own difference from Join.
//
//	orm.SelectAs[Pair](
//	    Employees.With(db).LeftJoinAs(Employees.Manager, mgr),
//	    Employees.Name, mgr.Name,
//	)
//	// every employee, with their manager's name where there is one and NULL
//	// where there is not
func (f *Filtered[E]) LeftJoinAs(rel Relationship, alias Model) *Filtered[E] {
	return f.joinAs(rel, alias, joinLeft, nil)
}

// JoinOnAs is JoinAs, ANDing extra conditions onto the ON clause rather than
// the WHERE clause — JoinOn's own difference from Join.
func (f *Filtered[E]) JoinOnAs(rel Relationship, alias Model, preds ...Predicate) *Filtered[E] {
	return f.joinAs(rel, alias, joinInner, preds)
}

// LeftJoinOnAs is LeftJoinAs, ANDing extra conditions onto the ON clause
// rather than the WHERE clause, which is the pairing JoinOn's own
// documentation is about: a condition on the joined table checked as part of
// matching related rows is what keeps a left join a left join.
//
//	orm.SelectAs[Pair](
//	    Employees.With(db).LeftJoinOnAs(Employees.Manager, mgr, mgr.Active.Equals(true)),
//	    Employees.Name, mgr.Name,
//	)
//	// every employee still comes back, matched only against an active
//	// manager; the same condition in a Where would drop the rest
func (f *Filtered[E]) LeftJoinOnAs(rel Relationship, alias Model, preds ...Predicate) *Filtered[E] {
	return f.joinAs(rel, alias, joinLeft, preds)
}

// JoinTo joins a table this one declares no relationship to, on conditions
// written out in full.
//
//	Users.With(db).
//	    JoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)).
//	    Where(Logins.Failed.Equals(true)).
//	    All(ctx)
//
//	// SELECT "users".* FROM "users"
//	// JOIN "logins" ON "logins"."user_id" = "users"."id"
//	// WHERE "logins"."failed" = $1
//
// Join and its siblings take a relationship, which supplies the keys to
// match on. This takes the conditions instead, for the tables between which
// no relationship is declared: a log table nobody wanted a foreign key on, a
// join on something other than a key, or a table whose relationship exists
// but is not the one this statement means to follow.
//
// Comparing two columns needs no vocabulary of its own: Value lifts a column
// into an expression, whose comparisons accept a column as readily as a
// literal.
//
// The conditions are the whole of the ON clause, so at least one is
// required. A join with none is a cross join, which pairs every row with
// every other, and this package does not offer one by omission.
//
// The table may be an alias, which is what joining the same table twice
// needs; see orm.Alias.
func (f *Filtered[E]) JoinTo(table Model, preds ...Predicate) *Filtered[E] {
	return f.joinTo(table, joinInner, preds)
}

// LeftJoinTo is JoinTo, keeping a primary row with no matching row on the
// joined table.
func (f *Filtered[E]) LeftJoinTo(table Model, preds ...Predicate) *Filtered[E] {
	return f.joinTo(table, joinLeft, preds)
}

// RightJoinTo is JoinTo the other way round: it keeps a row of the joined
// table that this one has no match for, leaving this table's columns NULL
// for it.
//
//	orm.SelectAs[Row](
//	    Users.With(db).RightJoinTo(Logins, Logins.UserID.Value().Equals(Users.ID)),
//	    Logins.ID, Users.Username,
//	)
//	// every login, with the username where there is one
//
// It reads only through SelectAs. The rows a Filtered read scans into are
// *E, and an unmatched row has no values for E's fields at all — not a zero
// value but no row — so All and First refuse a query carrying one rather
// than handing back a slice of blanks. Choosing a type that can hold the
// absence is SelectAs's job, with a pointer field for each column of this
// table.
//
// There is no RightJoin over a relationship. A relationship is declared on
// this table and read outwards, so a join that keeps the far side's
// unmatched rows reads backwards through it; writing the condition out says
// plainly which side is being kept.
func (f *Filtered[E]) RightJoinTo(table Model, preds ...Predicate) *Filtered[E] {
	return f.joinTo(table, joinRight, preds)
}

// FullJoinTo keeps the unmatched rows of both tables, with the other side's
// columns NULL for each.
//
// Like RightJoinTo it reads only through SelectAs, and for the same reason.
func (f *Filtered[E]) FullJoinTo(table Model, preds ...Predicate) *Filtered[E] {
	return f.joinTo(table, joinFull, preds)
}

func (f *Filtered[E]) join(rel Relationship, alias *tableState, kind joinKind, extra []Predicate) *Filtered[E] {
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
		alias: alias,
		extra: append([]Predicate(nil), extra...),
	})
	return out
}

// joinAs is join, resolving the model the caller named the far side with.
//
// The alias is checked for being an alias at all here, where the call that
// went wrong can be named, rather than in the compiler, which sees only a
// table state and could not tell JoinAs's mistake from Join's.
func (f *Filtered[E]) joinAs(rel Relationship, alias Model, kind joinKind, extra []Predicate) *Filtered[E] {
	st, err := aliasState(f.tableName(), "JoinAs", alias)
	if err != nil {
		out := f.clone()
		out.fail(err)
		return out
	}
	return f.join(rel, st, kind, extra)
}

// joinTo records a join onto a table named directly, with the caller's
// conditions as the whole ON clause.
func (f *Filtered[E]) joinTo(table Model, kind joinKind, preds []Predicate) *Filtered[E] {
	out := f.clone()
	if err := f.noDerived("JoinTo"); err != nil {
		out.fail(err)
		return out
	}
	st, err := joinTargetState(f.tableName(), table)
	if err != nil {
		out.fail(err)
		return out
	}
	if len(preds) == 0 {
		out.fail(fmt.Errorf("orm: table %q: JoinTo was given no conditions to join %q on; "+
			"a join with none is a cross join, which this package does not offer",
			f.tableName(), st.name))
		return out
	}
	out.joins = append(out.joins, joinSpec{
		kind:  kind,
		to:    st,
		extra: append([]Predicate(nil), preds...),
	})
	return out
}

// aliasState is the table state behind a model given where an alias is
// required.
func aliasState(table, op string, m Model) (*tableState, error) {
	st, err := joinTargetState(table, m)
	if err != nil {
		return nil, err
	}
	if st.aliasOf == "" {
		return nil, fmt.Errorf("orm: table %q: %s was given table %q under its own name; "+
			"it takes an alias, which orm.Alias makes", table, op, st.name)
	}
	return st, nil
}

// joinTargetState is the table state behind a model given as something to
// join onto, whatever the join form.
//
// A derived model is allowed, and joins as its bare name. That is what a
// recursive step's reference to the table being defined is: inside the
// recursion the name is in scope and holds every row found so far, so
// joining it is the whole point. Outside one it is a name the statement
// never defined, which the database reports — this package cannot tell the
// two apart from the model alone, and refusing both would cost the feature
// to keep an error message. See DerivedTable.Recursive.
func joinTargetState(table string, m Model) (*tableState, error) {
	if m == nil {
		return nil, fmt.Errorf("orm: table %q: this join was given no table", table)
	}
	st := stateOf(m)
	if st == nil {
		return nil, fmt.Errorf("orm: table %q: %T carries no table identity; declare it "+
			"with DefineTable", table, m)
	}
	return st, nil
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

// JoinAs is Filtered.JoinAs, off an unfiltered query.
func (q *Query[E]) JoinAs(rel Relationship, alias Model) *Filtered[E] {
	return q.filtered().JoinAs(rel, alias)
}

// LeftJoinAs is Filtered.LeftJoinAs, off an unfiltered query.
func (q *Query[E]) LeftJoinAs(rel Relationship, alias Model) *Filtered[E] {
	return q.filtered().LeftJoinAs(rel, alias)
}

// JoinOnAs is Filtered.JoinOnAs, off an unfiltered query.
func (q *Query[E]) JoinOnAs(rel Relationship, alias Model, preds ...Predicate) *Filtered[E] {
	return q.filtered().JoinOnAs(rel, alias, preds...)
}

// LeftJoinOnAs is Filtered.LeftJoinOnAs, off an unfiltered query.
func (q *Query[E]) LeftJoinOnAs(rel Relationship, alias Model, preds ...Predicate) *Filtered[E] {
	return q.filtered().LeftJoinOnAs(rel, alias, preds...)
}

// JoinTo is Filtered.JoinTo, off an unfiltered query.
func (q *Query[E]) JoinTo(table Model, preds ...Predicate) *Filtered[E] {
	return q.filtered().JoinTo(table, preds...)
}

// LeftJoinTo is Filtered.LeftJoinTo, off an unfiltered query.
func (q *Query[E]) LeftJoinTo(table Model, preds ...Predicate) *Filtered[E] {
	return q.filtered().LeftJoinTo(table, preds...)
}

// RightJoinTo is Filtered.RightJoinTo, off an unfiltered query.
func (q *Query[E]) RightJoinTo(table Model, preds ...Predicate) *Filtered[E] {
	return q.filtered().RightJoinTo(table, preds...)
}

// FullJoinTo is Filtered.FullJoinTo, off an unfiltered query.
func (q *Query[E]) FullJoinTo(table Model, preds ...Predicate) *Filtered[E] {
	return q.filtered().FullJoinTo(table, preds...)
}
