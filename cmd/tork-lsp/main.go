// Command tork-lsp is the Language Server Protocol server for .tork
// schema files. Editors start it themselves and speak the protocol
// over its standard input and output, so it takes no arguments and
// prints nothing a human is meant to read.
//
// Install it with:
//
//	go install github.com/tork-go/orm/cmd/tork-lsp@latest
//
// It is the one binary this module ships. The generator deliberately
// has none, since an application wires that into its own cmd/generate;
// an editor cannot do the same, because it has only a path to execute.
package main

import (
	"os"

	"github.com/tork-go/orm/lsp"
)

func main() {
	os.Exit(lsp.Run(os.Stdin, os.Stdout, os.Stderr))
}
