// Package parser turns .tork schema source into syntax. Scan tokenizes
// one file; the parser built on top of it produces the ast package's
// declarations.
//
// Everything in this package is error tolerant by construction: entry
// points return a partial result plus diagnostics instead of failing on
// the first mistake. The same code path serves the generator, where any
// error aborts the run before files are written, and the language
// server, where errors are the normal state because the user is mid
// keystroke. Building tolerance in later, once a fail fast parser
// exists, is a rewrite; building it first costs almost nothing.
package parser
