package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// version is reported to the client in the initialize handshake, where
// it shows up in the editor's server list.
const version = "0.1.0"

// Run serves the protocol over one pair of streams until the client
// disconnects, returning a process exit code suitable for os.Exit. It
// is the whole of cmd/tork-lsp:
//
//	func main() {
//	    os.Exit(lsp.Run(os.Stdin, os.Stdout, os.Stderr))
//	}
//
// Everything the server needs comes from the client and the
// filesystem, so there is nothing to configure.
func Run(r io.Reader, w io.Writer, errOut io.Writer) int {
	s := newServer(newConn(r, w), errOut)
	return s.serve()
}

// server holds the connection and the documents the editor has open.
// Its state is deliberately thin: no analysis is cached between
// requests, because analyzing a schema directory takes microseconds
// and a stale cache is the classic way a language server starts
// disagreeing with the file on disk.
type server struct {
	conn   *conn
	errOut io.Writer

	// overlays holds open documents by URI. Their contents stand in
	// for what is on disk, which is what lets the server answer
	// against unsaved edits.
	overlays map[string]string
	// published remembers where diagnostics were last sent, so a file
	// that becomes clean gets an empty list rather than keeping its
	// last error forever.
	published map[string]bool

	initialized  bool
	shuttingDown bool
}

func newServer(c *conn, errOut io.Writer) *server {
	return &server{
		conn:      c,
		errOut:    errOut,
		overlays:  map[string]string{},
		published: map[string]bool{},
	}
}

// serve is the read loop. A transport level failure ends the session,
// since a stream that cannot be framed cannot be recovered; a message
// level failure is answered and the loop continues.
func (s *server) serve() int {
	for {
		req, err := s.conn.read()
		if err != nil {
			var protoErr *protocolError
			switch {
			case errors.Is(err, errClosed):
				// The client hung up. Whether that is orderly depends
				// on whether it asked to shut down first.
				if s.shuttingDown {
					return 0
				}
				return 1
			case errors.As(err, &protoErr):
				if writeErr := s.conn.replyError(nil, protoErr.code, protoErr.Error()); writeErr != nil {
					fmt.Fprintln(s.errOut, writeErr)
					return 1
				}
				continue
			default:
				fmt.Fprintln(s.errOut, err)
				return 1
			}
		}
		if done, err := s.handle(req); err != nil {
			fmt.Fprintln(s.errOut, err)
			return 1
		} else if done {
			return 0
		}
	}
}

// handle dispatches one message, reporting whether the session should
// end. The error return is for write failures only: nothing a client
// can say should be able to stop the server.
func (s *server) handle(req *request) (done bool, err error) {
	isRequest := len(req.ID) > 0

	// Everything but the handshake waits for initialize, as the
	// protocol requires. A notification arriving early is dropped
	// rather than answered, since notifications take no reply.
	if !s.initialized && req.Method != "initialize" && req.Method != "exit" {
		if isRequest {
			return false, s.conn.replyError(req.ID, codeServerNotInit, "server not initialized")
		}
		return false, nil
	}

	switch req.Method {
	case "initialize":
		s.initialized = true
		return false, s.conn.reply(req.ID, initializeResult{
			Capabilities: serverCapabilities{
				TextDocumentSync:           syncFull,
				CompletionProvider:         &completionOptions{TriggerCharacters: []string{"@", ".", "\"", "[", "(", " "}},
				HoverProvider:              true,
				DefinitionProvider:         true,
				DocumentFormattingProvider: true,
				Workspace: &workspaceOptions{
					WorkspaceFolders: &workspaceFoldersOptions{Supported: true},
				},
			},
			ServerInfo: serverInfo{Name: "tork-lsp", Version: version},
		})
	case "initialized":
		return false, nil
	case "shutdown":
		s.shuttingDown = true
		return false, s.conn.reply(req.ID, nil)
	case "exit":
		// An exit before shutdown is an abnormal end, which the
		// protocol asks be reported as a nonzero code.
		return true, nil

	case "textDocument/didOpen":
		var p didOpenParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return false, nil
		}
		s.overlays[p.TextDocument.URI] = p.TextDocument.Text
		return false, s.publish(p.TextDocument.URI)
	case "textDocument/didChange":
		var p didChangeParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return false, nil
		}
		// Full sync means the last change carries the whole document.
		if n := len(p.Changes); n > 0 {
			s.overlays[p.TextDocument.URI] = p.Changes[n-1].Text
		}
		return false, s.publish(p.TextDocument.URI)
	case "textDocument/didSave":
		var p didSaveParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return false, nil
		}
		return false, s.publish(p.TextDocument.URI)
	case "textDocument/didClose":
		var p didCloseParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return false, nil
		}
		delete(s.overlays, p.TextDocument.URI)
		// Re-reporting from disk matters: the buffer that was just
		// discarded may have been the only thing making the schema
		// valid, or the only thing breaking it.
		return false, s.publish(p.TextDocument.URI)

	case "textDocument/completion":
		return false, s.answer(req, func(f *folder, name string, pos Position) any {
			return s.completion(f, name, pos)
		})
	case "textDocument/hover":
		return false, s.answer(req, func(f *folder, name string, pos Position) any {
			return s.hover(f, name, pos)
		})
	case "textDocument/definition":
		return false, s.answer(req, func(f *folder, name string, pos Position) any {
			return s.definition(f, name, pos)
		})
	case "textDocument/formatting":
		return false, s.formatting(req)

	default:
		if isRequest {
			return false, s.conn.replyError(req.ID, codeMethodNotFound, "unsupported method "+req.Method)
		}
		return false, nil
	}
}

// answer runs a position based request, doing the decoding, folder
// lookup, and null result handling every one of them shares. A
// document this server knows nothing about gets a null result rather
// than an error, because that is what a client expects when it asks
// about a file outside any schema directory.
func (s *server) answer(req *request, fn func(f *folder, name string, pos Position) any) error {
	var p positionParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return s.conn.replyError(req.ID, codeInvalidParams, err.Error())
	}
	f, name := s.forURI(p.TextDocument.URI)
	if f == nil {
		return s.conn.reply(req.ID, nil)
	}
	return s.conn.reply(req.ID, fn(f, name, p.Position))
}
