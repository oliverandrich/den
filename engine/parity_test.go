package engine_test

import (
	"github.com/oliverandrich/den/engine"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func parityDBs(t *testing.T) map[string]*engine.DB {
	t.Helper()
	return map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &ParityProduct{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParityProduct{}),
	}
}

func parityPersonDBs(t *testing.T) map[string]*engine.DB {
	t.Helper()
	return map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &ParityPerson{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParityPerson{}),
	}
}

func TestParity_InsertAndFindByID(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := &ParityProduct{Name: "Widget", Price: 29.99, Category: "A"}
			require.NoError(t, engine.Save(ctx, db, p))
			assert.NotEmpty(t, p.ID)

			found, err := engine.FindByID[ParityProduct](ctx, db, p.ID)
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
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "A", Price: 10, Category: "X"},
				{Name: "B", Price: 20, Category: "Y"},
				{Name: "C", Price: 30, Category: "X"},
			}))

			results, err := engine.NewQuery[ParityProduct](db, where.Field("category").Eq("X")).All(ctx)
			require.NoError(t, err)
			assert.Len(t, results, 2)
		})
	}
}

func TestParity_FindSortLimitSkip(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "A", Price: 30},
				{Name: "B", Price: 10},
				{Name: "C", Price: 20},
			}))

			results, err := engine.NewQuery[ParityProduct](db).
				Sort("price", engine.Asc).
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
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "A", Price: 10, Category: "X"},
				{Name: "B", Price: 20, Category: "Y"},
				{Name: "C", Price: 30, Category: "X"},
			}))

			count, err := engine.NewQuery[ParityProduct](db, where.Field("category").Eq("X")).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), count)
		})
	}
}

// Pins fix for den-qrg2: mixing where.Or with a sibling field predicate at the
// top level must AND-compose. Without proper parenthesisation, SQL precedence
// (AND > OR) silently drops the sibling for the OR branch.
func TestParity_OrAndComposition(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			// Data chosen so AND > OR precedence visibly breaks the query:
			// Or(name=A, name=B) AND category=X correctly matches only the
			// two X rows. Under broken precedence (A OR (B AND X)), the third
			// row with name=A but category=Y also matches, yielding 3.
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "A", Price: 10, Category: "X"},
				{Name: "B", Price: 20, Category: "X"},
				{Name: "A", Price: 30, Category: "Y"},
			}))

			c1, err := engine.NewQuery[ParityProduct](db, where.Or(
				where.Field("name").Eq("A"),
				where.Field("name").Eq("B"),
			)).Where(where.Field("category").Eq("X")).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c1, "Or + chained Where must AND-compose")

			c2, err := engine.NewQuery[ParityProduct](db,
				where.Or(where.Field("name").Eq("A"), where.Field("name").Eq("B")),
				where.Field("category").Eq("X"),
			).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c2, "variadic Or + Eq must AND-compose")

			c3, err := engine.NewQuery[ParityProduct](db, where.And(
				where.Or(where.Field("name").Eq("A"), where.Field("name").Eq("B")),
				where.Field("category").Eq("X"),
			)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c3, "explicit And-wrap baseline")
		})
	}
}

func TestParity_Delete(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := &ParityProduct{Name: "ToDelete", Price: 10}
			require.NoError(t, engine.Save(ctx, db, p))
			require.NoError(t, engine.Delete(ctx, db, p))

			_, err := engine.FindByID[ParityProduct](ctx, db, p.ID)
			assert.ErrorIs(t, err, engine.ErrNotFound)
		})
	}
}

func TestParity_Update(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := &ParityProduct{Name: "Original", Price: 10}
			require.NoError(t, engine.Save(ctx, db, p))

			p.Name = "Updated"
			p.Price = 99
			require.NoError(t, engine.Save(ctx, db, p))

			found, err := engine.FindByID[ParityProduct](ctx, db, p.ID)
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
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "Alpha", Price: 10},
				{Name: "Beta", Price: 20},
			}))

			p, err := engine.NewQuery[ParityProduct](db, where.Field("name").Eq("Beta")).First(ctx)
			require.NoError(t, err)
			assert.Equal(t, "Beta", p.Name)
		})
	}
}

func TestParity_Exists(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.Save(ctx, db, &ParityProduct{Name: "Exists", Price: 10}))

			exists, err := engine.NewQuery[ParityProduct](db, where.Field("name").Eq("Exists")).Exists(ctx)
			require.NoError(t, err)
			assert.True(t, exists)

			exists, err = engine.NewQuery[ParityProduct](db, where.Field("name").Eq("Nope")).Exists(ctx)
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
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "Cheap", Price: 9},
				{Name: "Mid", Price: 10},
				{Name: "Expensive", Price: 100},
			}))

			results, err := engine.NewQuery[ParityProduct](db).Sort("price", engine.Asc).All(ctx)
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
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "Alpha", Price: 10},
				{Name: "Beta", Price: 20},
				{Name: "Gamma", Price: 30},
			}))

			// Gt on a string field must not crash (was casting to ::float on PG)
			results, err := engine.NewQuery[ParityProduct](db, where.Field("name").Gt("Beta")).All(ctx)
			require.NoError(t, err)
			assert.Len(t, results, 1)
			assert.Equal(t, "Gamma", results[0].Name)

			// Lte on a string field
			results, err = engine.NewQuery[ParityProduct](db, where.Field("name").Lte("Beta")).All(ctx)
			require.NoError(t, err)
			assert.Len(t, results, 2)
		})
	}
}

func TestParity_NestedFieldQuery(t *testing.T) {
	for name, db := range parityPersonDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityPerson{
				{Name: "Alice", Age: 30, Address: ParityAddress{City: "Berlin", Country: "DE"}},
				{Name: "Bob", Age: 25, Address: ParityAddress{City: "Paris", Country: "FR"}},
				{Name: "Carol", Age: 35, Address: ParityAddress{City: "Berlin", Country: "DE"}},
			}))

			// Query on nested field
			results, err := engine.NewQuery[ParityPerson](db, where.Field("address.city").Eq("Berlin")).All(ctx)
			require.NoError(t, err)
			assert.Len(t, results, 2)

			// Sort on nested field
			results, err = engine.NewQuery[ParityPerson](db).Sort("address.city", engine.Asc).All(ctx)
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
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
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
			err := engine.NewQuery[ParityProduct](db).GroupBy("category").Into(ctx, &stats)
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

type ParityRegionProduct struct {
	document.Base
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Category string  `json:"category"`
	Region   string  `json:"region"`
}

func parityRegionDBs(t *testing.T) map[string]*engine.DB {
	t.Helper()
	return map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &ParityRegionProduct{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParityRegionProduct{}),
	}
}

// TestParity_GroupBy_SortAndLimit pins ORDER BY (by group key and by
// aggregate) together with LIMIT on grouped results across both backends.
func TestParity_GroupBy_SortAndLimit(t *testing.T) {
	for name, db := range paritySoftDBs(t) {
		// paritySoftDBs already seeds a ParitySoftProduct type with Name
		// and Price — adequate for single-key GroupBy-with-sort.
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.SaveAll(ctx, db, []*ParitySoftProduct{
				{Name: "A", Price: 10},
				{Name: "A", Price: 20},
				{Name: "B", Price: 30},
				{Name: "C", Price: 40},
				{Name: "C", Price: 50},
				{Name: "C", Price: 60},
			}))

			type Stats struct {
				Name  string `den:"group_key"`
				Count int64  `den:"count"`
			}

			// Top-2 by COUNT(*) DESC — expect C (3 rows) then A (2 rows).
			var top []Stats
			err := engine.NewQuery[ParitySoftProduct](db).Limit(2).
				GroupBy("name").
				OrderByAgg(engine.OpCount, "", engine.Desc).
				Into(ctx, &top)
			require.NoError(t, err)
			require.Len(t, top, 2)
			assert.Equal(t, "C", top[0].Name)
			assert.Equal(t, int64(3), top[0].Count)
			assert.Equal(t, "A", top[1].Name)
			assert.Equal(t, int64(2), top[1].Count)

			// Sort by group key ascending, full result.
			var asc []Stats
			err = engine.NewQuery[ParitySoftProduct](db).Sort("name", engine.Asc).
				GroupBy("name").Into(ctx, &asc)
			require.NoError(t, err)
			require.Len(t, asc, 3)
			assert.Equal(t, "A", asc[0].Name)
			assert.Equal(t, "B", asc[1].Name)
			assert.Equal(t, "C", asc[2].Name)
		})
	}
}

// TestParity_GroupBy_MultiKey pins two-key GROUP BY on both backends.
func TestParity_GroupBy_MultiKey(t *testing.T) {
	for name, db := range parityRegionDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityRegionProduct{
				{Name: "a", Price: 10, Category: "X", Region: "north"},
				{Name: "b", Price: 20, Category: "X", Region: "north"},
				{Name: "c", Price: 30, Category: "X", Region: "south"},
				{Name: "d", Price: 40, Category: "Y", Region: "north"},
			}))

			type Stats struct {
				Category string  `den:"group_key:0"`
				Region   string  `den:"group_key:1"`
				Count    int64   `den:"count"`
				Total    float64 `den:"sum:price"`
			}

			var stats []Stats
			err := engine.NewQuery[ParityRegionProduct](db).GroupBy("category", "region").Into(ctx, &stats)
			require.NoError(t, err)
			require.Len(t, stats, 3)

			byKey := map[string]Stats{}
			for _, s := range stats {
				byKey[s.Category+"|"+s.Region] = s
			}
			assert.Equal(t, int64(2), byKey["X|north"].Count)
			assert.InDelta(t, 30.0, byKey["X|north"].Total, 0.001)
			assert.Equal(t, int64(1), byKey["X|south"].Count)
			assert.Equal(t, int64(1), byKey["Y|north"].Count)
		})
	}
}

func TestParity_GroupBy_NullAggregateValue(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			// Seed three rows in one category; none carry a "discount" field
			// (ParityProduct does not define one), so json_extract / jsonb_path_text
			// return NULL for every row and the SUM/AVG/MIN/MAX aggregates evaluate
			// to SQL NULL. The scanner contract maps that to exactly 0.0.
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "A", Price: 10, Category: "X"},
				{Name: "B", Price: 20, Category: "X"},
				{Name: "C", Price: 30, Category: "X"},
			}))

			// Aggregate over a deliberately missing field on ParityProduct.
			type StatsOverMissingField struct {
				Category    string  `den:"group_key"`
				Count       int64   `den:"count"`
				SumDiscount float64 `den:"sum:discount"`
				AvgDiscount float64 `den:"avg:discount"`
				MinDiscount float64 `den:"min:discount"`
				MaxDiscount float64 `den:"max:discount"`
			}

			var stats []StatsOverMissingField
			err := engine.NewQuery[ParityProduct](db).GroupBy("category").Into(ctx, &stats)
			require.NoError(t, err)
			require.Len(t, stats, 1)
			assert.Equal(t, "X", stats[0].Category)
			assert.Equal(t, int64(3), stats[0].Count)
			// Delta of 0 means exact equality — pins the NULL→0.0 contract
			// without testifylint's float-compare false positive.
			assert.InDelta(t, 0.0, stats[0].SumDiscount, 0, "SUM over NULLs → exactly 0.0")
			assert.InDelta(t, 0.0, stats[0].AvgDiscount, 0)
			assert.InDelta(t, 0.0, stats[0].MinDiscount, 0)
			assert.InDelta(t, 0.0, stats[0].MaxDiscount, 0)
		})
	}
}

func TestParity_GroupBy_ZeroMatches(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "A", Price: 10, Category: "X"},
				{Name: "B", Price: 20, Category: "X"},
			}))

			type CountOnlyCatStats struct {
				Category string `den:"group_key"`
				Count    int64  `den:"count"`
			}

			var stats []CountOnlyCatStats
			err := engine.NewQuery[ParityProduct](db, where.Field("category").Eq("Z")).
				GroupBy("category").Into(ctx, &stats)
			require.NoError(t, err)
			assert.Empty(t, stats, "no matching rows → empty result, no synthetic zero group")
		})
	}
}

func TestParity_StringContains_EscapesSpecialChars(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			// Seed names containing each LIKE special character literally so a
			// search for the same character must match exactly the seeded row.
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "100% off_sale", Price: 1, Category: "X"},
				{Name: `back\slash`, Price: 2, Category: "X"},
				{Name: "plain text", Price: 3, Category: "X"},
			}))

			cases := []struct {
				query string
				want  string
			}{
				{"%", "100% off_sale"},
				{"_", "100% off_sale"},
				{`\`, `back\slash`},
			}
			for _, tc := range cases {
				results, err := engine.NewQuery[ParityProduct](db,
					where.Field("name").StringContains(tc.query)).All(ctx)
				require.NoError(t, err)
				require.Len(t, results, 1, "search %q must match exactly one seeded row", tc.query)
				assert.Equal(t, tc.want, results[0].Name)
			}
		})
	}
}

func TestParity_StringContains_Unicode(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "café noir", Price: 1, Category: "X"},
				{Name: "日本語サンプル", Price: 2, Category: "X"},
				{Name: "party 🎉 time", Price: 3, Category: "X"},
				{Name: "plain ascii", Price: 4, Category: "X"},
			}))

			cases := []struct {
				query string
				want  string
			}{
				{"café", "café noir"},
				{"日本", "日本語サンプル"},
				{"🎉", "party 🎉 time"},
			}
			for _, tc := range cases {
				results, err := engine.NewQuery[ParityProduct](db,
					where.Field("name").StringContains(tc.query)).All(ctx)
				require.NoError(t, err)
				require.Len(t, results, 1, "multi-byte query %q must match exactly one row", tc.query)
				assert.Equal(t, tc.want, results[0].Name)
			}
		})
	}
}

type ParitySoftProduct struct {
	document.Base
	document.SoftDelete
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

func paritySoftDBs(t *testing.T) map[string]*engine.DB {
	t.Helper()
	return map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &ParitySoftProduct{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParitySoftProduct{}),
	}
}

func TestParity_FindOneAndUpdate_MultipleMatches(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "Widget", Price: 10},
				{Name: "Widget", Price: 20},
			}))

			_, err := engine.NewQuery[ParityProduct](db, where.Field("name").Eq("Widget")).UpdateOne(ctx, engine.SetFields{"price": 99.0})
			require.ErrorIs(t, err, engine.ErrMultipleMatches)
		})
	}
}

func TestParity_FindOneAndUpsert_Insert(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			doc, inserted, err := engine.NewQuery[ParityProduct](db, where.Field("name").Eq("Widget")).UpsertOne(ctx, &ParityProduct{Name: "Widget", Price: 1.0, Category: "X"}, engine.SetFields{"price": 5.0})
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
			require.NoError(t, engine.Save(ctx, db, seed))

			doc, inserted, err := engine.NewQuery[ParityProduct](db, where.Field("name").Eq("Widget")).UpsertOne(ctx, &ParityProduct{Name: "Widget", Price: 999.0}, engine.SetFields{"price": 5.0})
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
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "Widget", Price: 10},
				{Name: "Widget", Price: 20},
			}))

			_, _, err := engine.NewQuery[ParityProduct](db, where.Field("name").Eq("Widget")).UpsertOne(ctx, &ParityProduct{Name: "Widget"}, engine.SetFields{"price": 99.0})
			require.ErrorIs(t, err, engine.ErrMultipleMatches)
		})
	}
}

func TestParity_FindOneAndUpsert_SoftDeletedSkippedByDefault(t *testing.T) {
	for name, db := range paritySoftDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			original := &ParitySoftProduct{Name: "Widget", Price: 1.0}
			require.NoError(t, engine.Save(ctx, db, original))
			require.NoError(t, engine.Delete(ctx, db, original))

			doc, inserted, err := engine.NewQuery[ParitySoftProduct](db, where.Field("name").Eq("Widget")).UpsertOne(ctx, &ParitySoftProduct{Name: "Widget", Price: 10.0}, engine.SetFields{"price": 20.0})
			require.NoError(t, err)
			assert.True(t, inserted)
			assert.NotEqual(t, original.ID, doc.ID)
		})
	}
}

type ParitySoftRevProduct struct {
	document.Base
	document.SoftDelete
	Name string `json:"name"`
}

func (p ParitySoftRevProduct) DenSettings() engine.Settings {
	return engine.Settings{UseRevision: true}
}

func paritySoftRevDBs(t *testing.T) map[string]*engine.DB {
	t.Helper()
	return map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &ParitySoftRevProduct{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParitySoftRevProduct{}),
	}
}

// TestParity_SoftDelete_BumpsRevision confirms both backends bump _rev on
// soft-delete so the revision chain stays consistent across Delete and
// Update.
func TestParity_SoftDelete_BumpsRevision(t *testing.T) {
	for name, db := range paritySoftRevDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := &ParitySoftRevProduct{Name: "v1"}
			require.NoError(t, engine.Save(ctx, db, p))
			revInsert := p.Rev

			require.NoError(t, engine.Delete(ctx, db, p))
			assert.NotEqual(t, revInsert, p.Rev, "soft-delete must bump Rev")
		})
	}
}

// TestParity_SoftDelete_AuditFields confirms DeletedBy and DeleteReason are
// recorded on the persisted document on both backends.
func TestParity_SoftDelete_AuditFields(t *testing.T) {
	for name, db := range paritySoftDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := &ParitySoftProduct{Name: "Widget"}
			require.NoError(t, engine.Save(ctx, db, p))
			require.NoError(t, engine.Delete(ctx, db, p,
				engine.SoftDeleteBy("usr_42"),
				engine.SoftDeleteReason("cleanup"),
			))

			found, err := engine.FindByID[ParitySoftProduct](ctx, db, p.ID)
			require.NoError(t, err)
			assert.True(t, found.IsDeleted())
			assert.Equal(t, "usr_42", found.DeletedBy)
			assert.Equal(t, "cleanup", found.DeleteReason)
		})
	}
}

// TestParity_SoftDelete_ConcurrentUpdateConflicts confirms on both backends
// that a stale Update after a concurrent soft-delete fails with
// ErrRevisionConflict instead of clobbering DeletedAt.
func TestParity_SoftDelete_ConcurrentUpdateConflicts(t *testing.T) {
	for name, db := range paritySoftRevDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := &ParitySoftRevProduct{Name: "v1"}
			require.NoError(t, engine.Save(ctx, db, p))

			a, err := engine.FindByID[ParitySoftRevProduct](ctx, db, p.ID)
			require.NoError(t, err)
			b, err := engine.FindByID[ParitySoftRevProduct](ctx, db, p.ID)
			require.NoError(t, err)

			require.NoError(t, engine.Delete(ctx, db, a))

			b.Name = "clobber"
			err = engine.Save(ctx, db, b)
			require.ErrorIs(t, err, engine.ErrRevisionConflict)

			found, err := engine.FindByID[ParitySoftRevProduct](ctx, db, p.ID)
			require.NoError(t, err)
			assert.True(t, found.IsDeleted())
			assert.Equal(t, "v1", found.Name)
		})
	}
}

func TestParity_FindOneAndUpsert_IncludeDeleted(t *testing.T) {
	for name, db := range paritySoftDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			original := &ParitySoftProduct{Name: "Widget", Price: 1.0}
			require.NoError(t, engine.Save(ctx, db, original))
			require.NoError(t, engine.Delete(ctx, db, original))

			doc, inserted, err := engine.NewQuery[ParitySoftProduct](db, where.Field("name").Eq("Widget")).
				IncludeDeleted().
				UpsertOne(ctx, &ParitySoftProduct{Name: "Widget", Price: 10.0}, engine.SetFields{"price": 20.0})
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

func (v *ParityValidated) Validate(_ context.Context) error {
	if v.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func parityValidatedDBs(t *testing.T) map[string]*engine.DB {
	t.Helper()
	return map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &ParityValidated{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParityValidated{}),
	}
}

func TestParity_SaveAll_ValidationFailureRollsBack(t *testing.T) {
	for name, db := range parityValidatedDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			docs := []*ParityValidated{{Name: "A"}, {Name: "B"}, {Name: ""}}
			err := engine.SaveAll(ctx, db, docs)
			require.ErrorIs(t, err, engine.ErrValidation)

			count, err := engine.NewQuery[ParityValidated](db).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(0), count, "the transaction rolls back when any doc fails validation")
		})
	}
}

func TestParity_QuerySetUpdate_HonorsCtxCancellation(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			docs := []*ParityProduct{
				{Name: "a", Price: 1, Category: "bulk"},
				{Name: "b", Price: 2, Category: "bulk"},
				{Name: "c", Price: 3, Category: "bulk"},
				{Name: "d", Price: 4, Category: "bulk"},
				{Name: "e", Price: 5, Category: "bulk"},
			}
			require.NoError(t, engine.SaveAll(ctx, db, docs))

			cancelCtx, cancel := context.WithCancel(context.Background())
			cancel()

			count, err := engine.NewQuery[ParityProduct](db, where.Field("category").Eq("bulk")).
				Update(cancelCtx, engine.SetFields{"category": "updated"})
			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, int64(0), count)

			remaining, err := engine.NewQuery[ParityProduct](db, where.Field("category").Eq("updated")).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(0), remaining, "batch tx rolled back on cancellation")
		})
	}
}

func TestParity_Iter_HonorsCtxCancellation(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			docs := []*ParityProduct{
				{Name: "a", Price: 1.0, Category: "X"},
				{Name: "b", Price: 2.0, Category: "X"},
				{Name: "c", Price: 3.0, Category: "X"},
				{Name: "d", Price: 4.0, Category: "X"},
				{Name: "e", Price: 5.0, Category: "X"},
			}
			require.NoError(t, engine.SaveAll(ctx, db, docs))

			iterCtx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var (
				seen    int
				lastErr error
			)
			for _, err := range engine.NewQuery[ParityProduct](db).Iter(iterCtx) {
				if err != nil {
					lastErr = err
					break
				}
				seen++
				if seen == 1 {
					cancel()
				}
			}

			require.ErrorIs(t, lastErr, context.Canceled)
			assert.Equal(t, 1, seen, "exactly one row yields before the per-row check fires")
		})
	}
}

func TestParity_NumericEqConsistency(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "A", Price: 10},
				{Name: "B", Price: 20},
			}))

			// Eq with numeric value must match correctly
			results, err := engine.NewQuery[ParityProduct](db, where.Field("price").Eq(float64(10))).All(ctx)
			require.NoError(t, err)
			assert.Len(t, results, 1)
			assert.Equal(t, "A", results[0].Name)
		})
	}
}

// TestParity_GroupBy_InTransaction exercises both backends'
// transaction-scoped GroupBy path. The non-tx GroupBy is well covered
// by aggregate_test.go but every GroupBy there runs against a *DB —
// the *Tx code path on both backends had no coverage at all.
func TestParity_GroupBy_InTransaction(t *testing.T) {
	type CatStats struct {
		Category string  `den:"group_key"`
		Count    int64   `den:"count"`
		Total    float64 `den:"sum:price"`
	}

	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.SaveAll(ctx, db, []*ParityProduct{
				{Name: "A", Price: 10, Category: "X"},
				{Name: "B", Price: 20, Category: "X"},
				{Name: "C", Price: 30, Category: "Y"},
				{Name: "D", Price: 40, Category: "Y"},
				{Name: "E", Price: 50, Category: "Y"},
			}))

			var stats []CatStats
			err := engine.RunInTransaction(ctx, db, func(tx *engine.Tx) error {
				return engine.NewQuery[ParityProduct](tx).GroupBy("category").Into(ctx, &stats)
			})
			require.NoError(t, err)
			require.Len(t, stats, 2)

			byCat := make(map[string]CatStats, len(stats))
			for _, s := range stats {
				byCat[s.Category] = s
			}
			assert.Equal(t, int64(2), byCat["X"].Count)
			assert.InDelta(t, 30.0, byCat["X"].Total, 0.001)
			assert.Equal(t, int64(3), byCat["Y"].Count)
			assert.InDelta(t, 120.0, byCat["Y"].Total, 0.001)
		})
	}
}

type ParityEvent struct {
	document.Base
	Name        string     `json:"name"`
	StartsAt    time.Time  `json:"starts_at"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
}

func parityEventDBs(t *testing.T) map[string]*engine.DB {
	t.Helper()
	return map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &ParityEvent{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParityEvent{}),
	}
}

// Pins fix for den-w3g0: comparison operators bound with a Go time.Time
// must match the RFC3339Nano JSON storage encoding on both backends. The
// SQLite backend previously bound time.Time via modernc.org/sqlite's
// "YYYY-MM-DD HH:MM:SS..." default, which lexicographically mismatched
// the stored "...T...Z" form and silently returned zero rows.
func TestParity_TimeComparisons(t *testing.T) {
	for name, db := range parityEventDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			base := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
			past := base.Add(-time.Hour)
			future := base.Add(time.Hour)

			require.NoError(t, engine.SaveAll(ctx, db, []*ParityEvent{
				{Name: "past", StartsAt: past, ScheduledAt: &past},
				{Name: "base", StartsAt: base, ScheduledAt: &base},
				{Name: "future", StartsAt: future, ScheduledAt: &future},
			}))

			c, err := engine.NewQuery[ParityEvent](db, where.Field("starts_at").Lte(base)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c, "Lte(time.Time)")

			c, err = engine.NewQuery[ParityEvent](db, where.Field("starts_at").Lt(base)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(1), c, "Lt(time.Time)")

			c, err = engine.NewQuery[ParityEvent](db, where.Field("starts_at").Gte(base)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c, "Gte(time.Time)")

			c, err = engine.NewQuery[ParityEvent](db, where.Field("starts_at").Gt(base)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(1), c, "Gt(time.Time)")

			c, err = engine.NewQuery[ParityEvent](db, where.Field("starts_at").Eq(base)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(1), c, "Eq(time.Time)")

			c, err = engine.NewQuery[ParityEvent](db, where.Field("starts_at").Ne(base)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c, "Ne(time.Time)")

			c, err = engine.NewQuery[ParityEvent](db, where.Field("starts_at").In(past, future)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c, "In(time.Time...)")

			c, err = engine.NewQuery[ParityEvent](db, where.Field("starts_at").NotIn(base)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c, "NotIn(time.Time...)")

			// Same contract for *time.Time fields with non-nil pointer values.
			c, err = engine.NewQuery[ParityEvent](db, where.Field("scheduled_at").Lte(base)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c, "Lte(time.Time) against *time.Time field")

			// Zero time must round-trip — storage emits "0001-01-01T00:00:00Z",
			// the bind path must produce the same string.
			z := &ParityEvent{Name: "zero", StartsAt: time.Time{}}
			require.NoError(t, engine.Save(ctx, db, z))
			c, err = engine.NewQuery[ParityEvent](db, where.Field("starts_at").Eq(time.Time{})).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(1), c, "Eq(zero time.Time)")
		})
	}
}

type ParityBlob struct {
	document.Base
	Name string `json:"name"`
	Hash []byte `json:"hash,omitempty"`
}

func parityBlobDBs(t *testing.T) map[string]*engine.DB {
	t.Helper()
	return map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &ParityBlob{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ParityBlob{}),
	}
}

// Pins fix for den-4l2y: comparison operators on []byte fields must match
// the base64-encoded JSON storage on both backends. The SQLite backend
// previously bound []byte as raw BLOB, lexicographically mismatching the
// stored base64 string and silently returning zero rows.
//
// Only Eq/Ne/In/NotIn are pinned: ordering comparisons (Gt/Gte/Lt/Lte) on
// []byte degenerate to lexicographic base64 string compare — semantically
// meaningless to users — but they still flow through the same formatBindArg
// plumbing pinned by TestParity_TimeComparisons.
func TestParity_BytesComparisons(t *testing.T) {
	for name, db := range parityBlobDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			h1 := []byte{0x01, 0x02, 0x03}
			h2 := []byte{0xde, 0xad, 0xbe, 0xef}
			h3 := []byte{0xff}

			require.NoError(t, engine.SaveAll(ctx, db, []*ParityBlob{
				{Name: "a", Hash: h1},
				{Name: "b", Hash: h2},
				{Name: "c", Hash: h3},
			}))

			c, err := engine.NewQuery[ParityBlob](db, where.Field("hash").Eq(h1)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(1), c, "Eq([]byte)")

			c, err = engine.NewQuery[ParityBlob](db, where.Field("hash").Ne(h1)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c, "Ne([]byte)")

			c, err = engine.NewQuery[ParityBlob](db, where.Field("hash").In(h1, h2)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c, "In([]byte...)")

			c, err = engine.NewQuery[ParityBlob](db, where.Field("hash").NotIn(h3)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(2), c, "NotIn([]byte...)")
		})
	}
}

// Pins fix for den-w3g0: a value saved in a non-UTC location must be queryable
// with a same-zoned time.Time. encoding/json preserves the original location
// in the stored RFC3339Nano string; the bind path must too.
func TestParity_TimeComparisons_NonUTCZone(t *testing.T) {
	for name, db := range parityEventDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			berlin, err := time.LoadLocation("Europe/Berlin")
			require.NoError(t, err)
			ts := time.Date(2026, 1, 15, 13, 0, 0, 0, berlin)
			require.NoError(t, engine.Save(ctx, db, &ParityEvent{Name: "berlin", StartsAt: ts}))

			c, err := engine.NewQuery[ParityEvent](db, where.Field("starts_at").Eq(ts)).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(1), c, "Eq(zoned time.Time) against same-zone storage")
		})
	}
}

// TestParity_QuerySetDelete_LargeBatch pins that QuerySet.Delete works for
// multi-row batches on both backends. pgx's default row buffer is ~50;
// 200 docs forces multiple fetch round-trips mid-iteration, which is the
// scenario that surfaced "conn busy" on the old DeleteMany path (cursor
// pinned while in-loop writes ran on the same connection). The drain-
// first pattern in QuerySet.Delete avoids that — this test pins the fix.
func TestParity_QuerySetDelete_LargeBatch(t *testing.T) {
	for name, db := range parityDBs(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			const N = 200
			docs := make([]*ParityProduct, N)
			for i := range N {
				docs[i] = &ParityProduct{Name: fmt.Sprintf("p%03d", i), Price: float64(i)}
			}
			require.NoError(t, engine.SaveAll(ctx, db, docs))

			count, err := engine.NewQuery[ParityProduct](db,
				where.Field("price").Lt(150.0),
			).Delete(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(150), count)

			remaining, err := engine.NewQuery[ParityProduct](db).Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, int64(50), remaining)
		})
	}
}

type AuditProbe struct {
	document.Base

	Str   string  `json:"str,omitempty"`
	Int   int     `json:"int,omitempty"`
	Int64 int64   `json:"int64,omitempty"`
	Float float64 `json:"float,omitempty"`
	Bool  bool    `json:"bool,omitempty"`
	PStr  *string `json:"pstr,omitempty"`
	PInt  *int    `json:"pint,omitempty"`

	T  time.Time  `json:"t,omitzero"`
	PT *time.Time `json:"pt,omitempty"`

	Bytes  []byte          `json:"bytes,omitempty"`
	RawMsg json.RawMessage `json:"raw,omitempty"`

	Dur time.Duration `json:"dur,omitempty"`

	Status auditStatus `json:"status,omitempty"`
}

type auditStatus string

const auditStatusActive auditStatus = "active"

func auditProbeDBs(t *testing.T) map[string]*engine.DB {
	t.Helper()
	return map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &AuditProbe{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &AuditProbe{}),
	}
}

// Audit (den-zejm) probing every realistic Go field type for the
// bind-vs-JSON-shape mismatch pattern. Each subtest saves a doc with one
// field set, then asserts that an Eq query on that field finds it.
// Failures on SQLite indicate the same family of bug as den-w3g0 / den-4l2y.
// Postgres is the reference: if it fails there, the bug is universal.
func TestParity_AuditBindShapes(t *testing.T) {
	type tc struct {
		name  string
		set   func(*AuditProbe)
		query where.Condition
	}
	cases := []tc{
		{"string", func(p *AuditProbe) { p.Str = "hello" }, where.Field("str").Eq("hello")},
		{"int", func(p *AuditProbe) { p.Int = 42 }, where.Field("int").Eq(42)},
		{"int64", func(p *AuditProbe) { p.Int64 = 9999999999 }, where.Field("int64").Eq(int64(9999999999))},
		{"float64", func(p *AuditProbe) { p.Float = 3.14 }, where.Field("float").Eq(3.14)},
		{"bool", func(p *AuditProbe) { p.Bool = true }, where.Field("bool").Eq(true)},
		{"*string non-nil", func(p *AuditProbe) { s := "ptr"; p.PStr = &s }, where.Field("pstr").Eq("ptr")},
		{"*int non-nil", func(p *AuditProbe) { i := 7; p.PInt = &i }, where.Field("pint").Eq(7)},

		{"time.Time", func(p *AuditProbe) {
			p.T = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
		}, where.Field("t").Eq(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))},
		{"*time.Time non-nil", func(p *AuditProbe) {
			tt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
			p.PT = &tt
		}, where.Field("pt").Eq(time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))},

		{"[]byte", func(p *AuditProbe) { p.Bytes = []byte{0x01, 0x02} }, where.Field("bytes").Eq([]byte{0x01, 0x02})},
		{"json.RawMessage", func(p *AuditProbe) { p.RawMsg = json.RawMessage(`{"k":"v"}`) }, where.Field("raw").Eq(json.RawMessage(`{"k":"v"}`))},

		{"time.Duration", func(p *AuditProbe) { p.Dur = 5 * time.Second }, where.Field("dur").Eq(5 * time.Second)},

		{"named typedef string", func(p *AuditProbe) { p.Status = auditStatusActive }, where.Field("status").Eq(auditStatusActive)},
	}

	for backendName, db := range auditProbeDBs(t) {
		t.Run(backendName, func(t *testing.T) {
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					ctx := context.Background()
					p := &AuditProbe{}
					c.set(p)
					require.NoError(t, engine.Save(ctx, db, p))

					raw, _ := json.Marshal(p)
					t.Logf("stored JSON: %s", raw)

					got, err := engine.NewQuery[AuditProbe](db, c.query).Count(ctx)
					require.NoError(t, err)
					assert.Equal(t, int64(1), got, "Eq query on %q must find the saved doc", c.name)
				})
			}
		})
	}
}
