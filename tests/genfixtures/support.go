// Package genfixtures holds generated models and the handwritten code
// they refer to. Everything with a .gen.go suffix is produced from
// tests/gen/testdata/kitchen/schema by the generator and regenerated
// with `make golden-update`; this file is the handwritten half, and
// exists to prove that generated and handwritten code coexist in one
// package the way a real application mixes them.
//
// That the package compiles at all is the point: it is the standing
// proof that the generator emits code the ORM accepts, checked by the
// ordinary `go build ./...` rather than by any test assertion.
package genfixtures

import "strconv"

// Meta is the Go type a Json column binds to through @go.type("Meta").
type Meta struct {
	Kind  string `json:"kind"`
	Score int    `json:"score"`
}

// nonce counts calls so NewNonce returns something different each
// time, which is all a client side default generator has to promise.
var nonce int

// NewNonce is the client side default behind @default(go("NewNonce")).
func NewNonce() string {
	nonce++
	return "n" + strconv.Itoa(nonce)
}
