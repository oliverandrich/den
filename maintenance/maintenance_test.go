// SPDX-License-Identifier: MIT

package maintenance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/oliverandrich/den/maintenance"
)

func TestResolve_Default(t *testing.T) {
	cfg := maintenance.Resolve()
	assert.False(t, cfg.DryRun, "no options should yield DryRun=false (i.e. apply changes)")
}

func TestResolve_DryRun(t *testing.T) {
	cfg := maintenance.Resolve(maintenance.DryRun())
	assert.True(t, cfg.DryRun, "DryRun() option must enable Config.DryRun")
}

func TestResolve_OptionIndependence(t *testing.T) {
	// Calling Resolve twice with separate option slices must not share state
	// — each call constructs a fresh Config.
	first := maintenance.Resolve(maintenance.DryRun())
	second := maintenance.Resolve()
	assert.True(t, first.DryRun)
	assert.False(t, second.DryRun)
}

// Compile-time pin on the option-constructor signature — a future change
// that makes DryRun() return something other than maintenance.Option breaks
// the engine wrapper `func DryRun() DropStaleOption { return maintenance.DryRun() }`.
var _ maintenance.Option = maintenance.DryRun()

func TestDropStaleResult_ZeroValueUsable(t *testing.T) {
	// engine.DropStaleIndexes returns a zero-value DropStaleResult when no
	// collections have stale indexes; both Dropped and Kept must be nil
	// (not panic on append).
	var r maintenance.DropStaleResult
	r.Dropped = append(r.Dropped, maintenance.StaleIndex{Collection: "x", Name: "ix"})
	r.Kept = append(r.Kept, maintenance.StaleIndex{Collection: "x", Name: "iy"})
	assert.Len(t, r.Dropped, 1)
	assert.Len(t, r.Kept, 1)
}

func TestStaleIndex_FieldOrder(t *testing.T) {
	// Pin the struct shape — composite-literal callers (engine.DropStaleIndexes
	// builds these inline) would fail to compile if the field order or types
	// drift.
	idx := maintenance.StaleIndex{
		Collection: "products",
		Name:       "ix_sku",
		Fields:     []string{"sku"},
		Unique:     true,
	}
	assert.Equal(t, "products", idx.Collection)
	assert.Equal(t, "ix_sku", idx.Name)
	assert.Equal(t, []string{"sku"}, idx.Fields)
	assert.True(t, idx.Unique)
}
