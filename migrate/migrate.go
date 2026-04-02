package migrate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
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
func (r *Registry) Up(ctx context.Context, db *den.DB) error {
	applied, err := loadApplied(ctx, db)
	if err != nil {
		return err
	}

	for _, m := range r.migrations {
		if slices.Contains(applied, m.version) {
			continue
		}
		if err := runForward(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}

// UpOne runs the next pending forward migration in a transaction.
func (r *Registry) UpOne(ctx context.Context, db *den.DB) error {
	applied, err := loadApplied(ctx, db)
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
	applied, err := loadApplied(ctx, db)
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
	applied, err := loadApplied(ctx, db)
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

func runForward(ctx context.Context, db *den.DB, m registeredMigration) error {
	return den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		if err := m.migration.Forward(ctx, tx); err != nil {
			return fmt.Errorf("%w: %s: %w", den.ErrMigrationFailed, m.version, err)
		}
		return markApplied(ctx, tx, m.version)
	})
}

func runBackward(ctx context.Context, db *den.DB, m *registeredMigration) error {
	return den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
		if err := m.migration.Backward(ctx, tx); err != nil {
			return fmt.Errorf("%w: %s: %w", den.ErrMigrationFailed, m.version, err)
		}
		return unmarkApplied(ctx, tx, m.version)
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

func ensureMigrationTable(ctx context.Context, db *den.DB) error {
	return db.Backend().EnsureCollection(ctx, migrationsCollection, den.CollectionMeta{Name: migrationsCollection})
}

func loadApplied(ctx context.Context, db *den.DB) ([]string, error) {
	if err := ensureMigrationTable(ctx, db); err != nil {
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

// markApplied and unmarkApplied work on the transaction backend
// so they are atomic with the migration itself.

func markApplied(_ context.Context, tx *den.Tx, version string) error {
	entries, err := loadEntriesFromTx(tx)
	if err != nil {
		return err
	}

	entries = append(entries, migrationEntry{
		Version:   version,
		AppliedAt: time.Now(),
	})

	return saveEntriesToTx(tx, entries)
}

func unmarkApplied(_ context.Context, tx *den.Tx, version string) error {
	entries, err := loadEntriesFromTx(tx)
	if err != nil {
		return err
	}

	filtered := entries[:0]
	for _, e := range entries {
		if e.Version != version {
			filtered = append(filtered, e)
		}
	}

	return saveEntriesToTx(tx, filtered)
}

func loadEntriesFromTx(tx *den.Tx) ([]migrationEntry, error) {
	data, err := den.TxGet(tx, migrationsCollection, "log")
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

func saveEntriesToTx(tx *den.Tx, entries []migrationEntry) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return den.TxPut(tx, migrationsCollection, "log", data)
}
