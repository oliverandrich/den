package postgres

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// minPGVersion is the minimum supported PostgreSQL major version.
// PostgreSQL 13 is required for: GENERATED ALWAYS AS ... STORED (PG 12),
// plus headroom since PG 12 reached EOL in November 2024.
const minPGVersion = 13

// serverVersion queries the PostgreSQL server_version_num setting and returns it as an integer.
// SHOW returns text, so we scan into a string and convert.
func serverVersion(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var raw string
	err := pool.QueryRow(ctx, "SHOW server_version_num").Scan(&raw)
	if err != nil {
		return 0, fmt.Errorf("query server version: %w", err)
	}
	num, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse server version %q: %w", raw, err)
	}
	return num, nil
}

// checkMinVersion validates that the server version meets the minimum requirement.
func checkMinVersion(versionNum int) error {
	major, minor := parseVersionNum(versionNum)
	if major < minPGVersion {
		return fmt.Errorf("den requires PostgreSQL %d or later, got %s", minPGVersion, formatVersion(major, minor))
	}
	return nil
}

// parseVersionNum extracts major and minor version from PostgreSQL's server_version_num.
// The format is MMmmpp (e.g., 160002 = 16.0.2, 130005 = 13.0.5).
// Since PG 10+, major is the first component and minor is the patch level.
func parseVersionNum(num int) (major, minor int) {
	major = num / 10000
	minor = num % 100
	return major, minor
}

// formatVersion returns a human-readable version string like "16.2".
func formatVersion(major, minor int) string {
	return fmt.Sprintf("%d.%d", major, minor)
}
