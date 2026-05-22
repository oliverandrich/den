// Package sqlite implements the Den backend for SQLite via
// modernc.org/sqlite (pure Go, no CGO).
//
// # Test layering
//
// Three test surfaces cover this backend, each with a distinct purpose:
//
//   - **go-sqlmock unit tests** (this package, mock_test.go) for
//     error paths that the live driver can't easily trigger:
//     Prepare-failure branches in getStmts, exec/query errors on
//     Put/Delete/Query/Count/Exists/Aggregate/GroupBy, ErrNoRows →
//     den.ErrNotFound mapping, mid-stream iterator errors, DropIndex
//     failures. No filesystem access required.
//   - **engine/parity_test.go** for cross-backend behavior
//     parity (CRUD, query operators, soft-delete, links, transactions).
//     Runs against both SQLite and a real PostgreSQL connection.
//   - **File-backed integration tests** (sqlite_test.go, fts_test.go)
//     for behavior tied to the real driver: PRAGMA application, FTS5
//     trigger semantics, parent-directory auto-creation, busy-timeout
//     handling.
//
// When adding a test, pick the surface that matches the contract under
// test. go-sqlmock is the wrong tool for anything that depends on real
// SQLite semantics (PRAGMA effects, FTS5, JSON1 functions) — such
// tests belong with the file-backed suite.
package sqlite
