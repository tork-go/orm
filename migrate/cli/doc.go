// Package cli is Tork ORM's migration command dispatcher: makemigrations,
// migrate up, migrate down, and history. It has no executable of its own;
// a small main.go in your own project calls Run (or RunWithArgs), passing
// in your driver dialect, connection string, and models:
//
//	func main() {
//	    os.Exit(cli.Run(postgres.Dialect{}, os.Getenv("DATABASE_URL"), "migrations",
//	        models.User, models.Post))
//	}
//
// Go cannot import an arbitrary package at runtime, so this is how your
// models get linked into a working CLI: compiling that file gives you a
// private binary with every subcommand fully built in.
package cli
