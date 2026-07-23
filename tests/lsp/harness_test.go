package lsp_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tork-go/orm/lsp"
)

// The suite drives a real server over a pipe pair, speaking the wire
// protocol byte for byte. Testing through the transport rather than
// calling handlers directly is deliberate: framing, JSON shapes, and
// the notification versus request distinction are exactly the parts an
// editor will break on, and they are invisible to a test that skips
// the wire.

// deadline bounds every wait in the harness, so a server that stops
// answering fails one test instead of hanging the package.
const deadline = 5 * time.Second

func timeout() <-chan time.Time { return time.After(deadline) }

// client is a test client. A goroutine drains the server's output the
// whole time, which matters because a pipe carries no buffer: a server
// publishing diagnostics nobody is reading would block, and then so
// would the next request, for a deadlock that is the harness's fault
// rather than the server's.
type client struct {
	t      *testing.T
	in     io.WriteCloser
	errOut *strings.Builder
	done   chan int

	incoming chan message
	readErr  chan error
	// reader is the client's end of the server's output, kept so a
	// test can close it and watch the server fail to write.
	reader io.Closer

	nextID int
	// seen holds notifications read while waiting for a reply.
	seen []message
}

// message is a decoded message from the server, either a reply or a
// notification depending on which fields are set.
type message struct {
	ID     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Result json.RawMessage  `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Params json.RawMessage `json:"params"`
}

// start launches a server over pipes and shuts it down when the test
// ends, failing if it does not exit promptly.
func start(t *testing.T) *client {
	t.Helper()
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()
	c := &client{
		t:        t,
		in:       clientWriter,
		reader:   clientReader,
		errOut:   &strings.Builder{},
		done:     make(chan int, 1),
		incoming: make(chan message, 256),
		readErr:  make(chan error, 1),
	}
	go func() {
		code := lsp.Run(serverReader, serverWriter, c.errOut)
		serverWriter.Close()
		// Sent and then closed, so a test that already took the exit
		// code and the cleanup that always waits for one can both
		// read this channel without either blocking on the other.
		c.done <- code
		close(c.done)
	}()
	go func() {
		reader := bufio.NewReader(clientReader)
		for {
			m, err := readMessage(reader)
			if err != nil {
				c.readErr <- err
				close(c.incoming)
				return
			}
			c.incoming <- m
		}
	}()
	t.Cleanup(func() {
		clientWriter.Close()
		select {
		case <-c.done:
		case <-timeout():
			t.Error("the server did not exit after the connection closed")
		}
	})
	return c
}

// closeReader hangs up on the server's output, which is what an
// editor that crashed looks like from the server's side.
func (c *client) closeReader() {
	c.t.Helper()
	if err := c.reader.Close(); err != nil {
		c.t.Fatalf("closing the reader: %v", err)
	}
}

func (c *client) sendRaw(body string) {
	c.t.Helper()
	if _, err := fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		c.t.Fatalf("writing message: %v", err)
	}
}

func (c *client) request(method string, params any) int {
	c.t.Helper()
	c.nextID++
	id := c.nextID
	c.sendRaw(encode(c.t, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}))
	return id
}

func (c *client) notify(method string, params any) {
	c.t.Helper()
	c.sendRaw(encode(c.t, map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}))
}

func encode(t *testing.T, v any) string {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encoding message: %v", err)
	}
	return string(body)
}

// readMessage pulls one framed message off the wire.
func readMessage(r *bufio.Reader) (message, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return message{}, fmt.Errorf("reading header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, _ := strings.Cut(line, ":")
		if strings.EqualFold(strings.TrimSpace(name), "content-length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return message{}, fmt.Errorf("bad Content-Length %q", value)
			}
		}
	}
	if length < 0 {
		return message{}, fmt.Errorf("message had no Content-Length header")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return message{}, fmt.Errorf("reading body: %w", err)
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return message{}, fmt.Errorf("decoding message %q: %w", body, err)
	}
	return m, nil
}

// read takes the next message the server sent, under the deadline.
func (c *client) read() message {
	c.t.Helper()
	select {
	case m, ok := <-c.incoming:
		if !ok {
			c.t.Fatalf("the server closed the connection: %v (stderr: %s)", <-c.readErr, c.errOut)
		}
		return m
	case <-timeout():
		c.t.Fatalf("the server sent nothing within the deadline (stderr: %s)", c.errOut)
		return message{}
	}
}

// await reads until the reply to id arrives, keeping any notifications
// that come first. Servers may interleave the two, so a test that
// assumed strict ordering would be testing an accident.
func (c *client) await(id int) message {
	c.t.Helper()
	for {
		m := c.read()
		if m.ID == nil {
			c.seen = append(c.seen, m)
			continue
		}
		var got int
		if err := json.Unmarshal(*m.ID, &got); err != nil {
			c.t.Fatalf("decoding reply id: %v", err)
		}
		if got == id {
			return m
		}
	}
}

// call sends a request and decodes the reply into out.
func (c *client) call(method string, params any, out any) message {
	c.t.Helper()
	m := c.await(c.request(method, params))
	if m.Error != nil {
		return m
	}
	if out != nil && len(m.Result) > 0 {
		if err := json.Unmarshal(m.Result, out); err != nil {
			c.t.Fatalf("decoding %s result %q: %v", method, m.Result, err)
		}
	}
	return m
}

// initialize performs the handshake every session starts with.
func (c *client) initialize() map[string]any {
	c.t.Helper()
	var result map[string]any
	c.call("initialize", map[string]any{}, &result)
	c.notify("initialized", map[string]any{})
	return result
}

// settle makes a round trip so that every notification the server
// meant to send has arrived. The server handles messages in order, so
// a reply to a request sent after a notification proves the
// notification is already in hand. This is what lets a test assert
// that nothing was published without waiting for a message that is
// never coming.
func (c *client) settle() {
	c.t.Helper()
	c.await(c.request("shutdown", nil))
}

// diagnostics returns the last diagnostics published for uri, and
// whether any were published at all. It settles first, so the answer
// is complete rather than merely current.
func (c *client) diagnostics(uri string) ([]lsp.Diagnostic, bool) {
	c.t.Helper()
	c.settle()
	var last []lsp.Diagnostic
	found := false
	for _, m := range c.seen {
		if m.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var p struct {
			URI         string           `json:"uri"`
			Diagnostics []lsp.Diagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			c.t.Fatalf("decoding diagnostics: %v", err)
		}
		if p.URI == uri {
			last, found = p.Diagnostics, true
		}
	}
	c.seen = nil
	return last, found
}

// diagnosticsFor is diagnostics for the common case where the test
// expects the server to have said something.
func (c *client) diagnosticsFor(uri string) []lsp.Diagnostic {
	c.t.Helper()
	ds, ok := c.diagnostics(uri)
	if !ok {
		c.t.Fatalf("the server published no diagnostics for %s", uri)
	}
	return ds
}

// workspace writes a schema directory and returns a function turning a
// base name into the URI the protocol uses for it.
func workspace(t *testing.T, files map[string]string) func(string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return func(name string) string {
		u := url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(dir, name))}
		return u.String()
	}
}

func (c *client) open(uri, text string) {
	c.t.Helper()
	c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "text": text},
	})
}

func (c *client) change(uri, text string) {
	c.t.Helper()
	c.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri},
		"contentChanges": []map[string]any{{"text": text}},
	})
}

func positionAt(uri string, line, character int) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     map[string]any{"line": line, "character": character},
	}
}

func messages(ds []lsp.Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Message)
	}
	return out
}

func labels(items []lsp.CompletionItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
