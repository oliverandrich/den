package den_test

import (
	"context"
	"testing"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
)

type BenchProduct struct {
	document.Base
	Name     string  `json:"name" den:"index"`
	Price    float64 `json:"price" den:"index"`
	Category string  `json:"category"`
}

func benchDB(b *testing.B) *den.DB {
	b.Helper()
	return dentest.MustOpen(b, &BenchProduct{})
}

func benchDBPostgres(b *testing.B) *den.DB {
	b.Helper()
	return dentest.MustOpenPostgres(b, dentest.PostgresURL(), &BenchProduct{})
}

func seedBenchProducts(b *testing.B, db *den.DB, n int) []string {
	b.Helper()
	ctx := context.Background()
	ids := make([]string, n)
	for i := range n {
		p := &BenchProduct{
			Name:     "Product",
			Price:    float64(i) * 1.5,
			Category: "cat",
		}
		if err := den.Insert(ctx, db, p); err != nil {
			b.Fatal(err)
		}
		ids[i] = p.ID
	}
	return ids
}

func runInsertBenchmark(b *testing.B, db *den.DB) {
	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		p := &BenchProduct{Name: "Widget", Price: 9.99, Category: "test"}
		if err := den.Insert(ctx, db, p); err != nil {
			b.Fatal(err)
		}
	}
}

func runFindByIDBenchmark(b *testing.B, db *den.DB) {
	ctx := context.Background()
	ids := seedBenchProducts(b, db, 100)
	target := ids[50]

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := den.FindByID[BenchProduct](ctx, db, target); err != nil {
			b.Fatal(err)
		}
	}
}

func runQueryAllBenchmark(b *testing.B, db *den.DB, n int) {
	ctx := context.Background()
	seedBenchProducts(b, db, n)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		results, err := den.NewQuery[BenchProduct](ctx, db).All()
		if err != nil {
			b.Fatal(err)
		}
		if len(results) != n {
			b.Fatalf("expected %d results, got %d", n, len(results))
		}
	}
}

func runQueryIterBenchmark(b *testing.B, db *den.DB, n int) {
	ctx := context.Background()
	seedBenchProducts(b, db, n)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		count := 0
		for _, err := range den.NewQuery[BenchProduct](ctx, db).Iter() {
			if err != nil {
				b.Fatal(err)
			}
			count++
		}
		if count != n {
			b.Fatalf("expected %d results, got %d", n, count)
		}
	}
}

func runUpdateBenchmark(b *testing.B, db *den.DB) {
	ctx := context.Background()
	p := &BenchProduct{Name: "Widget", Price: 9.99, Category: "test"}
	if err := den.Insert(ctx, db, p); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := range b.N {
		p.Price = float64(i)
		if err := den.Update(ctx, db, p); err != nil {
			b.Fatal(err)
		}
	}
}

func runDeleteBenchmark(b *testing.B, db *den.DB) {
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		p := &BenchProduct{Name: "Widget", Price: 9.99, Category: "test"}
		if err := den.Insert(ctx, db, p); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		if err := den.Delete(ctx, db, p); err != nil {
			b.Fatal(err)
		}
	}
}

func runQueryWithConditionBenchmark(b *testing.B, db *den.DB) {
	ctx := context.Background()
	seedBenchProducts(b, db, 100)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		results, err := den.NewQuery[BenchProduct](ctx, db,
			where.Field("price").Gt(50.0),
		).Sort("price", den.Asc).Limit(10).All()
		if err != nil {
			b.Fatal(err)
		}
		_ = results
	}
}

// --- SQLite benchmarks ---

func BenchmarkSQLite_Insert(b *testing.B) {
	runInsertBenchmark(b, benchDB(b))
}

func BenchmarkSQLite_FindByID(b *testing.B) {
	runFindByIDBenchmark(b, benchDB(b))
}

func BenchmarkSQLite_QueryAll10(b *testing.B) {
	runQueryAllBenchmark(b, benchDB(b), 10)
}

func BenchmarkSQLite_QueryAll100(b *testing.B) {
	runQueryAllBenchmark(b, benchDB(b), 100)
}

func BenchmarkSQLite_QueryIter10(b *testing.B) {
	runQueryIterBenchmark(b, benchDB(b), 10)
}

func BenchmarkSQLite_QueryIter100(b *testing.B) {
	runQueryIterBenchmark(b, benchDB(b), 100)
}

func BenchmarkSQLite_Update(b *testing.B) {
	runUpdateBenchmark(b, benchDB(b))
}

func BenchmarkSQLite_Delete(b *testing.B) {
	runDeleteBenchmark(b, benchDB(b))
}

func BenchmarkSQLite_QueryWithCondition(b *testing.B) {
	runQueryWithConditionBenchmark(b, benchDB(b))
}

// --- PostgreSQL benchmarks ---

func BenchmarkPostgres_Insert(b *testing.B) {
	runInsertBenchmark(b, benchDBPostgres(b))
}

func BenchmarkPostgres_FindByID(b *testing.B) {
	runFindByIDBenchmark(b, benchDBPostgres(b))
}

func BenchmarkPostgres_QueryAll10(b *testing.B) {
	runQueryAllBenchmark(b, benchDBPostgres(b), 10)
}

func BenchmarkPostgres_QueryAll100(b *testing.B) {
	runQueryAllBenchmark(b, benchDBPostgres(b), 100)
}

func BenchmarkPostgres_QueryIter10(b *testing.B) {
	runQueryIterBenchmark(b, benchDBPostgres(b), 10)
}

func BenchmarkPostgres_QueryIter100(b *testing.B) {
	runQueryIterBenchmark(b, benchDBPostgres(b), 100)
}

func BenchmarkPostgres_Update(b *testing.B) {
	runUpdateBenchmark(b, benchDBPostgres(b))
}

func BenchmarkPostgres_Delete(b *testing.B) {
	runDeleteBenchmark(b, benchDBPostgres(b))
}

func BenchmarkPostgres_QueryWithCondition(b *testing.B) {
	runQueryWithConditionBenchmark(b, benchDBPostgres(b))
}
