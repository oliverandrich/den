package den

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"

	"github.com/oliverandrich/den/internal"
)

// Register analyzes the given document types and registers their
// collections with the database. Must be called before any CRUD operations.
func Register(ctx context.Context, db *DB, types ...any) error {
	for _, docType := range types {
		t := reflect.TypeOf(docType)
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}

		info, err := internal.AnalyzeStruct(t)
		if err != nil {
			return fmt.Errorf("analyze %s: %w", t.Name(), err)
		}

		meta := buildCollectionMeta(info)
		settings := getSettings(docType)

		// Apply custom settings before creating backend resources
		if settings.CollectionName != "" {
			meta.Name = settings.CollectionName
		}
		meta.Indexes = append(meta.Indexes, settings.Indexes...)

		if err := db.backend.EnsureCollection(ctx, meta.Name, meta); err != nil {
			return fmt.Errorf("ensure collection %s: %w", meta.Name, err)
		}

		for _, idx := range meta.Indexes {
			if err := db.backend.EnsureIndex(ctx, meta.Name, idx); err != nil {
				return fmt.Errorf("ensure index %s on %s: %w", idx.Name, meta.Name, err)
			}
		}

		if err := ensureFTSForCollection(ctx, db, meta); err != nil {
			return fmt.Errorf("ensure FTS for %s: %w", meta.Name, err)
		}

		db.mu.Lock()
		db.collections[meta.Name] = &collectionInfo{
			meta:       meta,
			structInfo: info,
			settings:   settings,
		}
		derivedName := internal.CollectionName(t.Name())
		if derivedName != meta.Name {
			db.typeToCollection[derivedName] = meta.Name
		}
		// Invalidate typeCache so collectionFor picks up the new entry
		db.typeCache.Delete(t)
		db.mu.Unlock()
	}

	return nil
}

// collectionFor returns the collectionInfo for the given Go type.
func collectionFor[T any](db *DB) (*collectionInfo, error) {
	var zero T
	t := reflect.TypeOf(zero)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return collectionForType(db, t)
}

// collectionForType is the non-generic implementation used by both
// collectionFor[T] and link resolution (which only has a reflect.Type).
func collectionForType(db *DB, t reflect.Type) (*collectionInfo, error) {
	// Fast path: lock-free cache lookup
	if cached, ok := db.typeCache.Load(t); ok {
		v, _ := cached.(*collectionInfo)
		return v, nil
	}

	// Slow path: resolve name and look up in registry
	name := internal.CollectionName(t.Name())

	db.mu.RLock()
	if mapped, ok := db.typeToCollection[name]; ok {
		name = mapped
	}
	info, ok := db.collections[name]
	db.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotRegistered, name)
	}

	// Cache for future lookups
	db.typeCache.Store(t, info)
	return info, nil
}

// Meta returns the collection metadata for the given document type.
func Meta[T any](db *DB) (CollectionMeta, error) {
	col, err := collectionFor[T](db)
	if err != nil {
		return CollectionMeta{}, err
	}
	return col.meta, nil
}

// Collections returns the names of all registered collections in sorted order.
func Collections(db *DB) []string {
	db.mu.RLock()
	defer db.mu.RUnlock()

	names := make([]string, 0, len(db.collections))
	for name := range db.collections {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func buildCollectionMeta(info *internal.StructInfo) CollectionMeta {
	meta := CollectionMeta{
		Name:        info.CollectionName,
		HasSoftBase: info.HasDeletedAt,
	}

	for _, f := range info.Fields {
		fm := FieldMeta{
			Name:      f.JSONName,
			GoName:    f.GoName,
			Type:      f.Type.String(),
			Indexed:   f.Options.Index,
			Unique:    f.Options.Unique,
			FTS:       f.Options.FTS,
			IsPointer: f.IsPointer,
		}
		meta.Fields = append(meta.Fields, fm)

		if f.Options.Index && !f.Options.Unique {
			meta.Indexes = append(meta.Indexes, IndexDefinition{
				Name:   "idx_" + info.CollectionName + "_" + f.JSONName,
				Fields: []string{f.JSONName},
				Unique: false,
			})
		}
		if f.Options.Unique {
			meta.Indexes = append(meta.Indexes, IndexDefinition{
				Name:   "idx_" + info.CollectionName + "_" + f.JSONName,
				Fields: []string{f.JSONName},
				Unique: true,
			})
		}
	}

	// Collect unique_together and index_together groups
	uniqueGroups := make(map[string][]string)
	indexGroups := make(map[string][]string)
	for _, f := range info.Fields {
		if f.Options.UniqueTogether != "" {
			uniqueGroups[f.Options.UniqueTogether] = append(uniqueGroups[f.Options.UniqueTogether], f.JSONName)
		}
		if f.Options.IndexTogether != "" {
			indexGroups[f.Options.IndexTogether] = append(indexGroups[f.Options.IndexTogether], f.JSONName)
		}
	}

	// Create composite unique indexes (sorted group names for deterministic order)
	for _, name := range sortedKeys(uniqueGroups) {
		meta.Indexes = append(meta.Indexes, IndexDefinition{
			Name:   "idx_" + info.CollectionName + "_" + name,
			Fields: uniqueGroups[name],
			Unique: true,
		})
	}

	// Create composite non-unique indexes
	for _, name := range sortedKeys(indexGroups) {
		meta.Indexes = append(meta.Indexes, IndexDefinition{
			Name:   "idx_" + info.CollectionName + "_" + name,
			Fields: indexGroups[name],
			Unique: false,
		})
	}

	return meta
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
