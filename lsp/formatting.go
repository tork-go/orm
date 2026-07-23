package lsp

import (
	"encoding/json"

	"github.com/tork-go/orm/gen/format"
)

// formatting answers a format request with one edit replacing the
// whole document, since the formatter works on whole files and a
// minimal diff would be effort spent reaching the same result.
//
// Source with syntax errors formats to itself, so a client with format
// on save never loses text while the user is mid keystroke; the empty
// edit list here says exactly that.
func (s *server) formatting(req *request) error {
	var p formattingParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return s.conn.replyError(req.ID, codeInvalidParams, err.Error())
	}
	f, name := s.forURI(p.TextDocument.URI)
	if f == nil {
		return s.conn.reply(req.ID, nil)
	}
	doc := f.docs[name]
	formatted, _ := format.Source(name, []byte(doc.text))
	if string(formatted) == doc.text {
		return s.conn.reply(req.ID, []TextEdit{})
	}
	return s.conn.reply(req.ID, []TextEdit{{
		Range:   doc.wholeRange(),
		NewText: string(formatted),
	}})
}
