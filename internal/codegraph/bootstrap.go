package codegraph

import (
	"context"
	"fmt"
	"log/slog"
)

// Preflight checks that the connected role has the minimum database-level
// privileges go-code cannot acquire for itself. Call once at startup before
// registering any tools; return the error to the caller to log and exit(1).
//
// Two privileges CANNOT be self-granted without being the database owner or
// having GRANT OPTION:
//
//  1. USAGE on schema ag_catalog — needed by LOAD age and every cypher() call.
//     Fix: GRANT USAGE ON SCHEMA ag_catalog TO <role>;
//
//  2. CREATE on the current database — needed because ag_catalog.create_graph()
//     issues CREATE SCHEMA internally.
//     Fix: GRANT CREATE ON DATABASE <db> TO <role>;
//
// If AGE is not installed, both checks are skipped (go-code degrades to
// SQL-only mode without graph features).
func (s *Store) Preflight(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("preflight: acquire connection: %w", err)
	}
	defer conn.Release()

	var role string
	if err := conn.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		return fmt.Errorf("preflight: identify current_user: %w", err)
	}

	var ageInstalled bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'age')`,
	).Scan(&ageInstalled); err != nil {
		slog.Warn("preflight: cannot check AGE installation", slog.Any("error", err))
	}

	if !ageInstalled {
		slog.Info("codegraph: preflight OK (AGE not installed, graph features disabled)",
			slog.String("role", role))
		return nil
	}

	var missing []string

	var agCatalogUsage bool
	if err := conn.QueryRow(ctx,
		`SELECT has_schema_privilege(current_user, 'ag_catalog', 'USAGE')`,
	).Scan(&agCatalogUsage); err != nil {
		slog.Warn("preflight: cannot check ag_catalog USAGE privilege", slog.Any("error", err))
	} else if !agCatalogUsage {
		missing = append(missing,
			fmt.Sprintf("GRANT USAGE ON SCHEMA ag_catalog TO %s;", role))
	}

	var dbCreate bool
	if err := conn.QueryRow(ctx,
		`SELECT has_database_privilege(current_user, current_database(), 'CREATE')`,
	).Scan(&dbCreate); err != nil {
		slog.Warn("preflight: cannot check database CREATE privilege", slog.Any("error", err))
	} else if !dbCreate {
		var dbName string
		_ = conn.QueryRow(ctx, `SELECT current_database()`).Scan(&dbName)
		missing = append(missing,
			fmt.Sprintf("GRANT CREATE ON DATABASE %s TO %s;", dbName, role))
	}

	if len(missing) == 0 {
		slog.Info("codegraph: preflight OK", slog.String("role", role))
		return nil
	}

	for _, stmt := range missing {
		slog.Error("codegraph: missing privilege — operator must run this SQL as a superuser",
			slog.String("fix_sql", stmt))
	}
	return fmt.Errorf(
		"codegraph: role %q is missing %d database-level privilege(s); "+
			"run the GRANT statements logged above as a superuser, then restart go-code",
		role, len(missing),
	)
}
