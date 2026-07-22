package orm

import "reflect"

// WindowExpr is a window function in a SelectAs projection: a per-row rank
// computed over a partition of the result, rather than collapsing rows the
// way an aggregate does.
//
// Filtering on a window function's own result — the "top 3 per group" query
// — is written by wrapping the projection as a derived table, since no
// statement can name a window function in its own WHERE. See DefineDerived
// and DerivedTable.From.
//
// The vocabulary here is the three ranking functions. LAG, LEAD, NTILE,
// aggregates over a window and frame clauses are not built yet.
type WindowExpr struct {
	fn        string // ROW_NUMBER, RANK, DENSE_RANK
	partition []ColumnMeta
	order     []Ordering
	goType    reflect.Type
}

// GoType returns the Go type a window function's result decodes as, always
// int64.
func (w WindowExpr) GoType() reflect.Type { return w.goType }

// RowNumber numbers each row within its partition, starting at 1, with no
// ties: two rows sorted equal by OrderBy still get distinct numbers, in
// whatever order the database happens to visit them in.
func RowNumber() WindowExpr {
	return WindowExpr{fn: "ROW_NUMBER", goType: reflect.TypeFor[int64]()}
}

// Rank numbers each row within its partition, starting at 1, with ties:
// rows sorted equal by OrderBy share a rank, and the rank after a tie skips
// ahead by the number of tied rows.
func Rank() WindowExpr {
	return WindowExpr{fn: "RANK", goType: reflect.TypeFor[int64]()}
}

// DenseRank is Rank without the skip: the rank after a tie is only one more
// than the tie's own rank.
func DenseRank() WindowExpr {
	return WindowExpr{fn: "DENSE_RANK", goType: reflect.TypeFor[int64]()}
}

// PartitionBy restarts the window function's numbering within each group of
// rows sharing these columns' values, the way GroupBy divides a Grouped
// read. With no PartitionBy, the whole result is one partition.
func (w WindowExpr) PartitionBy(cols ...ColumnMeta) WindowExpr {
	out := w
	out.partition = append(append([]ColumnMeta(nil), w.partition...), cols...)
	return out
}

// OrderBy decides the order a window function counts rows in within their
// partition. Without it, the database is free to number rows in whatever
// order it visits them, which is rarely what a caller wants from RowNumber
// or Rank.
func (w WindowExpr) OrderBy(ords ...Ordering) WindowExpr {
	out := w
	out.order = append(append([]Ordering(nil), w.order...), ords...)
	return out
}
