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
// by hand, so Users.ID.Gt(100) rather than Comparison{...}. The fields are
// exported so tests and compilers can read them, not so callers construct
// them.
type Predicate interface {
	predicate()
}

// Operator is a binary comparison.
type Operator int

const (
	OpEq Operator = iota
	OpNotEq
	OpGt
	OpGte
	OpLt
	OpLte
)

// String returns the SQL spelling of the operator. Every dialect Tork
// targets spells these six identically, so this lives here rather than on
// the dialect.
func (o Operator) String() string {
	switch o {
	case OpEq:
		return "="
	case OpNotEq:
		return "<>"
	case OpGt:
		return ">"
	case OpGte:
		return ">="
	case OpLt:
		return "<"
	case OpLte:
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
//	Users.With(db).Where(orm.Has(Users.Posts, Posts.Published.Eq(true))).All(ctx)
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
//	Users.With(db).Where(orm.HasNone(Users.Posts, Posts.Published.Eq(true))).All(ctx)
//
// The second says the user has no published post, which is not the same as
// having no posts: a user with only drafts matches it.
//
// Coming from Prisma, Has is some and this is none. There is no every, which
// would be this with the condition negated: HasNone(Users.Posts,
// orm.Not(Posts.Published.Eq(true))) matches users all of whose posts are
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

func (Comparison) predicate() {}
func (InList) predicate()     {}
func (Range) predicate()      {}
func (Pattern) predicate()    {}
func (Nullness) predicate()   {}
func (Group) predicate()      {}
func (Negation) predicate()   {}
func (Existence) predicate()  {}

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

// Ordering is one term of an ORDER BY clause.
type Ordering struct {
	Col  ColumnMeta
	Desc bool
}

// Assignment is one `col = value` term of an UPDATE's SET clause.
type Assignment struct {
	Col   ColumnMeta
	Value any
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
