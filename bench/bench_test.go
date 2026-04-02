package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/oliverandrich/den"
	sqlitebackend "github.com/oliverandrich/den/backend/sqlite"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
)

// --- Document types ---

type User struct {
	document.Base
	Name  string `json:"name" den:"index"`
	Email string `json:"email" den:"unique"`
	Age   int    `json:"age" den:"index"`
	Bio   string `json:"bio,omitempty"` // used for Large benchmark payload
}

type Article struct {
	document.Base
	Title  string         `json:"title" den:"index"`
	Body   string         `json:"body"`
	Author den.Link[User] `json:"author"`
}

type Comment struct {
	document.Base
	Body      string            `json:"body"`
	ArticleID den.Link[Article] `json:"article"`
	AuthorID  den.Link[User]    `json:"commenter"`
}

// --- Backend factory ---

type backendFactory struct {
	name  string
	setup func(b *testing.B, types ...any) *den.DB
}

var backends = []backendFactory{
	{"sqlite", setupSQLite},
}

var concurrentBackends = []backendFactory{
	{"sqlite", setupSQLite},
}

var concurrentWriteBackends = []backendFactory{
	{"sqlite", setupSQLite},
}

func setupSQLite(b *testing.B, types ...any) *den.DB {
	b.Helper()
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "den-bench-sqlite-*")
	if err != nil {
		b.Fatal(err)
	}
	dbPath := filepath.Join(dir, "bench.db")
	backend, err := sqlitebackend.Open(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	db, err := den.Open(backend)
	if err != nil {
		b.Fatal(err)
	}
	if len(types) == 0 {
		types = []any{&User{}, &Article{}, &Comment{}}
	}
	if err := den.Register(ctx, db, types...); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
	return db
}

// --- BenchmarkSimple ---
// Insert N users in one batch, then scan all.

func BenchmarkSimple_Insert100K(b *testing.B) {
	ctx := context.Background()
	for _, bf := range backends {
		b.Run(bf.name, func(b *testing.B) {
			for range b.N {
				db := bf.setup(b, &User{})
				for i := range 100_000 {
					u := &User{
						Name:  fmt.Sprintf("User %d", i),
						Email: fmt.Sprintf("user%d@example.com", i),
						Age:   20 + (i % 50),
					}
					if err := den.Insert(ctx, db, u); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func BenchmarkSimple_ScanAll(b *testing.B) {
	ctx := context.Background()
	for _, bf := range backends {
		b.Run(bf.name, func(b *testing.B) {
			db := bf.setup(b, &User{})
			for i := range 100_000 {
				u := &User{
					Name:  fmt.Sprintf("User %d", i),
					Email: fmt.Sprintf("user%d@example.com", i),
					Age:   20 + (i % 50),
				}
				if err := den.Insert(ctx, db, u); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()

			for range b.N {
				results, err := den.NewQuery[User](ctx, db).All()
				if err != nil {
					b.Fatal(err)
				}
				if len(results) != 100_000 {
					b.Fatalf("expected 100000, got %d", len(results))
				}
			}
		})
	}
}

// --- BenchmarkReal ---
// Insert 100 users, 20 articles per user, 20 comments per article.
// Then query users by email (index lookup) and resolve links.

func BenchmarkReal_Insert(b *testing.B) {
	for _, bf := range backends {
		b.Run(bf.name, func(b *testing.B) {
			for range b.N {
				db := bf.setup(b)
				insertRealData(b, db)
			}
		})
	}
}

func BenchmarkReal_QueryByEmail(b *testing.B) {
	ctx := context.Background()
	for _, bf := range backends {
		b.Run(bf.name, func(b *testing.B) {
			db := bf.setup(b)
			insertRealData(b, db)
			b.ResetTimer()

			for range b.N {
				for i := range 100 {
					_, err := den.NewQuery[User](ctx, db,
						where.Field("email").Eq(fmt.Sprintf("user%d@example.com", i)),
					).First()
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func BenchmarkReal_QueryArticlesWithAuthor(b *testing.B) {
	ctx := context.Background()
	for _, bf := range backends {
		b.Run(bf.name, func(b *testing.B) {
			db := bf.setup(b)
			users := insertRealData(b, db)
			targetID := users[0].ID
			b.ResetTimer()

			for range b.N {
				articles, err := den.NewQuery[Article](ctx, db,
					where.Field("author").Eq(targetID),
				).All()
				if err != nil {
					b.Fatal(err)
				}
				if len(articles) != 20 {
					b.Fatalf("expected 20 articles, got %d", len(articles))
				}
			}
		})
	}
}

// --- BenchmarkMany ---
// Insert N users, then query all users 1000 times. Read-heavy simulation.

func BenchmarkMany_10(b *testing.B) {
	benchmarkMany(b, 10)
}

func BenchmarkMany_100(b *testing.B) {
	benchmarkMany(b, 100)
}

func BenchmarkMany_1000(b *testing.B) {
	benchmarkMany(b, 1000)
}

func benchmarkMany(b *testing.B, n int) {
	ctx := context.Background()
	for _, bf := range backends {
		b.Run(bf.name, func(b *testing.B) {
			db := bf.setup(b, &User{})
			seedUsers(b, db, n)
			b.ResetTimer()

			for range b.N {
				for range 1000 {
					results, err := den.NewQuery[User](ctx, db).All()
					if err != nil {
						b.Fatal(err)
					}
					if len(results) != n {
						b.Fatalf("expected %d, got %d", n, len(results))
					}
				}
			}
		})
	}
}

// --- BenchmarkLarge ---
// Insert 10000 users with N bytes of payload, then query all.

func BenchmarkLarge_50KB(b *testing.B) {
	benchmarkLarge(b, 50*1024)
}

func BenchmarkLarge_100KB(b *testing.B) {
	benchmarkLarge(b, 100*1024)
}

func BenchmarkLarge_200KB(b *testing.B) {
	benchmarkLarge(b, 200*1024)
}

func benchmarkLarge(b *testing.B, payloadSize int) {
	ctx := context.Background()
	payload := strings.Repeat("x", payloadSize)

	for _, bf := range backends {
		b.Run(bf.name, func(b *testing.B) {
			db := bf.setup(b, &User{})
			for i := range 10_000 {
				u := &User{
					Name:  fmt.Sprintf("User %d", i),
					Email: fmt.Sprintf("user%d@example.com", i),
					Age:   20 + (i % 50),
					Bio:   payload,
				}
				if err := den.Insert(ctx, db, u); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()

			for range b.N {
				results, err := den.NewQuery[User](ctx, db).All()
				if err != nil {
					b.Fatal(err)
				}
				if len(results) != 10_000 {
					b.Fatalf("expected 10000, got %d", len(results))
				}
			}
		})
	}
}

// --- BenchmarkConcurrent ---
// Insert 100K users, then N goroutines query all users simultaneously.

func BenchmarkConcurrent_2(b *testing.B) {
	benchmarkConcurrent(b, 2)
}

func BenchmarkConcurrent_4(b *testing.B) {
	benchmarkConcurrent(b, 4)
}

func BenchmarkConcurrent_8(b *testing.B) {
	benchmarkConcurrent(b, 8)
}

func benchmarkConcurrent(b *testing.B, numReaders int) {
	ctx := context.Background()
	for _, bf := range concurrentBackends {
		b.Run(bf.name, func(b *testing.B) {
			db := bf.setup(b, &User{})
			seedUsers(b, db, 1000)
			b.ResetTimer()
			b.SetParallelism(numReaders)

			var errOnce sync.Once
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					results, err := den.NewQuery[User](ctx, db).All()
					if err != nil {
						errOnce.Do(func() { b.Error(err) })
						return
					}
					if len(results) != 1000 {
						errOnce.Do(func() { b.Errorf("expected 1000, got %d", len(results)) })
						return
					}
				}
			})
		})
	}
}

// --- BenchmarkConcurrentWrite ---
// N goroutines inserting users simultaneously.

func BenchmarkConcurrentWrite_2(b *testing.B) {
	benchmarkConcurrentWrite(b, 2)
}

func BenchmarkConcurrentWrite_4(b *testing.B) {
	benchmarkConcurrentWrite(b, 4)
}

func BenchmarkConcurrentWrite_8(b *testing.B) {
	benchmarkConcurrentWrite(b, 8)
}

func benchmarkConcurrentWrite(b *testing.B, numWriters int) {
	ctx := context.Background()
	for _, bf := range concurrentWriteBackends {
		b.Run(bf.name, func(b *testing.B) {
			db := bf.setup(b, &User{})
			b.SetParallelism(numWriters)

			var counter atomic.Int64
			var errOnce sync.Once
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					n := counter.Add(1)
					u := &User{
						Name:  fmt.Sprintf("User %d", n),
						Email: fmt.Sprintf("user%d@example.com", n),
						Age:   20 + int(n%50),
					}
					if err := den.Insert(ctx, db, u); err != nil {
						errOnce.Do(func() { b.Error(err) })
						return
					}
				}
			})
		})
	}
}

// --- Helpers ---

func seedUsers(b *testing.B, db *den.DB, n int) []*User {
	b.Helper()
	ctx := context.Background()
	users := make([]*User, n)
	for i := range n {
		users[i] = &User{
			Name:  fmt.Sprintf("User %d", i),
			Email: fmt.Sprintf("user%d@example.com", i),
			Age:   20 + (i % 50),
		}
		if err := den.Insert(ctx, db, users[i]); err != nil {
			b.Fatal(err)
		}
	}
	return users
}

func insertRealData(b *testing.B, db *den.DB) []*User {
	b.Helper()
	ctx := context.Background()
	users := seedUsers(b, db, 100)

	for _, u := range users {
		for j := range 20 {
			a := &Article{
				Title:  fmt.Sprintf("Article %d by %s", j, u.Name),
				Body:   fmt.Sprintf("This is article number %d about various topics.", j),
				Author: den.NewLink(u),
			}
			if err := den.Insert(ctx, db, a); err != nil {
				b.Fatal(err)
			}

			for k := range 20 {
				c := &Comment{
					Body:      fmt.Sprintf("Comment %d on article %d", k, j),
					ArticleID: den.NewLink(a),
					AuthorID:  den.NewLink(users[(k+j)%len(users)]),
				}
				if err := den.Insert(ctx, db, c); err != nil {
					b.Fatal(err)
				}
			}
		}
	}

	return users
}
