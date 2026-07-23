// Package codegen renders an analyzed schema into Go source: one file
// per model in the exact handwritten idiom the ORM documents, an enum
// constants file, and a registry exposing AllModels for migration
// wiring. It is a pure function from Schema to bytes; the filesystem
// belongs to the CLI layer, which is what keeps generation trivially
// testable against golden files.
//
// Output is deterministic to the byte: models, enums, imports, and the
// registry all render in name order, so reorganizing schema files or
// rerunning the generator never dirties a checked in diff.
//
// Everything emitted is formatted through go/format before it leaves
// the package. A formatting failure is not a user error, it is a bug
// in the printer, and Generate says so in the error it returns.
package codegen
