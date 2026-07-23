package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// JSON-RPC 2.0 error codes, the subset a language server can produce.
const (
	codeParseError     = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeServerNotInit  = -32002
)

// request is one incoming message. A nil ID marks a notification,
// which must never be answered; that distinction is the whole reason
// the field is a pointer.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response carries its result behind a pointer so that a nil answer is
// still sent, as an explicit null. A response must have exactly one of
// result and error, and a client asking about a document this server
// knows nothing about is owed a null result rather than a missing
// field; omitempty on a plain any would drop it.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  *result         `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

// result is a reply value that encodes as itself, nil included.
type result struct{ value any }

func (r result) MarshalJSON() ([]byte, error) { return json.Marshal(r.value) }

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// notification is an outgoing message with no reply expected, which is
// how diagnostics reach the editor.
type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// conn is the framed message stream. Writes are the only shared state
// between the read loop and anything else, and the server is single
// threaded, so no locking is needed here.
type conn struct {
	r *bufio.Reader
	w io.Writer
}

func newConn(r io.Reader, w io.Writer) *conn {
	return &conn{r: bufio.NewReader(r), w: w}
}

// errClosed reports a stream that ended cleanly between messages,
// which is a normal way for an editor to disconnect rather than a
// failure worth reporting. It is deliberately not io.EOF: a message
// cut off partway through also surfaces as io.EOF, and wrapping that
// would make a truncated frame indistinguishable from a clean
// goodbye.
var errClosed = errors.New("lsp: client disconnected")

// read pulls one message: a header block terminated by a blank line,
// then exactly Content-Length bytes of JSON. Headers other than
// Content-Length are ignored, since Content-Type is the only other one
// the protocol defines and it has a single legal value.
func (c *conn) read() (*request, error) {
	length := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			if err == io.EOF && line == "" && length < 0 {
				return nil, errClosed
			}
			return nil, fmt.Errorf("lsp: reading header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("lsp: malformed header line %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "content-length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("lsp: invalid Content-Length %q", strings.TrimSpace(value))
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("lsp: message has no Content-Length header")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return nil, fmt.Errorf("lsp: reading message body: %w", err)
	}
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		// A body that is not JSON is reported to the client rather
		// than killing the connection, since the next message may
		// well be fine.
		return nil, &protocolError{code: codeParseError, err: err}
	}
	return &req, nil
}

// protocolError is a message level failure the server answers with an
// error response instead of shutting down.
type protocolError struct {
	code int
	err  error
}

func (e *protocolError) Error() string { return e.err.Error() }

func (c *conn) write(msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("lsp: encoding message: %w", err)
	}
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return fmt.Errorf("lsp: writing header: %w", err)
	}
	if _, err := c.w.Write(body); err != nil {
		return fmt.Errorf("lsp: writing body: %w", err)
	}
	return nil
}

func (c *conn) reply(id json.RawMessage, value any) error {
	return c.write(response{JSONRPC: "2.0", ID: id, Result: &result{value}})
}

func (c *conn) replyError(id json.RawMessage, code int, message string) error {
	return c.write(response{JSONRPC: "2.0", ID: id, Error: &responseError{Code: code, Message: message}})
}

func (c *conn) notify(method string, params any) error {
	return c.write(notification{JSONRPC: "2.0", Method: method, Params: params})
}
