// Package ast defines the syntax tree for .tork schema files. Every node
// carries the source span it was parsed from, because two consumers with
// opposite needs share this one tree: the analyzer walks it to build the
// semantic schema and needs spans only for diagnostics, while the
// language server needs to answer "what is under the cursor" and "where
// was this declared", which is nothing but span arithmetic.
//
// The sum types (Decl, Expr) follow the same convention as orm.Predicate
// and migrate.Operation: an unexported marker method plus one struct per
// variant, so the compiler enumerates the variants and a new one cannot
// be added without every type switch noticing.
//
// Comments are kept, not discarded, since the formatter must reproduce
// them. A comment ends up in exactly one of three places: the Doc group
// of the declaration or member directly below it, the Trailing comment
// of the member on the same line, or the Floating list of the enclosing
// block when it is separated from what follows by a blank line. Floating
// comments keep their spans, so the formatter can interleave them with
// members by source position.
package ast
