package den_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
)

type ParityProduct struct {
	document.Base
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Category string  `json:"category"`
}

type ParityAddress struct {
	City    string `json:"city"`
	Country string `json:"country"`
}

type ParityPerson struct {
	document.Base
	Name    string        `json:"name"`
	Age     int           `json:"age"`
	Address ParityAddress `json:"address"`
}

func parityDBs(t *testing.T) map[string]*den.DB {
	t.Helper()
	return map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &ParityProduct{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParityProduct{}),
	}
}

func parityPersonDBs(t *testing.T) map[string]*den.DB {
	t.Helper()
	return map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &ParityPerson{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParityPerson{}),
	}
}

func TestParity_InsertAndFindByID(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := &ParityProduct{Name: "Widget", Price: 29.99, Category: "A"}
			require.NoError(t, den.Insert(ctx, db, p))
			assert.NotEmpty(t, p.ID)

			found, err := den.FindByID[ParityProduct](ctx, db, p.ID)
			require.NoError(t, err)
			assert.Equal(t, "Widget", found.Name)
			assert.InDelta(t, 29.99, found.Price, 0.001)
		})
	}
}

func TestParity_FindWithFilter(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.InsertMany(ctx, db, []*ParityProduct{
				{Name: "A", Price: 10, Category: "X"},
				{Name: "B", Price: 20, Category: "Y"},
				{Name: "C", Price: 30, Category: "X"},
			}))

			results, err := den.NewQuery[ParityProduct](db, where.Field("category").Eq("X")).All(ctx)
			require.NoError(t, err)
			assert.Len(t, results, 2)
		})
	}
}

func TestParity_FindSortLimitSkip(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.InsertMany(ctx, db, []*ParityProduct{
				{Name: "A", Price: 30},
				{Name: "B", Price: 10},
				{Name: "C", Price: 20},
			}))

			results, err := den.NewQuery[ParityProduct](db).
				Sort("price", den.Asc).
				Limit(2).
				All(ctx)
			require.NoError(t, err)
			require.Len(t, results, 2)
			assert.InDelta(t, 10.0, results[0].Price, 0.001)
			assert.InDelta(t, 20.0, results[1].Price, 0.001)
		})
	}
}

func TestParity_Count(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.InsertMany(ctx, db, []*ParityProduct{
				{Name: "A", Price: 10, Category: "X"},
				{Name: "B", Price: 20, Category: "Y"},
				{Name: "C", Price: 30, Category: "X"},
			}))

			count, err := den.NewQuery[ParityProduct](db, where.Field("category").Eq("X")).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), count)
		})
	}
}

func TestParity_Delete(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := &ParityProduct{Name: "ToDelete", Price: 10}
			require.NoError(t, den.Insert(ctx, db, p))
			require.NoError(t, den.Delete(ctx, db, p))

			_, err := den.FindByID[ParityProduct](ctx, db, p.ID)
			assert.ErrorIs(t, err, den.ErrNotFound)
		})
	}
}

func TestParity_Update(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := &ParityProduct{Name: "Original", Price: 10}
			require.NoError(t, den.Insert(ctx, db, p))

			p.Name = "Updated"
			p.Price = 99
			require.NoError(t, den.Update(ctx, db, p))

			found, err := den.FindByID[ParityProduct](ctx, db, p.ID)
			require.NoError(t, err)
			assert.Equal(t, "Updated", found.Name)
			assert.InDelta(t, 99.0, found.Price, 0.001)
		})
	}
}

func TestParity_FindOne(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.InsertMany(ctx, db, []*ParityProduct{
				{Name: "Alpha", Price: 10},
				{Name: "Beta", Price: 20},
			}))

			p, err := den.NewQuery[ParityProduct](db, where.Field("name").Eq("Beta")).First(ctx)
			require.NoError(t, err)
			assert.Equal(t, "Beta", p.Name)
		})
	}
}

func TestParity_Exists(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.Insert(ctx, db, &ParityProduct{Name: "Exists", Price: 10}))

			exists, err := den.NewQuery[ParityProduct](db, where.Field("name").Eq("Exists")).Exists(ctx)
			require.NoError(t, err)
			assert.True(t, exists)

			exists, err = den.NewQuery[ParityProduct](db, where.Field("name").Eq("Nope")).Exists(ctx)
			require.NoError(t, err)
			assert.False(t, exists)
		})
	}
}

func TestParity_NumericSortOrder(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			// Prices that would sort wrong lexicographically: "9" > "10" > "100"
			require.NoError(t, den.InsertMany(ctx, db, []*ParityProduct{
				{Name: "Cheap", Price: 9},
				{Name: "Mid", Price: 10},
				{Name: "Expensive", Price: 100},
			}))

			results, err := den.NewQuery[ParityProduct](db).Sort("price", den.Asc).All(ctx)
			require.NoError(t, err)
			require.Len(t, results, 3)
			assert.InDelta(t, 9.0, results[0].Price, 0.001)
			assert.InDelta(t, 10.0, results[1].Price, 0.001)
			assert.InDelta(t, 100.0, results[2].Price, 0.001)
		})
	}
}

func TestParity_StringComparison(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.InsertMany(ctx, db, []*ParityProduct{
				{Name: "Alpha", Price: 10},
				{Name: "Beta", Price: 20},
				{Name: "Gamma", Price: 30},
			}))

			// Gt on a string field must not crash (was casting to ::float on PG)
			results, err := den.NewQuery[ParityProduct](db, where.Field("name").Gt("Beta")).All(ctx)
			require.NoError(t, err)
			assert.Len(t, results, 1)
			assert.Equal(t, "Gamma", results[0].Name)

			// Lte on a string field
			results, err = den.NewQuery[ParityProduct](db, where.Field("name").Lte("Beta")).All(ctx)
			require.NoError(t, err)
			assert.Len(t, results, 2)
		})
	}
}

func TestParity_NestedFieldQuery(t *testing.T) {
	for name, db := range parityPersonDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.InsertMany(ctx, db, []*ParityPerson{
				{Name: "Alice", Age: 30, Address: ParityAddress{City: "Berlin", Country: "DE"}},
				{Name: "Bob", Age: 25, Address: ParityAddress{City: "Paris", Country: "FR"}},
				{Name: "Carol", Age: 35, Address: ParityAddress{City: "Berlin", Country: "DE"}},
			}))

			// Query on nested field
			results, err := den.NewQuery[ParityPerson](db, where.Field("address.city").Eq("Berlin")).All(ctx)
			require.NoError(t, err)
			assert.Len(t, results, 2)

			// Sort on nested field
			results, err = den.NewQuery[ParityPerson](db).Sort("address.city", den.Asc).All(ctx)
			require.NoError(t, err)
			require.Len(t, results, 3)
			assert.Equal(t, "Berlin", results[0].Address.City)
			assert.Equal(t, "Berlin", results[1].Address.City)
			assert.Equal(t, "Paris", results[2].Address.City)
		})
	}
}

func TestParity_GroupBySQL(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.InsertMany(ctx, db, []*ParityProduct{
				{Name: "A", Price: 10, Category: "X"},
				{Name: "B", Price: 20, Category: "X"},
				{Name: "C", Price: 30, Category: "Y"},
				{Name: "D", Price: 40, Category: "Y"},
				{Name: "E", Price: 50, Category: "Y"},
			}))

			type CatStats struct {
				Category string  `den:"group_key"`
				Count    int64   `den:"count"`
				AvgPrice float64 `den:"avg:price"`
				Total    float64 `den:"sum:price"`
				MinPrice float64 `den:"min:price"`
				MaxPrice float64 `den:"max:price"`
			}

			var stats []CatStats
			err := den.NewQuery[ParityProduct](db).GroupBy("category").Into(ctx, &stats)
			require.NoError(t, err)
			require.Len(t, stats, 2)

			var x, y *CatStats
			for i := range stats {
				switch stats[i].Category {
				case "X":
					x = &stats[i]
				case "Y":
					y = &stats[i]
				}
			}

			require.NotNil(t, x)
			assert.Equal(t, int64(2), x.Count)
			assert.InDelta(t, 15.0, x.AvgPrice, 0.001)
			assert.InDelta(t, 30.0, x.Total, 0.001)
			assert.InDelta(t, 10.0, x.MinPrice, 0.001)
			assert.InDelta(t, 20.0, x.MaxPrice, 0.001)

			require.NotNil(t, y)
			assert.Equal(t, int64(3), y.Count)
			assert.InDelta(t, 40.0, y.AvgPrice, 0.001)
			assert.InDelta(t, 120.0, y.Total, 0.001)
			assert.InDelta(t, 30.0, y.MinPrice, 0.001)
			assert.InDelta(t, 50.0, y.MaxPrice, 0.001)
		})
	}
}

type ParitySoftProduct struct {
	document.Base
	document.SoftDelete
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

func paritySoftDBs(t *testing.T) map[string]*den.DB {
	t.Helper()
	return map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &ParitySoftProduct{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParitySoftProduct{}),
	}
}

func TestParity_FindOneAndUpdate_MultipleMatches(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.InsertMany(ctx, db, []*ParityProduct{
				{Name: "Widget", Price: 10},
				{Name: "Widget", Price: 20},
			}))

			_, err := den.FindOneAndUpdate[ParityProduct](ctx, db,
				den.SetFields{"price": 99.0},
				[]where.Condition{where.Field("name").Eq("Widget")},
			)
			require.ErrorIs(t, err, den.ErrMultipleMatches)
		})
	}
}

func TestParity_FindOneAndUpsert_Insert(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			doc, inserted, err := den.FindOneAndUpsert[ParityProduct](ctx, db,
				&ParityProduct{Name: "Widget", Price: 1.0, Category: "X"},
				den.SetFields{"price": 5.0},
				[]where.Condition{where.Field("name").Eq("Widget")},
			)
			require.NoError(t, err)
			assert.True(t, inserted)
			assert.InDelta(t, 5.0, doc.Price, 0.001)
			assert.Equal(t, "X", doc.Category)
		})
	}
}

func TestParity_FindOneAndUpsert_Update(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			seed := &ParityProduct{Name: "Widget", Price: 1.0, Category: "X"}
			require.NoError(t, den.Insert(ctx, db, seed))

			doc, inserted, err := den.FindOneAndUpsert[ParityProduct](ctx, db,
				&ParityProduct{Name: "Widget", Price: 999.0},
				den.SetFields{"price": 5.0},
				[]where.Condition{where.Field("name").Eq("Widget")},
			)
			require.NoError(t, err)
			assert.False(t, inserted)
			assert.Equal(t, seed.ID, doc.ID)
			assert.InDelta(t, 5.0, doc.Price, 0.001)
		})
	}
}

func TestParity_FindOneAndUpsert_MultipleMatches(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.InsertMany(ctx, db, []*ParityProduct{
				{Name: "Widget", Price: 10},
				{Name: "Widget", Price: 20},
			}))

			_, _, err := den.FindOneAndUpsert[ParityProduct](ctx, db,
				&ParityProduct{Name: "Widget"},
				den.SetFields{"price": 99.0},
				[]where.Condition{where.Field("name").Eq("Widget")},
			)
			require.ErrorIs(t, err, den.ErrMultipleMatches)
		})
	}
}

func TestParity_FindOneAndUpsert_SoftDeletedSkippedByDefault(t *testing.T) {
	for name, db := range paritySoftDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			original := &ParitySoftProduct{Name: "Widget", Price: 1.0}
			require.NoError(t, den.Insert(ctx, db, original))
			require.NoError(t, den.Delete(ctx, db, original))

			doc, inserted, err := den.FindOneAndUpsert[ParitySoftProduct](ctx, db,
				&ParitySoftProduct{Name: "Widget", Price: 10.0},
				den.SetFields{"price": 20.0},
				[]where.Condition{where.Field("name").Eq("Widget")},
			)
			require.NoError(t, err)
			assert.True(t, inserted)
			assert.NotEqual(t, original.ID, doc.ID)
		})
	}
}

func TestParity_FindOneAndUpsert_IncludeSoftDeleted(t *testing.T) {
	for name, db := range paritySoftDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			original := &ParitySoftProduct{Name: "Widget", Price: 1.0}
			require.NoError(t, den.Insert(ctx, db, original))
			require.NoError(t, den.Delete(ctx, db, original))

			doc, inserted, err := den.FindOneAndUpsert[ParitySoftProduct](ctx, db,
				&ParitySoftProduct{Name: "Widget", Price: 10.0},
				den.SetFields{"price": 20.0},
				[]where.Condition{where.Field("name").Eq("Widget")},
				den.IncludeSoftDeleted(),
			)
			require.NoError(t, err)
			assert.False(t, inserted)
			assert.Equal(t, original.ID, doc.ID)
			assert.InDelta(t, 20.0, doc.Price, 0.001)
		})
	}
}

type ParityValidated struct {
	document.Base
	Name string `json:"name"`
}

func (v *ParityValidated) Validate() error {
	if v.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func parityValidatedDBs(t *testing.T) map[string]*den.DB {
	t.Helper()
	return map[string]*den.DB{
		"sqlite":   dentest.MustOpen(t, &ParityValidated{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParityValidated{}),
	}
}

func TestParity_InsertMany_PreValidate_FailsAtEnd(t *testing.T) {
	for name, db := range parityValidatedDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			docs := []*ParityValidated{{Name: "A"}, {Name: "B"}, {Name: ""}}
			err := den.InsertMany(ctx, db, docs, den.PreValidate())
			require.ErrorIs(t, err, den.ErrValidation)

			count, err := den.NewQuery[ParityValidated](db).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(0), count, "no document is written when pre-validation fails")
		})
	}
}

func TestParity_NumericEqConsistency(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, den.InsertMany(ctx, db, []*ParityProduct{
				{Name: "A", Price: 10},
				{Name: "B", Price: 20},
			}))

			// Eq with numeric value must match correctly
			results, err := den.NewQuery[ParityProduct](db, where.Field("price").Eq(float64(10))).All(ctx)
			require.NoError(t, err)
			assert.Len(t, results, 1)
			assert.Equal(t, "A", results[0].Name)
		})
	}
}
