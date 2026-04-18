package den_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/oliverandrich/den"
	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
)

// BenchJob is the queue-consumer fixture. The TxLockByID benchmark locks
// pre-seeded rows with SkipLocked so workers claim disjoint work. A separate
// type keeps the parallel scenarios independent from BenchArticle's indexes.
type BenchJob struct {
	document.Base
	Status  string `json:"status" den:"index"`
	Payload string `json:"payload"`
}

func conBenchDBPostgres(b *testing.B) *den.DB {
	b.Helper()
	return dentest.MustOpenPostgres(b, dentest.PostgresURL(), &BenchArticle{}, &BenchAuthor{}, &BenchJob{})
}

func conBenchDBSQLite(b *testing.B) *den.DB {
	b.Helper()
	return dentest.MustOpen(b, &BenchArticle{}, &BenchAuthor{}, &BenchJob{})
}

// --- Parallel FindByID (read-only) ---
//
// Every goroutine reads a rotating ID from the pre-seeded set. On PG this
// scales with pool connections; on SQLite WAL readers don't block, so it
// should scale up to the cost of opening the read.

func runConFindByID(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	ids := seedArticles(b, db, 1000, authorID)

	b.ResetTimer()
	b.ReportAllocs()
	var counter atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := counter.Add(1)
			if _, err := den.FindByID[BenchArticle](ctx, db, ids[int(idx)%len(ids)]); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkConcurrent_SQLite_FindByID(b *testing.B)   { runConFindByID(b, conBenchDBSQLite(b)) }
func BenchmarkConcurrent_Postgres_FindByID(b *testing.B) { runConFindByID(b, conBenchDBPostgres(b)) }

// --- Parallel Insert (write-only) ---
//
// On SQLite, BEGIN IMMEDIATE serializes writers — throughput stays flat
// around single-writer speed. On PG, MVCC lets writes run concurrently
// so throughput should scale with the pool.

func runConInsert(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)

	b.ResetTimer()
	b.ReportAllocs()
	var counter atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := counter.Add(1)
			doc := makeBenchArticle(int(i), authorID)
			// Ensure slug uniqueness under concurrency even if counter wraps
			doc.Slug = doc.ID + "-" + doc.Slug
			if err := den.Insert(ctx, db, doc); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkConcurrent_SQLite_Insert(b *testing.B)   { runConInsert(b, conBenchDBSQLite(b)) }
func BenchmarkConcurrent_Postgres_Insert(b *testing.B) { runConInsert(b, conBenchDBPostgres(b)) }

// --- Mixed read/write 80/20 ---
//
// Realistic web-service shape: lots of reads, a few writes. Each goroutine
// decides per-iteration by counter modulo 5.

func runConMixed(b *testing.B, db *den.DB) {
	ctx := context.Background()
	authorID := seedAuthor(b, db)
	ids := seedArticles(b, db, 1000, authorID)

	b.ResetTimer()
	b.ReportAllocs()
	var counter atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := counter.Add(1)
			if i%5 == 0 {
				doc, err := den.FindByID[BenchArticle](ctx, db, ids[int(i)%len(ids)])
				if err != nil {
					b.Fatal(err)
				}
				doc.Stock++
				if err := den.Update(ctx, db, doc); err != nil {
					b.Fatal(err)
				}
			} else {
				if _, err := den.FindByID[BenchArticle](ctx, db, ids[int(i)%len(ids)]); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

func BenchmarkConcurrent_SQLite_Mixed8020(b *testing.B)   { runConMixed(b, conBenchDBSQLite(b)) }
func BenchmarkConcurrent_Postgres_Mixed8020(b *testing.B) { runConMixed(b, conBenchDBPostgres(b)) }

// --- Queue consumer: TxLockByID + SkipLocked ---
//
// N workers pop jobs from a shared set. Each iteration claims one row and
// marks it processed. Rows that are locked by another worker are skipped
// (returning ErrNotFound on PG, treated as idle). On SQLite SkipLocked is
// a no-op — writers serialize.

func runConQueueConsumer(b *testing.B, db *den.DB) {
	ctx := context.Background()

	const jobCount = 5000
	jobs := make([]*BenchJob, jobCount)
	for i := range jobCount {
		jobs[i] = &BenchJob{Status: "pending", Payload: "work item"}
	}
	if err := den.InsertMany(ctx, db, jobs); err != nil {
		b.Fatal(err)
	}
	ids := make([]string, jobCount)
	for i, j := range jobs {
		ids[i] = j.ID
	}

	b.ResetTimer()
	b.ReportAllocs()
	var counter atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := int(counter.Add(1)) % len(ids)
			_ = den.RunInTransaction(ctx, db, func(tx *den.Tx) error {
				job, err := den.TxLockByID[BenchJob](tx, ids[idx], den.SkipLocked())
				if err != nil {
					if errors.Is(err, den.ErrNotFound) {
						// Another worker holds it (or it was already processed).
						return nil
					}
					return err
				}
				job.Status = "processed"
				return den.TxUpdate(tx, job)
			})
		}
	})
}

func BenchmarkConcurrent_SQLite_QueueConsumer(b *testing.B) {
	runConQueueConsumer(b, conBenchDBSQLite(b))
}
func BenchmarkConcurrent_Postgres_QueueConsumer(b *testing.B) {
	runConQueueConsumer(b, conBenchDBPostgres(b))
}
