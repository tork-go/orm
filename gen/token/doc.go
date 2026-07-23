// Package token defines the lexical vocabulary of .tork schema files:
// token kinds, source positions, and spans. It sits at the bottom of the
// gen package family so that every later stage (the parser, the analyzer,
// the formatter, the language server) can say "this construct, at this
// place in this file" without importing anything heavier than positions.
package token
