# Tork schema for VS Code

Schema language support for `.tork` files: syntax highlighting, live
diagnostics, completion, hover, go to definition across files, and
formatting.

Everything but the highlighting comes from `tork-lsp`, which answers
from the same parser and analyzer the generator uses. What the editor
tells you and what `go run ./cmd/generate` tells you are therefore the
same findings, reported in the same words.

## Installing

```
go install github.com/tork-go/orm/cmd/tork-lsp@latest
```

The extension looks for `tork-lsp` on `PATH`, then in `$GOBIN`, then in
`$GOPATH/bin`, then in `~/go/bin`. Set `tork.lsp.path` if it lives
somewhere else.

## Building the extension

```
npm install
npm run compile
```

Press F5 in VS Code to open an Extension Development Host with it
loaded. To produce an installable package:

```
npx vsce package
```

## Smoke test

With the extension loaded, open a directory holding two `.tork` files
and check each of these by hand. They cover the paths that only a real
editor exercises.

1. **Highlighting.** Keywords, types, attributes, strings, and comments
   are each colored differently.
2. **Diagnostics.** Type a bad type name; the squiggle appears without
   saving. Fix it; the squiggle goes.
3. **Cross file diagnostics.** Declare the same model name in both
   files. The error lands on the second declaration, in the other file
   from the one being edited.
4. **Completion.** After a field name and a space, the type list
   includes models and enums declared in the other file. After `@`, the
   attribute list appears with documentation beside each entry.
5. **Hover.** Hovering a field shows its column, Go type, and
   constraints; hovering a relation shows its key.
6. **Go to definition.** F12 on a model type in one file jumps to its
   declaration in the other.
7. **Formatting.** Shift+Alt+F aligns the block. With format on save
   enabled, saving a file with a syntax error leaves it untouched rather
   than mangling it.
8. **Recovery.** Delete a closing brace. Diagnostics appear, and hover
   and completion keep working on the rest of the file.
