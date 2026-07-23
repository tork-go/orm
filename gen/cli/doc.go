// Package cli is the generator's command line surface: it reads a
// directory of .tork files, generates Go models from them, and reports
// what it did. Like migrate/cli it ships no binary of its own. The
// application writes a three line main that names its own directories,
// which is the whole of the configuration story:
//
//	package main
//
//	import (
//	    "os"
//
//	    gencli "github.com/tork-go/orm/gen/cli"
//	)
//
//	func main() {
//	    os.Exit(gencli.Run(gencli.Config{
//	        SchemaDir: "schema",
//	        OutDir:    "models",
//	    }))
//	}
//
// and then runs `go run ./cmd/generate`. There is no config file to
// discover, parse, or keep in step with the Go build, for the same
// reason the migration tool has none: the program that already knows
// where its packages live is the natural place to say so.
package cli
