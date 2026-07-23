// Package diag defines the one diagnostic type every stage of the .tork
// toolchain reports through: a file name, a source span, a severity, and
// a human message. The lexer, parser, analyzer, and language server all
// speak this type, which is what lets the CLI print a single ordered
// report for a whole schema directory and the language server forward the
// same findings as editor squiggles, with no translation layer between
// the stages.
package diag
