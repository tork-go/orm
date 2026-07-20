// Package schema holds a dialect-agnostic, in-memory representation of a
// database schema, plus the logic to build one from Go model definitions
// (ExtractSchema). A driver package builds the same representation from a
// live database (introspection), so the two can be structurally diffed.
//
// This package knows nothing about SQL or any specific database. It
// imports the orm package to read model metadata, but never a driver.
package schema
