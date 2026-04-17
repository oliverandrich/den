package den_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
)

// TestAllWithCount_WithFetchLinks_SmallPool reproduces den-1c7s: AllWithCount
// opens a read TX for the iterator, but fetchAllLinksOnDoc routes the linked
// reads through db.backend.Get — a separate pool connection. With a tight
// pool and a few concurrent callers, every connection is consumed by an
// active iterator plus its link fetches and the test times out.
//
// After the fix, link resolution reuses the iterator's transaction
// connection and the test completes well inside the deadline.
func TestAllWithCount_WithFetchLinks_SmallPool(t *testing.T) {
	base := dentest.PostgresURL()
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	// Two connections only — deliberately smaller than the per-goroutine
	// footprint of the pre-fix code path (1 for the iterator + 1 for each
	// link fetch).
	url := base + sep + "pool_max_conns=2"

	db, err := den.OpenURL(url)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx := context.Background()
		for _, name := range den.Collections(db) {
			_ = db.Backend().DropCollection(ctx, name)
		}
		_ = db.Close()
	})

	ctx := context.Background()
	require.NoError(t, den.Register(ctx, db, &Door{}, &Window{}, &House{}))

	// Seed: N houses, each with a linked Door. N >= pool size so every
	// iteration step needs a link fetch; together with concurrent callers,
	// the pre-fix code path exhausts the pool.
	const houses = 10
	doors := make([]*Door, houses)
	for i := range houses {
		doors[i] = &Door{Height: 200 + i, Width: 80}
		require.NoError(t, den.Insert(ctx, db, doors[i]))
	}
	for i := range houses {
		h := &House{Name: fmt.Sprintf("h-%d", i), Door: den.NewLink(doors[i])}
		require.NoError(t, den.Insert(ctx, db, h))
	}

	// Run concurrent AllWithCount + WithFetchLinks. Without the fix,
	// goroutines block on pool acquire and the ctx deadline fires.
	const goroutines = 4
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	allResults := make([][]*House, goroutines)

	for g := range goroutines {
		wg.Add(1)
		go func(gi int) {
			defer wg.Done()
			results, _, err := den.NewQuery[House](deadline, db).WithFetchLinks().AllWithCount()
			errs[gi] = err
			allResults[gi] = results
		}(g)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "goroutine %d", i)
	}
	for i, res := range allResults {
		require.Lenf(t, res, houses, "goroutine %d: wrong result count", i)
		for _, h := range res {
			assert.Truef(t, h.Door.IsLoaded(), "goroutine %d: link on %s not loaded", i, h.Name)
			require.NotNilf(t, h.Door.Value, "goroutine %d: link value on %s is nil", i, h.Name)
			assert.NotZerof(t, h.Door.Value.Height, "goroutine %d: link height on %s is zero", i, h.Name)
		}
	}
}
