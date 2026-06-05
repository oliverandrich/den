package engine_test

import (
	"github.com/oliverandrich/den/engine"

	"context"
	"errors"
	"testing"

	"github.com/oliverandrich/den/dentest"
	"github.com/oliverandrich/den/document"
	"github.com/oliverandrich/den/where"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type FTSArticle struct {
	document.Base
	Title    string `json:"title" den:"fts"`
	Body     string `json:"body" den:"fts"`
	Category string `json:"category"`
}

func TestSearch_SQLite(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, engine.SaveAll(ctx, db, []*FTSArticle{
		{Title: "Go Programming", Body: "Learn Go language basics", Category: "tech"},
		{Title: "Python Tutorial", Body: "Introduction to Python", Category: "tech"},
		{Title: "Cooking Tips", Body: "How to make pasta", Category: "food"},
	}))

	results, err := engine.NewQuery[FTSArticle](db).Search(ctx, "Go")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Go Programming", results[0].Title)
}

// Pins fix for den-qrg2 on the FTS path: Search appends WHERE conditions
// after the MATCH/@@ predicate, and an Or() sibling must not absorb the
// MATCH via SQL AND > OR precedence.
func TestSearch_OrAndComposition(t *testing.T) {
	dbs := ftsBackends(t, nil)
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.SaveAll(ctx, db, []*FTSArticle{
				{Title: "alpha doc", Body: "hello world", Category: "tech"},
				{Title: "beta doc", Body: "hello world", Category: "tech"},
				{Title: "gamma doc", Body: "hello world", Category: "food"},
			}))

			hits, err := engine.NewQuery[FTSArticle](db, where.Or(
				where.Field("title").Eq("alpha doc"),
				where.Field("title").Eq("beta doc"),
			), where.Field("category").Eq("tech")).Search(ctx, "hello")
			require.NoError(t, err)
			assert.Len(t, hits, 2, "Or + sibling Eq must AND-compose alongside MATCH")
		})
	}
}

// TestSearch_HonorsScopeInTx pins that Search routes through the caller's
// scope just like every other operation: a doc inserted inside the caller's
// tx is visible to a tx-bound Search before commit. SQLite FTS5 triggers
// fire on the same connection; PostgreSQL's tsvector + GIN see same-tx
// writes via MVCC. Both backends share the contract.
func TestSearch_HonorsScopeInTx(t *testing.T) {
	dbs := ftsBackends(t, nil)
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			var insideHits []*FTSArticle
			err := engine.RunInTransaction(ctx, db, func(tx *engine.Tx) error {
				if err := engine.Save(ctx, tx, &FTSArticle{
					Title: "TxLocalSecret", Body: "tx-local body", Category: "tech",
				}); err != nil {
					return err
				}
				hits, err := engine.NewQuery[FTSArticle](tx).Search(ctx, "TxLocalSecret")
				if err != nil {
					return err
				}
				insideHits = hits
				return nil
			})
			require.NoError(t, err)
			require.Len(t, insideHits, 1, "tx-bound Search must see tx-local writes")
			assert.Equal(t, "TxLocalSecret", insideHits[0].Title)

			after, err := engine.NewQuery[FTSArticle](db).Search(ctx, "TxLocalSecret")
			require.NoError(t, err)
			require.Len(t, after, 1, "doc remains visible after commit")
		})
	}
}

// TestSearch_RollbackHidesDocs pins the isolation guarantee: if the tx
// rolls back, the in-tx-Searchable doc never reaches committed state and
// is invisible to a fresh DB-bound Search.
func TestSearch_RollbackHidesDocs(t *testing.T) {
	dbs := ftsBackends(t, nil)
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			rollbackErr := errors.New("force rollback")
			err := engine.RunInTransaction(ctx, db, func(tx *engine.Tx) error {
				if err := engine.Save(ctx, tx, &FTSArticle{
					Title: "ShouldNeverReachDisk", Body: "rolled-back body", Category: "tech",
				}); err != nil {
					return err
				}
				// Confirm the doc IS visible in-tx before we roll back —
				// otherwise the rollback test would pass even if Search were
				// still bypassing the tx (silent regression).
				hits, err := engine.NewQuery[FTSArticle](tx).Search(ctx, "ShouldNeverReachDisk")
				if err != nil {
					return err
				}
				require.Len(t, hits, 1, "doc must be visible in tx pre-rollback")
				return rollbackErr
			})
			require.ErrorIs(t, err, rollbackErr)

			after, err := engine.NewQuery[FTSArticle](db).Search(ctx, "ShouldNeverReachDisk")
			require.NoError(t, err)
			assert.Empty(t, after, "rolled-back doc must not be Searchable from db scope")
		})
	}
}

// TestSearch_TxBoundWhereSeesTxLocal verifies that Where conditions applied
// alongside Search on a tx-bound QuerySet also operate on tx-local data —
// the whole query travels through the tx connection, not just the FTS bit.
func TestSearch_TxBoundWhereSeesTxLocal(t *testing.T) {
	dbs := ftsBackends(t, nil)
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			var insideHits []*FTSArticle
			err := engine.RunInTransaction(ctx, db, func(tx *engine.Tx) error {
				if err := engine.SaveAll(ctx, tx, []*FTSArticle{
					{Title: "TechPost", Body: "shared keyword tech", Category: "tech"},
					{Title: "FoodPost", Body: "shared keyword food", Category: "food"},
				}); err != nil {
					return err
				}
				hits, err := engine.NewQuery[FTSArticle](tx,
					where.Field("category").Eq("tech"),
				).Search(ctx, "shared")
				if err != nil {
					return err
				}
				insideHits = hits
				return nil
			})
			require.NoError(t, err)
			require.Len(t, insideHits, 1, "Where filter must apply against tx-local data")
			assert.Equal(t, "TechPost", insideHits[0].Title)
		})
	}
}

func TestSearch_SQLite_MultipleResults(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, engine.SaveAll(ctx, db, []*FTSArticle{
		{Title: "Go Basics", Body: "Learn Go", Category: "tech"},
		{Title: "Advanced Go", Body: "Go concurrency patterns", Category: "tech"},
		{Title: "Python Basics", Body: "Learn Python", Category: "tech"},
	}))

	results, err := engine.NewQuery[FTSArticle](db).Search(ctx, "Go")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestSearch_SQLite_WithFilter(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, engine.SaveAll(ctx, db, []*FTSArticle{
		{Title: "Go Web", Body: "Building web apps with Go", Category: "tech"},
		{Title: "Go Cooking", Body: "Go to recipes for beginners", Category: "food"},
	}))

	results, err := engine.NewQuery[FTSArticle](db, where.Field("category").Eq("tech")).Search(ctx, "Go")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Go Web", results[0].Title)
}

func TestSearch_SQLite_WithLimit(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, engine.SaveAll(ctx, db, []*FTSArticle{
		{Title: "Go One", Body: "First Go article", Category: "tech"},
		{Title: "Go Two", Body: "Second Go article", Category: "tech"},
		{Title: "Go Three", Body: "Third Go article", Category: "tech"},
	}))

	results, err := engine.NewQuery[FTSArticle](db).Limit(2).Search(ctx, "Go")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestSearch_SQLite_NoResults(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, engine.Save(ctx, db, &FTSArticle{Title: "Hello", Body: "World"}))

	results, err := engine.NewQuery[FTSArticle](db).Search(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestSearch_Postgres(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, engine.SaveAll(ctx, db, []*FTSArticle{
		{Title: "Go Programming", Body: "Learn Go language basics", Category: "tech"},
		{Title: "Python Tutorial", Body: "Introduction to Python", Category: "tech"},
		{Title: "Cooking Tips", Body: "How to make pasta", Category: "food"},
	}))

	results, err := engine.NewQuery[FTSArticle](db).Search(ctx, "programming")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Go Programming", results[0].Title)
}

func TestSearch_Postgres_WithFilter(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, engine.SaveAll(ctx, db, []*FTSArticle{
		{Title: "Go Web", Body: "Building web apps with Go", Category: "tech"},
		{Title: "Go Cooking", Body: "Go to recipes for beginners", Category: "food"},
	}))

	results, err := engine.NewQuery[FTSArticle](db, where.Field("category").Eq("tech")).Search(ctx, "Go")
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Go Web", results[0].Title)
}

func TestSearch_Postgres_WithLimit(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, engine.SaveAll(ctx, db, []*FTSArticle{
		{Title: "Go One", Body: "First Go article", Category: "tech"},
		{Title: "Go Two", Body: "Second Go article", Category: "tech"},
		{Title: "Go Three", Body: "Third Go article", Category: "tech"},
	}))

	results, err := engine.NewQuery[FTSArticle](db).Limit(2).Search(ctx, "Go")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// TestSearch_HonorsAfterCursor pins that Search respects After(id) on
// both backends, matching the non-FTS QuerySet behavior. The rank-based
// default order is overridden with an explicit Sort("_id") so the cursor
// semantics are predictable across backends.
func TestSearch_HonorsAfterCursor(t *testing.T) {
	dbs := ftsBackends(t, nil)
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			require.NoError(t, engine.SaveAll(ctx, db, []*FTSArticle{
				{Title: "Go One", Body: "First Go article"},
				{Title: "Go Two", Body: "Second Go article"},
				{Title: "Go Three", Body: "Third Go article"},
			}))

			first, err := engine.NewQuery[FTSArticle](db).Sort("_id", engine.Asc).Search(ctx, "Go")
			require.NoError(t, err)
			require.Len(t, first, 3)

			rest, err := engine.NewQuery[FTSArticle](db).Sort("_id", engine.Asc).After(first[0].ID).Search(ctx, "Go")
			require.NoError(t, err)
			require.Len(t, rest, 2, "After(id) must be honored in FTS Search")
			assert.Equal(t, first[1].ID, rest[0].ID)
			assert.Equal(t, first[2].ID, rest[1].ID)
		})
	}
}

// ftsBackends returns a fresh SQLite + PostgreSQL DB pair seeded with the
// same articles, for tests that assert both backends agree.
func ftsBackends(t *testing.T, seed []*FTSArticle) map[string]*engine.DB {
	t.Helper()
	dbs := map[string]*engine.DB{
		"sqlite":   dentest.MustOpen(t, &FTSArticle{}),
		"postgres": dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{}),
	}
	if len(seed) > 0 {
		for _, db := range dbs {
			require.NoError(t, engine.SaveAll(context.Background(), db, seed))
		}
	}
	return dbs
}

// TestSearch_MultiTokenANDs pins that a multi-word literal term matches only
// documents containing every token (implicit AND), identically on both
// backends.
func TestSearch_MultiTokenANDs(t *testing.T) {
	dbs := ftsBackends(t, []*FTSArticle{
		{Title: "both", Body: "red green together"},
		{Title: "red only", Body: "red alone"},
		{Title: "green only", Body: "green alone"},
	})
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			hits, err := engine.NewQuery[FTSArticle](db).Search(ctx, "red green")
			require.NoError(t, err)
			require.Len(t, hits, 1, "only the doc with both tokens matches")
			assert.Equal(t, "both", hits[0].Title)
		})
	}
}

// TestSearch_OperatorNeutralized_NoSyntaxError pins the core safety win:
// FTS5 operators and punctuation in raw user input never raise a syntax
// error on either backend (previously the SQLite MATCH path would).
func TestSearch_OperatorNeutralized_NoSyntaxError(t *testing.T) {
	dbs := ftsBackends(t, []*FTSArticle{{Title: "x", Body: "red green blue"}})
	nasty := []string{
		"red OR yellow", // boolean operator
		"gre*",          // prefix expansion
		"title:red",     // column scoping
		`red"green`,     // stray double quote
		"NEAR(red green)",
	}
	for name, db := range dbs {
		for _, term := range nasty {
			t.Run(name+"/"+term, func(t *testing.T) {
				_, err := engine.NewQuery[FTSArticle](db).Search(context.Background(), term)
				require.NoError(t, err, "raw operator input must not error")
			})
		}
	}
}

// TestSearch_OperatorNeutralized_NoBooleanOr pins that `red OR yellow` is not
// interpreted as a boolean OR: a doc matching only one of the words is never
// pulled in. Asserted per-backend (the exact hit set differs by stemming, an
// inherent backend gap) but the exclusion holds on both.
func TestSearch_OperatorNeutralized_NoBooleanOr(t *testing.T) {
	dbs := ftsBackends(t, []*FTSArticle{
		{Title: "redonly", Body: "red apple"},
		{Title: "yellowonly", Body: "yellow banana"},
		{Title: "both", Body: "red yellow plum"},
	})
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			hits, err := engine.NewQuery[FTSArticle](db).Search(context.Background(), "red OR yellow")
			require.NoError(t, err)
			got := titlesOf(hits)
			assert.NotContains(t, got, "redonly", "boolean OR must not pull in red-only doc")
			assert.NotContains(t, got, "yellowonly", "boolean OR must not pull in yellow-only doc")
		})
	}
}

// TestSearch_OperatorNeutralized_NoPrefix pins that a trailing * is a literal
// character, not an FTS5 prefix operator: `gre*` must not match `green`.
func TestSearch_OperatorNeutralized_NoPrefix(t *testing.T) {
	dbs := ftsBackends(t, []*FTSArticle{{Title: "greendoc", Body: "green meadow"}})
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			hits, err := engine.NewQuery[FTSArticle](db).Search(context.Background(), "gre*")
			require.NoError(t, err)
			assert.NotContains(t, titlesOf(hits), "greendoc", "* must not prefix-match green")
		})
	}
}

// titlesOf extracts titles for set-membership assertions (IDs differ per
// backend; titles are the shared identity here).
func titlesOf(articles []*FTSArticle) []string {
	titles := make([]string, len(articles))
	for i, a := range articles {
		titles[i] = a.Title
	}
	return titles
}

// TestSearch_BlankReturnsEmpty pins that an all-blank term short-circuits to
// an empty, non-nil result without touching the backend.
func TestSearch_BlankReturnsEmpty(t *testing.T) {
	dbs := ftsBackends(t, []*FTSArticle{{Title: "x", Body: "red green"}})
	for name, db := range dbs {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			for _, blank := range []string{"", "   ", "\t\n"} {
				hits, err := engine.NewQuery[FTSArticle](db).Search(ctx, blank)
				require.NoError(t, err)
				assert.Empty(t, hits)
				assert.NotNil(t, hits, "blank search returns a non-nil empty slice")
			}
		})
	}
}

// TestSearchRaw_AllowsRawFTS5 pins that SearchRaw reaches FTS5 query syntax
// on SQLite (a prefix query matches) while Search neutralises the same input
// to a literal that matches nothing — proving the two terminals differ.
func TestSearchRaw_AllowsRawFTS5(t *testing.T) {
	db := dentest.MustOpen(t, &FTSArticle{})
	ctx := context.Background()
	require.NoError(t, engine.Save(ctx, db, &FTSArticle{Title: "Go", Body: "golang rocks"}))

	raw, err := engine.NewQuery[FTSArticle](db).SearchRaw(ctx, "gola*")
	require.NoError(t, err)
	require.Len(t, raw, 1, "SearchRaw must honor the FTS5 prefix operator")

	literal, err := engine.NewQuery[FTSArticle](db).Search(ctx, "gola*")
	require.NoError(t, err)
	assert.Empty(t, literal, `Search quotes "gola*" as a literal token, matching nothing`)
}

func TestSearch_Postgres_NoResults(t *testing.T) {
	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &FTSArticle{})
	ctx := context.Background()

	require.NoError(t, engine.Save(ctx, db, &FTSArticle{Title: "Hello", Body: "World"}))

	results, err := engine.NewQuery[FTSArticle](db).Search(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, results)
}

// ftsUpgradeArticleV1/V2 simulate adding den:"fts" to an already-populated
// collection: both map to the same collection name, V1 predates the tag.
type ftsUpgradeArticleV1 struct {
	document.Base
	Title string `json:"title"`
}

func (ftsUpgradeArticleV1) DenSettings() engine.Settings {
	return engine.Settings{CollectionName: "fts_upgrade_articles"}
}

type ftsUpgradeArticleV2 struct {
	document.Base
	Title string `json:"title" den:"fts"`
}

func (ftsUpgradeArticleV2) DenSettings() engine.Settings {
	return engine.Settings{CollectionName: "fts_upgrade_articles"}
}

// TestSearch_FTSAddedToExistingCollection pins the schema-upgrade path on
// SQLite: documents saved before the fts tag existed must be searchable
// after the first Register with the tag — EnsureFTS backfills the index on
// first creation. PostgreSQL is unaffected by design (the generated
// tsvector column backfills at ALTER time).
func TestSearch_FTSAddedToExistingCollection(t *testing.T) {
	ctx := context.Background()
	dsn := "sqlite:///" + t.TempDir() + "/fts_upgrade.db"

	db, err := engine.OpenURL(ctx, dsn, engine.WithTypes(&ftsUpgradeArticleV1{}))
	require.NoError(t, err)
	require.NoError(t, engine.SaveAll(ctx, db, []*ftsUpgradeArticleV1{
		{Title: "Go Programming"},
		{Title: "Python Basics"},
	}))
	require.NoError(t, db.Close())

	db, err = engine.OpenURL(ctx, dsn, engine.WithTypes(&ftsUpgradeArticleV2{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	results, err := engine.NewQuery[ftsUpgradeArticleV2](db).Search(ctx, "Go")
	require.NoError(t, err)
	require.Len(t, results, 1, "pre-existing rows must be searchable after the fts upgrade")
	assert.Equal(t, "Go Programming", results[0].Title)
}

// TestSearch_FTSAddedToExistingCollection_Postgres pins the same upgrade
// contract on PostgreSQL: the generated tsvector column backfills at ALTER
// time today, and this must keep holding if the implementation ever changes.
func TestSearch_FTSAddedToExistingCollection_Postgres(t *testing.T) {
	ctx := context.Background()

	db := dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ftsUpgradeArticleV1{})
	require.NoError(t, engine.SaveAll(ctx, db, []*ftsUpgradeArticleV1{
		{Title: "Go Programming"},
		{Title: "Python Basics"},
	}))
	require.NoError(t, db.Close())

	db = dentest.MustOpenPostgres(t, dentest.PostgresURL(), &ftsUpgradeArticleV2{})

	results, err := engine.NewQuery[ftsUpgradeArticleV2](db).Search(ctx, "Go")
	require.NoError(t, err)
	require.Len(t, results, 1, "pre-existing rows must be searchable after the fts upgrade")
	assert.Equal(t, "Go Programming", results[0].Title)
}
