package orm

import "errors"

// ErrNoRows is returned when a query that expects a row matched none.
//
// It exists because the driver contract cannot report the difference. Row
// has no Err method, so the only signal a QueryRow gives is whatever
// sentinel its driver returns from Scan, and this package imports no
// driver and so cannot name one. First and Find therefore run their query
// through Query with a LIMIT, ask Rows.Next whether anything came back,
// and return this when nothing did.
var ErrNoRows = errors.New("orm: no rows")
