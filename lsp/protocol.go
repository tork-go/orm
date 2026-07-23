package lsp

// The protocol structures below cover only what this server actually
// sends and receives. The specification is enormous and most of it
// describes capabilities a schema language has no use for; declaring
// the whole thing would be a large surface to keep correct in exchange
// for nothing.

// Position is a zero based line and character offset. Character counts
// UTF-16 code units, which is the protocol's default and the reason
// positions.go exists to convert.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half open span between two positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location points at a range in a specific document, the shape go to
// definition answers with.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Diagnostic severities, as the protocol numbers them.
const (
	severityError   = 1
	severityWarning = 2
)

// Diagnostic is one finding shown in the editor.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Source   string `json:"source"`
	Message  string `json:"message"`
}

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type textDocumentItem struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type contentChange struct {
	Text string `json:"text"`
}

type didChangeParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Changes      []contentChange        `json:"contentChanges"`
}

type didCloseParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type didSaveParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type positionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type formattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

// TextEdit replaces a range with new text. Formatting answers with a
// single edit covering the whole document, since the formatter works
// on whole files and a minimal diff would be effort spent to produce
// the same result.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// Completion item kinds, as the protocol numbers them. Only the four
// that fit a schema language are used.
const (
	kindField    = 5
	kindClass    = 7
	kindProperty = 10
	kindEnum     = 13
	kindKeyword  = 14
)

// CompletionItem is one suggestion. Detail is the grey text beside the
// label and Documentation the panel below it, which is where an
// attribute explains what it does.
type CompletionItem struct {
	Label         string `json:"label"`
	Kind          int    `json:"kind"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
}

// MarkupContent is hover text. Markdown is what every client renders,
// so the kind is never anything else.
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Hover is the answer to a hover request.
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// Text document sync kinds. Full is the only one this server offers:
// schemas are small enough that resending the document costs nothing,
// and incremental sync would add a text patching layer whose bugs
// would look exactly like analyzer bugs.
const syncFull = 1

type serverCapabilities struct {
	TextDocumentSync           int                `json:"textDocumentSync"`
	CompletionProvider         *completionOptions `json:"completionProvider,omitempty"`
	HoverProvider              bool               `json:"hoverProvider"`
	DefinitionProvider         bool               `json:"definitionProvider"`
	DocumentFormattingProvider bool               `json:"documentFormattingProvider"`
	Workspace                  *workspaceOptions  `json:"workspace,omitempty"`
}

type completionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters"`
}

type workspaceOptions struct {
	WorkspaceFolders *workspaceFoldersOptions `json:"workspaceFolders,omitempty"`
}

type workspaceFoldersOptions struct {
	Supported bool `json:"supported"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	Capabilities serverCapabilities `json:"capabilities"`
	ServerInfo   serverInfo         `json:"serverInfo"`
}
