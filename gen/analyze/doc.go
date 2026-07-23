// Package analyze turns parsed .tork files into one validated semantic
// schema. The parser knows only shape; this package knows meaning: it
// merges every file of a schema directory into a single namespace,
// resolves type references across files, checks each attribute against
// what the ORM can actually express, and reports everything wrong as
// diagnostics rather than stopping at the first finding.
//
// The output Schema is the sole input to code generation, and every
// node in it keeps a pointer back to the syntax it came from, which is
// what lets the language server answer hover and go to definition
// questions from the same analysis the generator runs.
//
// Analysis never fails and never panics on any tree the parser can
// produce, including the partial trees behind syntax errors; constructs
// the parser already reported are skipped in silence here, so one
// mistake is one diagnostic.
package analyze
