package den

import (
	"context"
	"fmt"
	"iter"
)

// Iter returns an iterator over matching documents for use with range.
// Documents are streamed one at a time via the backend's Iterator,
// not collected in memory.
//
//	for doc, err := range den.NewQuery[Product](db).Iter(ctx) {
//	    if err != nil { return err }
//	    fmt.Println(doc.Name)
//	}
//
// Cancelling ctx stops the iteration: the per-row prologue checks
// ctx.Err() and surfaces it through the seq2 error path, so at most one
// further document is yielded to the consumer after cancellation. With
// WithFetchLinks, an in-flight link fetch may still complete its current
// backend round-trip before the next prologue check fires; the link
// resolver passes ctx through, so the round-trip after that observes the
// cancellation.
func (qs QuerySet[T]) Iter(ctx context.Context) iter.Seq2[*T, error] {
	return func(yield func(*T, error) bool) {
		if err := qs.preflight(); err != nil {
			yield(nil, err)
			return
		}
		col, err := collectionFor[T](qs.scope.db())
		if err != nil {
			yield(nil, err)
			return
		}

		q := qs.buildBackendQuery(col)

		it, err := qs.scope.readWriter().Query(ctx, col.meta.Name, q)
		if err != nil {
			yield(nil, err)
			return
		}
		defer func() { _ = it.Close() }()

		for it.Next() {
			if err := ctx.Err(); err != nil {
				yield(nil, err)
				return
			}

			doc := new(T)
			if err := decodeIterRow(qs.scope.db(), it.Bytes(), doc); err != nil {
				yield(nil, fmt.Errorf("decode: %w", err))
				return
			}

			if qs.fetchLinks {
				if err := fetchAllLinksOnDoc(ctx, qs.scope.db(), qs.scope.db().backend, doc, qs.nestDepth); err != nil {
					yield(nil, fmt.Errorf("fetch links: %w", err))
					return
				}
			}
			if !yield(doc, nil) {
				return
			}
		}
		if err := it.Err(); err != nil {
			yield(nil, err)
		}
	}
}
