// SPDX-License-Identifier: MIT

// Package lock defines the option vocabulary for row-level and advisory
// locks used by engine.LockByID, engine.AdvisoryLock, and
// QuerySet.ForUpdate. Backend semantics live in den/backend
// ([backend.LockMode] and its constants); this package layers the
// option-pattern constructors ([SkipLocked], [NoWait]) and a [Resolve]
// helper that turns a slice of options into a single backend.LockMode.
//
// Application code reaches these as den.NoWait, den.SkipLocked, etc. —
// direct imports of this package are only needed when implementing a
// helper that itself takes ...lock.Option.
package lock

import (
	"errors"

	"github.com/oliverandrich/den/backend"
)

// Option configures a lock acquisition (LockByID, ForUpdate). Apply
// options via [Resolve] to derive the effective backend.LockMode.
type Option func(*config)

// config tracks each option independently (rather than collapsing to a
// single mode) so [Resolve] can detect and reject the caller passing both
// SkipLocked and NoWait, which are mutually exclusive in PostgreSQL.
type config struct {
	skipLocked bool
	noWait     bool
}

// SkipLocked makes the lock acquisition return ErrNotFound immediately if
// another transaction already holds the row lock, instead of blocking.
// Maps to PostgreSQL's FOR UPDATE SKIP LOCKED. Useful for queue-consumer
// patterns where each worker should claim a different row without
// contending. On SQLite this option is a no-op.
//
// Because PostgreSQL returns zero rows for both "locked by another tx"
// and "row does not exist", the caller cannot distinguish these cases
// via the error alone.
//
// Passing both SkipLocked and NoWait causes [Resolve] to return an error
// — they are mutually exclusive in PostgreSQL.
func SkipLocked() Option {
	return func(c *config) { c.skipLocked = true }
}

// NoWait makes the lock acquisition return ErrLocked immediately if
// another transaction already holds the row lock, instead of blocking.
// Maps to PostgreSQL's FOR UPDATE NOWAIT. Useful when the caller wants
// to decide between retry, abort, or an alternative code path. On SQLite
// this option is a no-op.
//
// Passing both SkipLocked and NoWait causes [Resolve] to return an error
// — they are mutually exclusive in PostgreSQL.
func NoWait() Option {
	return func(c *config) { c.noWait = true }
}

// Resolve applies the given options and collapses them into a single
// backend.LockMode. Returns an error when the options contradict each
// other (currently only the SkipLocked + NoWait combination).
func Resolve(opts ...Option) (backend.LockMode, error) {
	cfg := config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.skipLocked && cfg.noWait {
		return backend.LockDefault, errors.New("den: SkipLocked and NoWait are mutually exclusive")
	}
	switch {
	case cfg.skipLocked:
		return backend.LockSkipLocked, nil
	case cfg.noWait:
		return backend.LockNoWait, nil
	default:
		return backend.LockDefault, nil
	}
}
