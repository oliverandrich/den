// Package postgres implements the Den backend for PostgreSQL via
// jackc/pgx/v5.
//
// # Test layering
//
// Three test surfaces cover this backend, each with a distinct purpose:
//
//   - **pgxmock unit tests** (this package) for postgres-only error
//     paths: SQLSTATE → den-sentinel mapping, SQL emission for
//     advisory locks and FOR UPDATE variants, mid-stream iterator
//     errors, pool acquire failures. No live PostgreSQL required.
//   - **engine/parity_test.go** for cross-backend behavior
//     parity (CRUD, query operators, soft-delete, links, transactions).
//     Runs against both SQLite and a real PostgreSQL connection.
//   - **Real-PostgreSQL integration tests** for concurrency-driven
//     failures the mock cannot model: cursor pinning ("conn busy" on
//     in-loop writes), transaction isolation behavior, advisory-lock
//     release semantics, deadlock retries.
//
// When adding a test, pick the surface that matches the contract under
// test. pgxmock is the wrong tool for anything concurrency-driven —
// such tests belong with the live-PG suite.
package postgres
