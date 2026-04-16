package den_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
)

type staleDoc struct {
	document.Base
	Name string `json:"name" den:"index"`
}

func TestDropStaleIndexes_DropsOrphan(t *testing.T) {
	ctx := context.Background()
	db := dentest.MustOpen(t, &staleDoc{})

	require.NoError(t, db.Backend().EnsureIndex(ctx, "staledoc", den.IndexDefinition{
		Name: "idx_staledoc_orphan", Fields: []string{"orphan"},
	}))

	result, err := den.DropStaleIndexes(ctx, db)
	require.NoError(t, err)
	require.Len(t, result.Dropped, 1)
	assert.Equal(t, "idx_staledoc_orphan", result.Dropped[0].Name)
	assert.Equal(t, "staledoc", result.Dropped[0].Collection)
	assert.Equal(t, []string{"orphan"}, result.Dropped[0].Fields)

	require.Len(t, result.Kept, 1)
	assert.Equal(t, "idx_staledoc_name", result.Kept[0].Name)

	recorded, err := db.Backend().ListRecordedIndexes(ctx, "staledoc")
	require.NoError(t, err)
	require.Len(t, recorded, 1)
	assert.Equal(t, "idx_staledoc_name", recorded[0].Name)
}

func TestDropStaleIndexes_DryRun(t *testing.T) {
	ctx := context.Background()
	db := dentest.MustOpen(t, &staleDoc{})

	require.NoError(t, db.Backend().EnsureIndex(ctx, "staledoc", den.IndexDefinition{
		Name: "idx_staledoc_orphan", Fields: []string{"orphan"},
	}))

	result, err := den.DropStaleIndexes(ctx, db, den.DryRun())
	require.NoError(t, err)
	require.Len(t, result.Dropped, 1)
	assert.Equal(t, "idx_staledoc_orphan", result.Dropped[0].Name)

	recorded, err := db.Backend().ListRecordedIndexes(ctx, "staledoc")
	require.NoError(t, err)
	require.Len(t, recorded, 2, "DryRun must not actually drop the orphan")
}

func TestDropStaleIndexes_NoStale(t *testing.T) {
	ctx := context.Background()
	db := dentest.MustOpen(t, &staleDoc{})

	result, err := den.DropStaleIndexes(ctx, db)
	require.NoError(t, err)
	assert.Empty(t, result.Dropped)
	require.Len(t, result.Kept, 1)
	assert.Equal(t, "idx_staledoc_name", result.Kept[0].Name)
}
