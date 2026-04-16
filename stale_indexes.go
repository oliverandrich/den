package den

import (
	"context"
	"fmt"
)

// DropStaleIndexes removes indexes previously created by Register() that no
// longer correspond to a registered IndexDefinition. Managed indexes (for
// example the PostgreSQL GIN index, FTS triggers, or tables) are not tracked
// and therefore cannot be dropped by this function.
//
// Typically invoked from a migration or deployment script after a struct has
// changed. Pass DryRun() to inspect what would be dropped without making
// changes.
func DropStaleIndexes(ctx context.Context, db *DB, opts ...DropStaleOption) (DropStaleResult, error) {
	cfg := dropStaleConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	db.mu.RLock()
	infos := make([]*collectionInfo, 0, len(db.collections))
	for _, info := range db.collections {
		infos = append(infos, info)
	}
	db.mu.RUnlock()

	var result DropStaleResult
	for _, info := range infos {
		expected := make(map[string]struct{}, len(info.meta.Indexes))
		for _, idx := range info.meta.Indexes {
			expected[idx.Name] = struct{}{}
		}

		recorded, err := db.backend.ListRecordedIndexes(ctx, info.meta.Name)
		if err != nil {
			return result, fmt.Errorf("list recorded indexes for %s: %w", info.meta.Name, err)
		}

		for _, rec := range recorded {
			entry := StaleIndex{
				Collection: info.meta.Name,
				Name:       rec.Name,
				Fields:     rec.Fields,
				Unique:     rec.Unique,
			}
			if _, ok := expected[rec.Name]; ok {
				result.Kept = append(result.Kept, entry)
				continue
			}
			if cfg.dryRun {
				result.Dropped = append(result.Dropped, entry)
				continue
			}
			if err := db.backend.DropIndex(ctx, info.meta.Name, rec.Name); err != nil {
				return result, fmt.Errorf("drop stale index %s on %s: %w", rec.Name, info.meta.Name, err)
			}
			result.Dropped = append(result.Dropped, entry)
		}
	}
	return result, nil
}

// DropStaleOption configures DropStaleIndexes.
type DropStaleOption func(*dropStaleConfig)

type dropStaleConfig struct {
	dryRun bool
}

// DryRun causes DropStaleIndexes to report the indexes that would be dropped
// without actually dropping them.
func DryRun() DropStaleOption {
	return func(c *dropStaleConfig) { c.dryRun = true }
}

// DropStaleResult summarizes a DropStaleIndexes call.
// Dropped contains the indexes that were (or would be, under DryRun) removed.
// Kept contains indexes that are still referenced by a current IndexDefinition.
type DropStaleResult struct {
	Dropped []StaleIndex
	Kept    []StaleIndex
}

// StaleIndex identifies an index inspected by DropStaleIndexes.
type StaleIndex struct {
	Collection string
	Name       string
	Fields     []string
	Unique     bool
}
