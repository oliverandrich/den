package migrate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	json "github.com/goccy/go-json"

	"github.com/oliverandrich/den"
)

// Migration defines a forward and optional backward migration function.
// Both receive a transaction for atomic execution.
type Migration struct {
	Forward  func(ctx context.Context, tx *den.Tx) error
	Backward func(ctx context.Context, tx *den.Tx) error
}

type registeredMigration struct {
	version   string
	migration Migration
}

// Registry holds registered migrations and provides the runner API.
type Registry struct {
	migrations []registeredMigration

	// ensureTableOnce collapses concurrent in-process Up/Down starters into
	// a single EnsureCollection call for the migration log. Without this,
	// 8 goroutines simultaneously asking PostgreSQL for the auto GIN index
	// via CREATE INDEX CONCURRENTLY IF NOT EXISTS deadlock on the shared
	// ShareUpdateExclusiveLock acquisition. One goroutine runs the setup
	// once; the rest wait and reuse the result (including any error).
	ensureTableOnce sync.Once
	ensureTableErr  error
}

// NewRegistry creates a new migration registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a migration with the given version identifier.
// Versions are sorted lexicographically for execution order.
func (r *Registry) Register(version string, m Migration) {
	r.migrations = append(r.migrations, registeredMigration{
		version:   version,
		migration: m,
	})
	sort.Slice(r.migrations, func(i, j int) bool {
		return r.migrations[i].version < r.migrations[j].version
	})
}

// Up runs all pending forward migrations, each in its own transaction.
// Concurrent starters serialize on an advisory lock; every registered
// migration runs exactly once across processes.
func (r *Registry) Up(ctx context.Context, db *den.DB) error {
	if err := r.ensureMigrationTable(ctx, db); err != nil {
		return err
	}
	for _, m := range r.migrations {
		if err := runForward(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}

// UpOne runs the next pending forward migration in a transaction.
// Loads the current applied set to find the first pending version; the
// actual "apply exactly once" guarantee comes from the lock in runForward.
func (r *Registry) UpOne(ctx context.Context, db *den.DB) error {
	if err := r.ensureMigrationTable(ctx, db); err != nil {
		return err
	}
	applied, err := r.loadApplied(ctx, db)
	if err != nil {
		return err
	}

	for _, m := range r.migrations {
		if slices.Contains(applied, m.version) {
			continue
		}
		return runForward(ctx, db, m)
	}
	return nil // nothing pending
}

// DownOne rolls back the last applied migration in a transaction.
func (r *Registry) DownOne(ctx context.Context, db *den.DB) error {
	applied, err := r.loadApplied(ctx, db)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		return nil
	}

	last := applied[len(applied)-1]
	m, ok := r.findMigration(last)
	if !ok {
		return fmt.Errorf("den: migration %q not found in registry", last)
	}

	if m.migration.Backward == nil {
		return fmt.Errorf("den: migration %q has no backward function", last)
	}

	return runBackward(ctx, db, m)
}

// Down rolls back all applied migrations in reverse order, each in its own transaction.
func (r *Registry) Down(ctx context.Context, db *den.DB) error {
	applied, err := r.loadApplied(ctx, db)
	if err != nil {
		return err
	}

	for i := len(applied) - 1; i >= 0; i-- {
		version := applied[i]
		m, ok := r.findMigration(version)
		if !ok {
			return fmt.Errorf("den: migration %q not found in registry", version)
		}
		if m.migration.Backward == nil {
			return fmt.Errorf("den: migration %q has no backward function", version)
		}

		if err := runBackward(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}

// migrationLockKey is the advisory-lock key used to serialize concurrent
// migration runners. The value is arbitrary and only needs to be stable so
// two starters contend on the same key; "den.migrate" CRC32 keeps it
// distinct from any other advisory lock a user might use in their own code.
const migrationLockKey int64 = 0x44656e4d4947524e // "DenMIGRN"

func runForward(ctx context.Context, db *den.DB, m registeredMigration) error {
	return den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		// Serialize concurrent starters: on PG this is pg_advisory_xact_lock,
		// on SQLite a no-op (IMMEDIATE tx already serializes writers).
		if err := den.AdvisoryLock(ctx, tx, migrationLockKey); err != nil {
			return err
		}

		// Re-read the applied set inside the tx+lock so we see writes
		// committed by any other starter that went first.
		entries, err := loadEntriesFromTx(ctx, tx)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.Version == m.version {
				return nil // another starter got here first
			}
		}

		if err := m.migration.Forward(ctx, tx); err != nil {
			return fmt.Errorf("%w: %s: %w", den.ErrMigrationFailed, m.version, err)
		}

		entries = append(entries, migrationEntry{
			Version:   m.version,
			AppliedAt: time.Now(),
		})
		return saveEntriesToTx(ctx, tx, entries)
	})
}

func runBackward(ctx context.Context, db *den.DB, m *registeredMigration) error {
	return den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		if err := den.AdvisoryLock(ctx, tx, migrationLockKey); err != nil {
			return err
		}

		entries, err := loadEntriesFromTx(ctx, tx)
		if err != nil {
			return err
		}
		found := false
		for _, e := range entries {
			if e.Version == m.version {
				found = true
				break
			}
		}
		if !found {
			return nil // another starter rolled this back first
		}

		if err := m.migration.Backward(ctx, tx); err != nil {
			return fmt.Errorf("%w: %s: %w", den.ErrMigrationFailed, m.version, err)
		}

		filtered := entries[:0]
		for _, e := range entries {
			if e.Version != m.version {
				filtered = append(filtered, e)
			}
		}
		return saveEntriesToTx(ctx, tx, filtered)
	})
}

func (r *Registry) findMigration(version string) (*registeredMigration, bool) {
	for i := range r.migrations {
		if r.migrations[i].version == version {
			return &r.migrations[i], true
		}
	}
	return nil, false
}

// migrationLog tracks applied migrations.
type migrationEntry struct {
	Version   string    `json:"version"`
	AppliedAt time.Time `json:"applied_at"`
}

const migrationsCollection = "_den_migrations"

// ensureMigrationTable runs EnsureCollection at most once per Registry.
// Concurrent callers block on sync.Once so only one goroutine drives the
// DDL; everyone else observes the cached result.
func (r *Registry) ensureMigrationTable(ctx context.Context, db *den.DB) error {
	r.ensureTableOnce.Do(func() {
		r.ensureTableErr = db.Backend().EnsureCollection(ctx, migrationsCollection, den.CollectionMeta{Name: migrationsCollection})
	})
	return r.ensureTableErr
}

func (r *Registry) loadApplied(ctx context.Context, db *den.DB) ([]string, error) {
	if err := r.ensureMigrationTable(ctx, db); err != nil {
		return nil, err
	}
	data, err := db.Backend().Get(ctx, migrationsCollection, "log")
	if err != nil {
		if errors.Is(err, den.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	var entries []migrationEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}

	versions := make([]string, len(entries))
	for i, e := range entries {
		versions[i] = e.Version
	}
	return versions, nil
}

func loadEntriesFromTx(ctx context.Context, tx *den.Tx) ([]migrationEntry, error) {
	data, err := den.RawGet(ctx, tx, migrationsCollection, "log")
	if err != nil {
		if errors.Is(err, den.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	var entries []migrationEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func saveEntriesToTx(ctx context.Context, tx *den.Tx, entries []migrationEntry) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return den.RawPut(ctx, tx, migrationsCollection, "log", data)
}
