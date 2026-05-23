package engine

import (
	"context"
)

// Avg returns the average of the given field across matching documents.
//
// Scalar aggregates ignore Limit, Skip, Sort, After, and Before — they
// always operate on the full WHERE-filtered set.
func (qs QuerySet[T]) Avg(ctx context.Context, field string) (float64, error) {
	return qs.aggregate(ctx, OpAvg, field)
}

// Sum returns the sum of the given field across matching documents.
// See Avg for the modifier-applicability rules.
func (qs QuerySet[T]) Sum(ctx context.Context, field string) (float64, error) {
	return qs.aggregate(ctx, OpSum, field)
}

// Min returns the minimum value of the given field across matching documents.
// See Avg for the modifier-applicability rules.
func (qs QuerySet[T]) Min(ctx context.Context, field string) (float64, error) {
	return qs.aggregate(ctx, OpMin, field)
}

// Max returns the maximum value of the given field across matching documents.
// See Avg for the modifier-applicability rules.
func (qs QuerySet[T]) Max(ctx context.Context, field string) (float64, error) {
	return qs.aggregate(ctx, OpMax, field)
}

func (qs QuerySet[T]) aggregate(ctx context.Context, op AggregateOp, field string) (float64, error) {
	if err := qs.preflight(); err != nil {
		return 0, err
	}
	col, err := collectionFor[T](qs.scope.db())
	if err != nil {
		return 0, err
	}
	q := qs.buildBackendQuery(col)
	result, err := qs.scope.readWriter().Aggregate(ctx, col.meta.Name, op, field, q)
	if err != nil {
		return 0, err
	}
	if result == nil {
		return 0, nil
	}
	return *result, nil
}
