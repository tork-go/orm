// Package format prints a parsed .tork file back as canonical source.
// It is what `tork fmt` writes and what the language server returns for
// a format request, so the two can never disagree about what canonical
// means.
//
// Formatting is idempotent: formatting already formatted source
// returns it unchanged, which is the property that makes format on
// save safe. Every comment survives, in the same relation to the
// declaration it was written against, and blank lines the author put
// between members are kept (collapsed to one), because those are the
// paragraph breaks that make a hundred field model readable.
//
// Only well formed files are formatted. Source with syntax errors is
// returned untouched by Source, since reprinting a partial tree would
// silently delete whatever the parser could not make sense of.
package format
