// SPDX-License-Identifier: MIT

// Package maintenance defines the data shapes and option vocabulary for
// den's admin-side operations — notably engine.DropStaleIndexes, the
// post-schema-change cleanup that drops indexes Den previously created
// but no longer recognises.
//
// Application code reaches these as den.DropStaleIndexes, den.DryRun,
// den.StaleIndex, etc. Direct imports of this package are useful when
// writing helper tooling that itself takes ...maintenance.Option or
// returns a maintenance.DropStaleResult.
package maintenance

// Option configures DropStaleIndexes. Apply options via [Resolve] to
// derive the effective [Config].
type Option func(*Config)

// Config holds the resolved option state for DropStaleIndexes. Exported so
// engine.DropStaleIndexes (and any future tooling) can read the flags it
// produced via [Resolve].
type Config struct {
	DryRun bool
}

// DryRun causes DropStaleIndexes to report the indexes that would be
// dropped without actually dropping them.
func DryRun() Option {
	return func(c *Config) { c.DryRun = true }
}

// Resolve applies the given options and returns the resulting [Config].
func Resolve(opts ...Option) Config {
	cfg := Config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// DropStaleResult summarizes a DropStaleIndexes call.
// Dropped contains the indexes that were (or would be, under DryRun)
// removed. Kept contains indexes that are still referenced by a current
// IndexDefinition.
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
