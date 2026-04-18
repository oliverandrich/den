package den_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
)

// BenchArticle is a ~1 KB JSON document with the mix of typed fields, indexed
// fields, an FTS field, a pointer-embedded Author, and a small metadata map
// that real document workloads actually carry. It is intentionally richer
// than BenchProduct (which exercises the low-level hot paths).
type BenchArticle struct {
	document.Base
	Title       string                `json:"title" den:"index"`
	Slug        string                `json:"slug" den:"unique"`
	Body        string                `json:"body" den:"fts"`
	Summary     string                `json:"summary"`
	Status      string                `json:"status" den:"index"`
	Category    string                `json:"category" den:"index"`
	Tags        []string              `json:"tags" den:"index"`
	Price       float64               `json:"price" den:"index"`
	Stock       int                   `json:"stock"`
	PublishedAt time.Time             `json:"published_at" den:"index"`
	Author      den.Link[BenchAuthor] `json:"author"`
	Meta        map[string]any        `json:"meta"`
}

// BenchAuthor is the Link[T] target for WithFetchLinks benchmarks.
type BenchAuthor struct {
	document.Base
	Name  string `json:"name" den:"index"`
	Email string `json:"email" den:"unique"`
	Bio   string `json:"bio"`
}

const benchBody = "In publishing and graphic design, Lorem ipsum is a placeholder text commonly used to demonstrate the visual form of a document or a typeface without relying on meaningful content. Lorem ipsum may be used as a placeholder before the final copy is available. " +
	"It is also used to temporarily replace text in a process called greeking, which allows designers to consider the form of a webpage or publication, without the meaning of the text influencing the design. " +
	"Lorem ipsum is typically a corrupted version of De finibus bonorum et malorum, a 1st-century BC text by the Roman statesman and philosopher Cicero, with words altered, added, and removed to make it nonsensical and improper Latin. " +
	"The first two words themselves are a truncation of dolorem ipsum. Variations of the passage are used, and other ad hoc invented dummy texts are sometimes used."

var (
	benchCategories = []string{"news", "review", "tutorial", "opinion", "analysis", "interview"}
	benchStatuses   = []string{"draft", "published", "archived"}
)

func makeBenchArticle(i int, authorID string) *BenchArticle {
	return &BenchArticle{
		Title:       fmt.Sprintf("Article %06d: The quick brown fox", i),
		Slug:        fmt.Sprintf("article-%06d", i),
		Body:        benchBody,
		Summary:     "A short summary describing what the article covers in a sentence or two.",
		Status:      benchStatuses[i%len(benchStatuses)],
		Category:    benchCategories[i%len(benchCategories)],
		Tags:        []string{"go", "database", "jsonb"},
		Price:       float64(i%100) + 0.99,
		Stock:       i % 1000,
		PublishedAt: time.Unix(1700000000+int64(i*3600), 0),
		Author:      den.Link[BenchAuthor]{ID: authorID},
		Meta: map[string]any{
			"views":    i * 17,
			"featured": i%7 == 0,
			"locale":   "en-US",
		},
	}
}

func rwBenchDB(b *testing.B) *den.DB {
	b.Helper()
	return dentest.MustOpen(b, &BenchArticle{}, &BenchAuthor{})
}

func rwBenchDBPostgres(b *testing.B) *den.DB {
	b.Helper()
	return dentest.MustOpenPostgres(b, dentest.PostgresURL(), &BenchArticle{}, &BenchAuthor{})
}

func seedAuthor(b *testing.B, db *den.DB) string {
	b.Helper()
	a := &BenchAuthor{Name: "Jane Author", Email: "jane@example.com", Bio: "Prolific writer."}
	if err := den.Insert(context.Background(), db, a); err != nil {
		b.Fatal(err)
	}
	return a.ID
}

func seedArticles(b *testing.B, db *den.DB, n int, authorID string) []string {
	b.Helper()
	ctx := context.Background()
	docs := make([]*BenchArticle, n)
	for i := range n {
		docs[i] = makeBenchArticle(i, authorID)
	}
	if err := den.InsertMany(ctx, db, docs); err != nil {
		b.Fatal(err)
	}
	ids := make([]string, n)
	for i, d := range docs {
		ids[i] = d.ID
	}
	return ids
}

// --- Insert ---

func runRWInsert(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	b.ResetTimer()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		doc := makeBenchArticle(i, authorID)
		if err := den.Insert(ctx, db, doc); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkRW_SQLite_Insert(b *testing.B)   { runRWInsert(b, rwBenchDB(b)) }
func BenchmarkRW_Postgres_Insert(b *testing.B) { runRWInsert(b, rwBenchDBPostgres(b)) }

// --- InsertMany ---

func runRWInsertMany(b *testing.B, db *den.DB, batch int) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	b.ResetTimer()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		docs := make([]*BenchArticle, batch)
		for j := range batch {
			docs[j] = makeBenchArticle(i*batch+j, authorID)
		}
		if err := den.InsertMany(ctx, db, docs); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkRW_SQLite_InsertMany100(b *testing.B)   { runRWInsertMany(b, rwBenchDB(b), 100) }
func BenchmarkRW_Postgres_InsertMany100(b *testing.B) { runRWInsertMany(b, rwBenchDBPostgres(b), 100) }
func BenchmarkRW_SQLite_InsertMany1000(b *testing.B)  { runRWInsertMany(b, rwBenchDB(b), 1000) }
func BenchmarkRW_Postgres_InsertMany1000(b *testing.B) {
	runRWInsertMany(b, rwBenchDBPostgres(b), 1000)
}

// --- FindByID ---

func runRWFindByID(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	ids := seedArticles(b, db, 1000, authorID)
	b.ResetTimer()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		if _, err := den.FindByID[BenchArticle](ctx, db, ids[i%len(ids)]); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkRW_SQLite_FindByID(b *testing.B)   { runRWFindByID(b, rwBenchDB(b)) }
func BenchmarkRW_Postgres_FindByID(b *testing.B) { runRWFindByID(b, rwBenchDBPostgres(b)) }

// --- FindByIDs (10) ---

func runRWFindByIDs(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	ids := seedArticles(b, db, 1000, authorID)
	b.ResetTimer()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		batch := make([]string, 10)
		for j := range 10 {
			batch[j] = ids[(i*10+j)%len(ids)]
		}
		if _, err := den.FindByIDs[BenchArticle](ctx, db, batch); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkRW_SQLite_FindByIDs10(b *testing.B)   { runRWFindByIDs(b, rwBenchDB(b)) }
func BenchmarkRW_Postgres_FindByIDs10(b *testing.B) { runRWFindByIDs(b, rwBenchDBPostgres(b)) }

// --- Query filtered (limit 10 / 100) ---

func runRWQueryFiltered(b *testing.B, db *den.DB, limit int) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	_ = seedArticles(b, db, 1000, authorID)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		results, err := den.NewQuery[BenchArticle](db,
			where.Field("status").Eq("published"),
		).Sort("published_at", den.Desc).Limit(limit).All(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if len(results) == 0 {
			b.Fatal("expected results")
		}
	}
}

func BenchmarkRW_SQLite_QueryFiltered10(b *testing.B) { runRWQueryFiltered(b, rwBenchDB(b), 10) }
func BenchmarkRW_Postgres_QueryFiltered10(b *testing.B) {
	runRWQueryFiltered(b, rwBenchDBPostgres(b), 10)
}
func BenchmarkRW_SQLite_QueryFiltered100(b *testing.B) { runRWQueryFiltered(b, rwBenchDB(b), 100) }
func BenchmarkRW_Postgres_QueryFiltered100(b *testing.B) {
	runRWQueryFiltered(b, rwBenchDBPostgres(b), 100)
}

// --- Iter 1000 ---

func runRWIter(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	_ = seedArticles(b, db, 1000, authorID)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		count := 0
		for _, err := range den.NewQuery[BenchArticle](db).Iter(ctx) {
			if err != nil {
				b.Fatal(err)
			}
			count++
		}
		if count != 1000 {
			b.Fatalf("expected 1000, got %d", count)
		}
	}
}

func BenchmarkRW_SQLite_Iter1000(b *testing.B)   { runRWIter(b, rwBenchDB(b)) }
func BenchmarkRW_Postgres_Iter1000(b *testing.B) { runRWIter(b, rwBenchDBPostgres(b)) }

// --- Aggregate: Count with filter ---

func runRWCount(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	_ = seedArticles(b, db, 1000, authorID)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := den.NewQuery[BenchArticle](db,
			where.Field("status").Eq("published"),
		).Count(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRW_SQLite_CountFiltered(b *testing.B)   { runRWCount(b, rwBenchDB(b)) }
func BenchmarkRW_Postgres_CountFiltered(b *testing.B) { runRWCount(b, rwBenchDBPostgres(b)) }

// --- Aggregate: Sum with filter ---

func runRWSum(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	_ = seedArticles(b, db, 1000, authorID)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := den.NewQuery[BenchArticle](db,
			where.Field("status").Eq("published"),
		).Sum(ctx, "price"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRW_SQLite_SumFiltered(b *testing.B)   { runRWSum(b, rwBenchDB(b)) }
func BenchmarkRW_Postgres_SumFiltered(b *testing.B) { runRWSum(b, rwBenchDBPostgres(b)) }

// --- FTS Search ---

func runRWSearch(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	_ = seedArticles(b, db, 1000, authorID)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		results, err := den.NewQuery[BenchArticle](db).Limit(20).Search(ctx, "Cicero")
		if err != nil {
			b.Fatal(err)
		}
		if len(results) == 0 {
			b.Fatal("expected search matches")
		}
	}
}

func BenchmarkRW_SQLite_Search(b *testing.B)   { runRWSearch(b, rwBenchDB(b)) }
func BenchmarkRW_Postgres_Search(b *testing.B) { runRWSearch(b, rwBenchDBPostgres(b)) }

// --- WithFetchLinks ---

func runRWWithFetchLinks(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	_ = seedArticles(b, db, 100, authorID)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		results, err := den.NewQuery[BenchArticle](db).
			WithFetchLinks().
			Sort("published_at", den.Desc).
			Limit(20).
			All(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if len(results) == 0 {
			b.Fatal("expected results")
		}
	}
}

func BenchmarkRW_SQLite_WithFetchLinks(b *testing.B)   { runRWWithFetchLinks(b, rwBenchDB(b)) }
func BenchmarkRW_Postgres_WithFetchLinks(b *testing.B) { runRWWithFetchLinks(b, rwBenchDBPostgres(b)) }

// --- Update (single) ---

func runRWUpdate(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	ids := seedArticles(b, db, 100, authorID)
	b.ResetTimer()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		doc, err := den.FindByID[BenchArticle](ctx, db, ids[i%len(ids)])
		if err != nil {
			b.Fatal(err)
		}
		doc.Stock++
		if err := den.Update(ctx, db, doc); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkRW_SQLite_Update(b *testing.B)   { runRWUpdate(b, rwBenchDB(b)) }
func BenchmarkRW_Postgres_Update(b *testing.B) { runRWUpdate(b, rwBenchDBPostgres(b)) }

// --- QuerySet.Update bulk 100 ---

func runRWBulkUpdate(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	_ = seedArticles(b, db, 100, authorID)
	b.ResetTimer()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		newStatus := "archived"
		if i%2 == 0 {
			newStatus = "published"
		}
		if _, err := den.NewQuery[BenchArticle](db).Update(ctx, den.SetFields{"status": newStatus}); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkRW_SQLite_BulkUpdate100(b *testing.B)   { runRWBulkUpdate(b, rwBenchDB(b)) }
func BenchmarkRW_Postgres_BulkUpdate100(b *testing.B) { runRWBulkUpdate(b, rwBenchDBPostgres(b)) }

// --- Transaction (read + write + commit) ---

func runRWTransaction(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	ids := seedArticles(b, db, 100, authorID)
	b.ResetTimer()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		err := den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
			doc, err := den.TxFindByID[BenchArticle](tx, ids[i%len(ids)])
			if err != nil {
				return err
			}
			doc.Stock++
			return den.TxUpdate(tx, doc)
		})
		if err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func BenchmarkRW_SQLite_Transaction(b *testing.B)   { runRWTransaction(b, rwBenchDB(b)) }
func BenchmarkRW_Postgres_Transaction(b *testing.B) { runRWTransaction(b, rwBenchDBPostgres(b)) }
