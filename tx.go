package den

import (
	"context"
	"fmt"
)

// Tx wraps a backend Transaction for use in RunInTransaction.
type Tx struct {
	db  *DB
	tx  Transaction
	ctx context.Context
}

// RunInTransaction executes fn within a transaction.
// If fn returns nil, the transaction is committed.
// If fn returns an error, the transaction is rolled back.
func RunInTransaction(ctx context.Context, db *DB, fn func(tx *Tx) error) error {
	btx, err := db.backend.Begin(ctx, true)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	tx := &Tx{db: db, tx: btx, ctx: ctx}

	defer func() {
		if r := recover(); r != nil {
			_ = btx.Rollback()
			panic(r)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := btx.Rollback(); rbErr != nil {
			return fmt.Errorf("rollback failed after %w: %w", err, rbErr)
		}
		return err
	}

	if err := btx.Commit(); err != nil {
		return fmt.Errorf("%w: %w", ErrTransactionFailed, err)
	}
	return nil
}

// TxFindByID retrieves a document by ID within a transaction.
func TxFindByID[T any](tx *Tx, id string) (*T, error) {
	col, err := collectionFor[T](tx.db)
	if err != nil {
		return nil, err
	}

	data, err := tx.tx.Get(tx.ctx, col.meta.Name, id)
	if err != nil {
		return nil, err
	}

	result := new(T)
	if err := tx.db.decode(data, result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	captureSnapshot(data, result)

	return result, nil
}

// TxInsert inserts a document within a transaction.
func TxInsert[T any](tx *Tx, document *T, opts ...CRUDOption) error {
	return insertCore(tx.ctx, tx.db, tx.tx, document, opts...)
}

// TxUpdate updates a document within a transaction.
func TxUpdate[T any](tx *Tx, document *T, opts ...CRUDOption) error {
	return updateCore(tx.ctx, tx.db, tx.tx, document, opts...)
}

// TxDelete deletes a document within a transaction.
func TxDelete[T any](tx *Tx, document *T, opts ...CRUDOption) error {
	return deleteCore(tx.ctx, tx.db, tx.tx, document, opts...)
}

// TxGet performs a raw key lookup within the transaction.
func TxGet(tx *Tx, collection, id string) ([]byte, error) {
	return tx.tx.Get(tx.ctx, collection, id)
}

// TxPut performs a raw key write within the transaction.
func TxPut(tx *Tx, collection, id string, data []byte) error {
	return tx.tx.Put(tx.ctx, collection, id, data)
}
