package orm

import "strings"

// Predicate is one boolean condition in a WHERE clause.
//
// Predicates are pure data: building one touches no database, renders no
// SQL, and cannot fail. That is deliberate. It keeps the model layer free
// of dialect knowledge, lets tests assert on the built structure rather
// than on a string, and leaves a future driver free to compile the tree
// itself if its syntax diverges too far for the shared compiler.
//
// The implementations are one concrete type per shape rather than a single
// struct with a kind tag and a union of optional fields, matching the way
// migrate models its operations, and for the same reason: a Comparison
// cannot be handed a value that only an InList would read.
//
// Predicates are built by the operation mixins on the column types, never
// by hand, so Users.ID.GreaterThan(100) rather than Comparison{...}. The
// fields are exported so tests and compilers can read them, not so callers
// construct them.
type Predicate interface {
	predicate()
}

// Operator is a binary comparison.
type Operator int

const (
	OpEquals Operator = iota
	OpNotEquals
	OpGreaterThan
	OpGreaterOrEqual
	OpLessThan
	OpLessOrEqual
)

// String returns the SQL spelling of the operator. Every dialect Tork
// targets spells these six identically, so this lives here rather than on
// the dialect.
func (o Operator) String() string {
	switch o {
	case OpEquals:
		return "="
	case OpNotEquals:
		return "<>"
	case OpGreaterThan:
		return ">"
	case OpGreaterOrEqual:
		return ">="
	case OpLessThan:
		return "<"
	case OpLessOrEqual:
		return "<="
	}
	return "?"
}

// Conjunction joins a group of predicates.
type Conjunction int

const (
	ConjAnd Conjunction = iota
	ConjOr
)

// Comparison is `col <op> value`.
type Comparison struct {
	Col   ColumnMeta
	Op    Operator
	Value any
}

// InList is `col IN (values...)`, or NOT IN when Not is set.
type InList struct {
	Col    ColumnMeta
	Values []any
	Not    bool
}

// Range is `col BETWEEN lo AND hi`, or NOT BETWEEN when Not is set.
// BETWEEN is inclusive at both ends in every dialect Tork targets.
type Range struct {
	Col    ColumnMeta
	Lo, Hi any
	Not    bool
}

// Pattern is `col LIKE value`, or ILIKE when CaseInsensitive is set.
//
// Value is always a complete LIKE pattern, already escaped. The
// convenience constructors (Contains, StartsWith, EndsWith) escape the
// caller's substring and wrap it in wildcards before storing it here,
// while Like stores the caller's pattern untouched, because there the
// wildcards are the point. Escaping at construction rather than at compile
// time keeps the meaning of this field unambiguous no matter which
// constructor produced it.
type Pattern struct {
	Col             ColumnMeta
	Value           string
	CaseInsensitive bool
	Not             bool
}

// Nullness is `col IS NULL`, or IS NOT NULL when Not is set.
type Nullness struct {
	Col ColumnMeta
	Not bool
}

// Group is a parenthesised list of predicates joined by Conj.
type Group struct {
	Conj  Conjunction
	Preds []Predicate
}

// Negation is `NOT (pred)`.
type Negation struct {
	Pred Predicate
}

// Existence is `EXISTS (...)` over a relationship, or NOT EXISTS when Not is
// set. Build one with Has or HasNone.
type Existence struct {
	Rel   *relation
	Preds []Predicate
	Not   bool
}

// InSubquery is `col IN (SELECT ...)`, or NOT IN when Not is set. Build one
// with a column's InQuery or NotInQuery.
type InSubquery struct {
	Col ColumnMeta
	Sub subquerySource
	Not bool
}

// JSONHasKey is "the document has this top-level key". Build one with a JSON
// column's HasKey.
type JSONHasKey struct {
	Col ColumnMeta
	Key string
}

// JSONContains is "the document contains this one, as a subtree". Build one
// with a JSON column's Contains. Value is the Go value to test containment of,
// encoded through the column's own codec exactly as a stored document is.
type JSONContains struct {
	Col   ColumnMeta
	Value any
}

// JSONKey is `(col ->> key) <op> value`: the text at a top-level key compared
// against a value. Build one with a JSON column's Key(...).Equals or
// .NotEquals. The value is text because ->> extracts text; a number or a
// nested object is what Contains and Raw are for.
type JSONKey struct {
	Col   ColumnMeta
	Key   string
	Op    Operator
	Value string
}

// ArrayContains is "the array holds all of these elements". Build one with an
// array column's Has (one element) or HasAll (several). An empty list is
// "holds all of nothing", which every array does.
//
// Elems is a typed slice ([]int, []string, ...), bound whole as one array
// parameter rather than element by element: an ARRAY[$1, $2] constructor
// leaves its element type for the database to infer, and it infers text, so
// an int array's containment would compile to integer[] @> text[]. Binding
// the slice lets the driver encode it as the array type the column is.
type ArrayContains struct {
	Col   ColumnMeta
	Elems any
}

// ArrayOverlaps is "the array holds any of these elements". Build one with an
// array column's HasAny. An empty list overlaps nothing.
type ArrayOverlaps struct {
	Col   ColumnMeta
	Elems any
}

// ArrayLength is `len(col) <op> value`: the number of elements compared
// against a count. Build one with an array column's Len().GreaterThan and
// the rest.
type ArrayLength struct {
	Col   ColumnMeta
	Op    Operator
	Value int
}

// FullText is a full-text search: the column's text matches the query. Build
// one with a string column's Matches.
type FullText struct {
	Col   ColumnMeta
	Query string
}

// subquerySource is something that renders as a single-column SELECT inside
// another statement. Only this package's own query values satisfy it.
type subquerySource interface {
	compileWithin(outer *compiler) (string, error)
}

// Relationship is a relationship marker: HasMany, HasOne, BelongsTo or
// ManyToMany.
//
// It is what Has and HasNone accept, and nothing outside this package can
// satisfy it. A narrowed relationship is deliberately not one: Has takes its
// conditions directly, so a Limit or an OrderBy would have nothing to mean.
type Relationship interface {
	relationOf() *relation
}

// Has matches rows that have at least one related row, optionally narrowed by
// conditions on it.
//
//	Users.With(db).Where(orm.Has(Users.Posts)).All(ctx)
//	Users.With(db).Where(orm.Has(Users.Posts, Posts.Published.Equals(true))).All(ctx)
//
// It compiles to an EXISTS over the related table, correlated on the columns
// the relationship joins:
//
//	WHERE EXISTS (
//	    SELECT 1 FROM "posts"
//	    WHERE "posts"."author_id" = "users"."id" AND "posts"."published" = $1
//	)
//
// Being an ordinary predicate, it goes anywhere one goes: beside other
// conditions, inside Or and Not, and in front of a write. It nests, too, so
// the conditions may themselves ask about a relationship of the related rows.
//
// It answers a different question from Load, and the two do not imply each
// other: filtering by a published post and loading the published ones are
// separate requests, and a caller wanting one without the other should not
// have to unpick the pair.
func Has(rel Relationship, preds ...Predicate) Predicate {
	return existence(rel, preds, false)
}

// HasNone matches rows that have no related row, or none matching the
// conditions given.
//
//	Users.With(db).Where(orm.HasNone(Users.Posts)).All(ctx)
//	Users.With(db).Where(orm.HasNone(Users.Posts, Posts.Published.Equals(true))).All(ctx)
//
// The second says the user has no published post, which is not the same as
// having no posts: a user with only drafts matches it.
//
// Coming from Prisma, Has is some and this is none. There is no every, which
// would be this with the condition negated: HasNone(Users.Posts,
// orm.Not(Posts.Published.Equals(true))) matches users all of whose posts are
// published, and, like Prisma's every, also those with no posts at all.
func HasNone(rel Relationship, preds ...Predicate) Predicate {
	return existence(rel, preds, true)
}

// existence builds the predicate both spellings share. A nil relationship is
// carried rather than rejected, so it is reported when the statement compiles
// like every other mistake in a predicate, rather than by panicking here.
func existence(rel Relationship, preds []Predicate, not bool) Predicate {
	e := Existence{Preds: append([]Predicate(nil), preds...), Not: not}
	if rel != nil {
		e.Rel = rel.relationOf()
	}
	return e
}

func (Comparison) predicate()    {}
func (InList) predicate()        {}
func (Range) predicate()         {}
func (Pattern) predicate()       {}
func (Nullness) predicate()      {}
func (Group) predicate()         {}
func (Negation) predicate()      {}
func (Existence) predicate()     {}
func (InSubquery) predicate()    {}
func (JSONHasKey) predicate()    {}
func (JSONContains) predicate()  {}
func (JSONKey) predicate()       {}
func (ArrayContains) predicate() {}
func (ArrayOverlaps) predicate() {}
func (ArrayLength) predicate()   {}
func (FullText) predicate()      {}

// And joins preds with AND.
//
// A single predicate is returned unwrapped rather than in a one-element
// group, so the common cases produce no redundant parentheses. Calling it
// with no predicates yields an empty group, which compiles to AND's
// identity, TRUE; Or's empty identity is FALSE. Passing zero predicates is
// almost always a bug in caller code that filtered a slice down to
// nothing, so both cases are defined rather than left to chance.
func And(preds ...Predicate) Predicate {
	if len(preds) == 1 {
		return preds[0]
	}
	return Group{Conj: ConjAnd, Preds: preds}
}

// Or joins preds with OR. See And for the one- and zero-predicate cases.
func Or(preds ...Predicate) Predicate {
	if len(preds) == 1 {
		return preds[0]
	}
	return Group{Conj: ConjOr, Preds: preds}
}

// Not negates p.
func Not(p Predicate) Predicate {
	return Negation{Pred: p}
}

// Ordering is one term of an ORDER BY clause: a column, or a computed
// expression, sorted one way or the other.
//
// expr is set instead of Col by an expression's own Asc and Desc, and is
// nil for every ordering a column produces, which is the ordinary case and
// the only one that existed before expressions did. It is unexported so
// that Ordering{Col: ..., Desc: ...} keeps meaning exactly what it did.
//
// Not every consumer can take one. Cursor paging reads the ordering
// columns back out of a row by name to build its seek predicate, and a
// computed value has no field to read, so it rejects an expression
// ordering rather than seeking on nothing; see Filtered.Cursor.
type Ordering struct {
	Col  ColumnMeta
	Desc bool
	expr expression
}

// Assignment is one `col = value` term of an UPDATE's SET clause.
//
// Expr is set instead of Value by Increment, Decrement and SetExpr (see
// query_expr.go), rendering `col = col <op> ...` rather than binding a
// literal. It is nil for every assignment Set/SetPtr/SetNull produce,
// which is the ordinary case and the only one that existed before Expr did.
//
// It holds the unexported expression interface rather than a concrete
// Expr[T], for the reason InSubquery.Sub holds subquerySource: the field is
// readable by anyone but satisfiable only by this package, and Assignment
// itself is not parameterised by the column's type.
type Assignment struct {
	Col   ColumnMeta
	Value any
	Expr  expression
}

// escapeLike neutralises the three characters LIKE treats specially, so a
// caller's substring matches literally. The escape character is backslash,
// which every dialect Tork targets accepts, and which the compiler states
// explicitly with an ESCAPE clause rather than relying on it being the
// default.
func escapeLike(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\', '%', '_':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
