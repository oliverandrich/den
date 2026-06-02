package engine_test

import (
	"github.com/oliverandrich/den/engine"

	"reflect"
	"testing"

	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type introEager struct {
	document.Base
	D engine.Link[Door] `json:"d" den:"eager"`
}

type introUnreg struct {
	document.Base
	Name string `json:"name"`
}

type introCar struct {
	document.Base
	Brand string `json:"brand"`
}

type introGarage struct {
	document.Base
	Car engine.Link[introCar] `json:"car"`
}

func TestLinkFields_House(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &Window{}, &House{})
	lfs, err := engine.LinkFields[House](db)
	require.NoError(t, err)
	require.Len(t, lfs, 2)

	byName := make(map[string]engine.LinkFieldMeta, len(lfs))
	for _, lf := range lfs {
		byName[lf.JSONName] = lf
	}

	door := byName["door"]
	assert.Equal(t, "Door", door.GoName)
	assert.False(t, door.Slice)
	assert.False(t, door.Eager)
	assert.Equal(t, "door", door.TargetCollection)
	assert.Equal(t, reflect.TypeFor[Door](), door.TargetType)

	win := byName["windows"]
	assert.True(t, win.Slice)
	assert.Equal(t, "window", win.TargetCollection)
	assert.Equal(t, reflect.TypeFor[Window](), win.TargetType)
}

func TestLinkFields_Eager(t *testing.T) {
	db := dentest.MustOpen(t, &Door{}, &introEager{})
	lfs, err := engine.LinkFields[introEager](db)
	require.NoError(t, err)
	require.Len(t, lfs, 1)
	assert.Equal(t, "d", lfs[0].JSONName)
	assert.True(t, lfs[0].Eager)
}

func TestLinkFields_ErrNotRegistered(t *testing.T) {
	db := dentest.MustOpen(t, &Door{})
	_, err := engine.LinkFields[introUnreg](db)
	require.ErrorIs(t, err, engine.ErrNotRegistered)
}

func TestLinkFields_UnregisteredTarget(t *testing.T) {
	db := dentest.MustOpen(t, &introGarage{}) // introCar deliberately not registered
	lfs, err := engine.LinkFields[introGarage](db)
	require.NoError(t, err)
	require.Len(t, lfs, 1)
	assert.Equal(t, "car", lfs[0].JSONName)
	assert.Empty(t, lfs[0].TargetCollection, "unregistered target leaves collection blank, no error")
	assert.Equal(t, reflect.TypeFor[introCar](), lfs[0].TargetType)
}
