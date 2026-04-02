package den

import (
	"fmt"
	"iter"
)

// Iter returns an iterator over matching documents for use with range.
// Documents are streamed one at a time via the backend's Iterator,
// not collected in memory.
//
//	for doc, err := range den.NewQuery[Product](ctx, db).Iter() {
//	    if err != nil { return err }
//	    fmt.Println(doc.Name)
//	}
func (qs QuerySet[T]) Iter() iter.Seq2[*T, error] {
	return func(yield func(*T, error) bool) {
		col, err := collectionFor[T](qs.db)
		if err != nil {
			yield(nil, err)
			return
		}

		q := qs.buildBackendQuery(col)

		it, err := qs.db.backend.Query(qs.ctx, col.meta.Name, q)
		if err != nil {
			yield(nil, err)
			return
		}
		defer func() { _ = it.Close() }()

		for it.Next() {
			rawBytes := make([]byte, len(it.Bytes()))
			copy(rawBytes, it.Bytes())

			doc := new(T)
			if err := qs.db.decode(rawBytes, doc); err != nil {
				if !yield(nil, fmt.Errorf("decode: %w", err)) {
					return
				}
				continue
			}
			captureSnapshot(rawBytes, doc)

			if qs.fetchLinks {
				if err := fetchAllLinksOnDoc(qs.ctx, qs.db, doc, qs.nestDepth); err != nil {
					if !yield(nil, fmt.Errorf("fetch links: %w", err)) {
						return
					}
					continue
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
