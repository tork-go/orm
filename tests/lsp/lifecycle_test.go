package lsp_test

import (
	"encoding/json"
	"testing"
)

func TestInitialize_AdvertisesTheServerCapabilities(t *testing.T) {
	c := start(t)
	result := c.initialize()

	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("initialize returned no capabilities: %v", result)
	}
	for _, name := range []string{"hoverProvider", "definitionProvider", "documentFormattingProvider"} {
		if enabled, _ := caps[name].(bool); !enabled {
			t.Errorf("%s = false, want true", name)
		}
	}
	if sync, _ := caps["textDocumentSync"].(float64); sync != 1 {
		t.Errorf("textDocumentSync = %v, want 1 (full)", caps["textDocumentSync"])
	}
	completion, ok := caps["completionProvider"].(map[string]any)
	if !ok {
		t.Fatalf("no completionProvider capability: %v", caps)
	}
	triggers, _ := completion["triggerCharacters"].([]any)
	if len(triggers) == 0 {
		t.Errorf("completion advertises no trigger characters")
	}
	info, ok := result["serverInfo"].(map[string]any)
	if !ok || info["name"] != "tork-lsp" {
		t.Errorf("serverInfo = %v, want a name of tork-lsp", result["serverInfo"])
	}
}

func TestRequestsBeforeInitialize_AreRefused(t *testing.T) {
	c := start(t)
	uri := workspace(t, nil)("schema.tork")
	m := c.call("textDocument/hover", positionAt(uri, 0, 0), nil)
	if m.Error == nil {
		t.Fatal("expected an error before initialize")
	}
	if m.Error.Code != -32002 {
		t.Errorf("error code = %d, want -32002 (server not initialized)", m.Error.Code)
	}
}

// Notifications arriving before the handshake are dropped rather than
// answered, since a notification takes no reply at all.
func TestNotificationsBeforeInitialize_AreDropped(t *testing.T) {
	c := start(t)
	uri := workspace(t, nil)("schema.tork")
	c.open(uri, "model A {\n\tid Int @id\n}\n")

	// The handshake that follows must be answered normally, proving
	// the dropped notification neither replied nor wedged the server.
	if result := c.initialize(); result["capabilities"] == nil {
		t.Fatal("the server did not recover from a premature notification")
	}
}

func TestShutdownThenExit_EndsTheSessionCleanly(t *testing.T) {
	c := start(t)
	c.initialize()

	m := c.call("shutdown", nil, nil)
	if m.Error != nil {
		t.Fatalf("shutdown returned an error: %v", m.Error)
	}
	c.notify("exit", nil)

	select {
	case code := <-c.done:
		if code != 0 {
			t.Errorf("exit code = %d, want 0 after an orderly shutdown", code)
		}
	case <-timeout():
		t.Fatal("the server did not exit after the exit notification")
	}
}

// A connection that drops without a shutdown is an abnormal end, and
// the protocol asks that it be reported as one.
func TestConnectionDroppedWithoutShutdown_ReportsFailure(t *testing.T) {
	c := start(t)
	c.initialize()
	c.in.Close()

	select {
	case code := <-c.done:
		if code != 1 {
			t.Errorf("exit code = %d, want 1 when the client disappears", code)
		}
	case <-timeout():
		t.Fatal("the server did not notice the closed connection")
	}
}

func TestUnknownMethod_IsRefusedWithoutEndingTheSession(t *testing.T) {
	c := start(t)
	c.initialize()

	m := c.call("textDocument/rename", map[string]any{}, nil)
	if m.Error == nil {
		t.Fatal("expected an error for an unsupported method")
	}
	if m.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601 (method not found)", m.Error.Code)
	}
	if want := "unsupported method textDocument/rename"; m.Error.Message != want {
		t.Errorf("error message = %q, want %q", m.Error.Message, want)
	}
	// The session survives: an unknown notification is ignored too.
	c.notify("$/setTrace", map[string]any{"value": "off"})
	if m := c.call("shutdown", nil, nil); m.Error != nil {
		t.Errorf("the session did not survive an unsupported method")
	}
}

func TestMalformedJSON_IsReportedWithoutEndingTheSession(t *testing.T) {
	c := start(t)
	c.initialize()

	c.sendRaw("{not json")
	m := c.read()
	if m.Error == nil {
		t.Fatalf("expected a parse error, got %+v", m)
	}
	if m.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700 (parse error)", m.Error.Code)
	}
	if m := c.call("shutdown", nil, nil); m.Error != nil {
		t.Errorf("the session did not survive malformed JSON")
	}
}

func TestMalformedFraming_EndsTheSession(t *testing.T) {
	tests := map[string]string{
		"no content length":      "Sec: 1\r\n\r\n{}",
		"unparseable length":     "Content-Length: abc\r\n\r\n{}",
		"header without a colon": "nonsense\r\n\r\n{}",
	}
	for name, framing := range tests {
		t.Run(name, func(t *testing.T) {
			c := start(t)
			if _, err := c.in.Write([]byte(framing)); err != nil {
				t.Fatalf("writing: %v", err)
			}
			select {
			case code := <-c.done:
				if code != 1 {
					t.Errorf("exit code = %d, want 1", code)
				}
			case <-timeout():
				t.Fatal("the server accepted a frame it cannot have understood")
			}
			if c.errOut.Len() == 0 {
				t.Errorf("nothing was reported on stderr")
			}
		})
	}
}

func TestRequestWithInvalidParams_IsRefused(t *testing.T) {
	c := start(t)
	c.initialize()
	c.sendRaw(encode(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      99,
		"method":  "textDocument/hover",
		"params":  "not an object",
	}))
	var id json.RawMessage
	m := c.read()
	if m.ID == nil {
		t.Fatalf("expected a reply, got a notification: %+v", m)
	}
	id = *m.ID
	if string(id) != "99" {
		t.Errorf("reply id = %s, want 99", id)
	}
	if m.Error == nil || m.Error.Code != -32602 {
		t.Errorf("error = %+v, want code -32602 (invalid params)", m.Error)
	}
}

func TestMalformedNotificationParams_AreIgnored(t *testing.T) {
	c := start(t)
	c.initialize()
	for _, method := range []string{
		"textDocument/didOpen", "textDocument/didChange",
		"textDocument/didClose", "textDocument/didSave",
	} {
		c.sendRaw(encode(t, map[string]any{
			"jsonrpc": "2.0",
			"method":  method,
			"params":  "not an object",
		}))
	}
	if m := c.call("shutdown", nil, nil); m.Error != nil {
		t.Errorf("the session did not survive malformed notification params")
	}
}
