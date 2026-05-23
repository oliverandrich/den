package engine

// Scope is the common parameter type for every CRUD entry point that works
// both outside and inside a transaction. It is sealed to *DB and *Tx — the
// gateway methods are unexported so external types cannot implement it, and
// callers can only obtain a Scope by passing one of the two concrete types.
//
// The idiom mirrors the implicit DBTX pattern used around database/sql
// (where *sql.DB and *sql.Tx share the query surface) but is explicit here
// so the compiler can document and enforce which operations accept either.
type Scope interface {
	readWriter() ReadWriter
	db() *DB
}
