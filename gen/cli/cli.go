package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const usage = `usage:
  generate    read the schema and write the Go models (the default)
  check       report schema problems without writing anything
  fmt [-l]    rewrite the schema files in canonical form
`

// Config says where a project keeps its schema and where generated
// code goes. Every field has a working default, so the common project
// needs to set none of them.
type Config struct {
	// SchemaDir holds the .tork files, all of which together form one
	// schema. Defaults to "schema".
	SchemaDir string
	// OutDir receives the generated .go files. Defaults to "models".
	OutDir string
	// Package names the generated package. Defaults to the last
	// element of OutDir, which is the Go convention anyway.
	Package string
}

// withDefaults fills the blanks, so the rest of the package can read
// the config without repeating the fallbacks.
func (c Config) withDefaults() Config {
	if c.SchemaDir == "" {
		c.SchemaDir = "schema"
	}
	if c.OutDir == "" {
		c.OutDir = "models"
	}
	if c.Package == "" {
		c.Package = filepath.Base(c.OutDir)
	}
	return c
}

// Run parses os.Args[1:] and runs the requested subcommand, returning
// a process exit code suitable for os.Exit: 0 on success, 1 when the
// schema is bad or the filesystem refuses, and 2 for a usage mistake.
//
// No arguments means generate, so `go run ./cmd/generate` does the
// obvious thing.
func Run(cfg Config) int {
	return RunWithArgs(os.Args[1:], os.Stdout, os.Stderr, cfg)
}

// RunWithArgs is Run with its arguments and output streams explicit,
// letting callers (including this package's own tests) drive it
// without touching the real process's os.Args or stdio.
func RunWithArgs(args []string, out, errOut io.Writer, cfg Config) int {
	cfg = cfg.withDefaults()
	command := "generate"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "generate":
		if len(args) > 0 {
			fmt.Fprint(errOut, usage)
			return 2
		}
		return runGenerate(out, errOut, cfg)
	case "check":
		if len(args) > 0 {
			fmt.Fprint(errOut, usage)
			return 2
		}
		return runCheck(out, errOut, cfg)
	case "fmt":
		return runFmt(args, out, errOut, cfg)
	default:
		fmt.Fprint(errOut, usage)
		return 2
	}
}

func runFmt(args []string, out, errOut io.Writer, cfg Config) int {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	fs.SetOutput(errOut)
	list := fs.Bool("l", false, "list the files that are not canonical instead of rewriting them")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprint(errOut, usage)
		return 2
	}
	return runFormat(out, errOut, cfg, *list)
}
