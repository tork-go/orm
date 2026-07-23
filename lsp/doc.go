// Package lsp is a Language Server Protocol server for .tork schema
// files: diagnostics as you type, completion, hover, go to definition
// across files, and formatting.
//
// It answers every question from the same lexer, parser, analyzer, and
// formatter the generator uses, so the editor and `go run
// ./cmd/generate` can never disagree about what a schema means. That
// sharing is why the parser recovers from errors instead of stopping
// at the first one: an editor sees broken source on nearly every
// keystroke, and a server that gave up on it would go blind exactly
// when help is most wanted.
//
// The transport is JSON-RPC 2.0 over stdio with Content-Length framing,
// written against encoding/json rather than pulled in as a dependency,
// which keeps this package as stdlib only as the rest of the module.
// Requests are handled one at a time: analyzing a schema directory
// takes microseconds, so a work queue would buy nothing and cost the
// clarity of a straight line.
package lsp
