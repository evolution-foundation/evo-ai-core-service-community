//go:build integration

// Integration test that RUNS the backfill migration instead of matching its text.
// The migration is valid SQL that still cannot update a row under forced row level
// security, so a text assertion passes for a migration that aborts every deployment.
// It provisions a replica of evo_core_agents owned by a NOSUPERUSER NOBYPASSRLS role
// — the arrangement of docker-compose.restricted-db.yml and of the cluster — and
// executes the migration file through it.
//
// Run with:
//
//	EVO_TENANT_TEST_DATABASE_URL=postgres://evo_app:evo_app_pwd@localhost:5442/evo_community?sslmode=disable \
//	go test -tags=integration ./pkg/agent/service/...
package service

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

const (
	envBackfillDatabaseURL = "EVO_TENANT_TEST_DATABASE_URL"
	backfillTestSchema     = "backfill_migration_test"
	backfillOwnerRole      = "backfill_migration_owner"
	backfillOtherRole      = "backfill_migration_other"
	backfillRolePassword   = "backfill_migration_test"
	retiredModel           = "gpt-4.1-nano"
)

func openBackfillSuperuser(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv(envBackfillDatabaseURL)
	if dsn == "" {
		t.Skipf("%s not set — skipping integration", envBackfillDatabaseURL)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open superuser: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping superuser: %v", err)
	}
	return db, dsn
}

// dsnAs rewrites the superuser DSN to connect as one of the provisioned roles.
func dsnAs(t *testing.T, dsn, role string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parsing %s: %v", envBackfillDatabaseURL, err)
	}
	u.User = url.UserPassword(role, backfillRolePassword)
	return u.String()
}

// provisionBackfillReplica builds evo_core_agents as the cluster has it: owned by the
// migrating role, row level security enabled AND forced, one row on the retired id.
func provisionBackfillReplica(t *testing.T, admin *sql.DB) {
	t.Helper()
	dropBackfillReplica(t, admin)

	stmts := []string{
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS", backfillOwnerRole, backfillRolePassword),
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS", backfillOtherRole, backfillRolePassword),
		fmt.Sprintf("CREATE SCHEMA %s AUTHORIZATION %s", backfillTestSchema, backfillOwnerRole),
		fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", backfillTestSchema, backfillOtherRole),
		fmt.Sprintf(`CREATE TABLE %s.evo_core_agents (
			id serial PRIMARY KEY, tenant_id uuid, model text, type text)`, backfillTestSchema),
		fmt.Sprintf(`INSERT INTO %s.evo_core_agents (tenant_id, model, type)
			VALUES ('11111111-1111-1111-1111-111111111111', '%s', 'llm')`, backfillTestSchema, retiredModel),
		fmt.Sprintf("ALTER TABLE %s.evo_core_agents OWNER TO %s", backfillTestSchema, backfillOwnerRole),
		fmt.Sprintf("ALTER TABLE %s.evo_core_agents ENABLE ROW LEVEL SECURITY", backfillTestSchema),
		fmt.Sprintf("ALTER TABLE %s.evo_core_agents FORCE ROW LEVEL SECURITY", backfillTestSchema),
		fmt.Sprintf(`CREATE POLICY tenant_isolation ON %s.evo_core_agents
			USING (tenant_id = (NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)`, backfillTestSchema),
		fmt.Sprintf("GRANT SELECT, UPDATE ON %s.evo_core_agents TO %s", backfillTestSchema, backfillOtherRole),
	}
	for _, stmt := range stmts {
		if _, err := admin.Exec(stmt); err != nil {
			t.Fatalf("provisioning the replica (%s): %v", stmt, err)
		}
	}
}

func dropBackfillReplica(t *testing.T, admin *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", backfillTestSchema),
		fmt.Sprintf("DROP OWNED BY %s", backfillOtherRole),
		fmt.Sprintf("DROP OWNED BY %s", backfillOwnerRole),
		fmt.Sprintf("DROP ROLE IF EXISTS %s", backfillOtherRole),
		fmt.Sprintf("DROP ROLE IF EXISTS %s", backfillOwnerRole),
	} {
		// The DROP OWNED calls fail when the role is absent, which is the clean state.
		_, _ = admin.Exec(stmt)
	}
}

// runBackfillMigrationAs executes the migration file the way the runner does: the whole
// file in one Exec, with the replica resolving the unqualified table name.
func runBackfillMigrationAs(t *testing.T, dsn, role string) error {
	t.Helper()
	db, err := sql.Open("postgres", dsnAs(t, dsn, role))
	if err != nil {
		t.Fatalf("sql.Open %s: %v", role, err)
	}
	defer db.Close()

	if _, err := db.Exec(fmt.Sprintf("SET search_path = %s", backfillTestSchema)); err != nil {
		t.Fatalf("search_path for %s: %v", role, err)
	}
	_, err = db.Exec(readBackfillMigration(t))
	return err
}

func TestBackfillMigrationUpdatesUnderForcedRowSecurity(t *testing.T) {
	admin, dsn := openBackfillSuperuser(t)
	defer admin.Close()
	provisionBackfillReplica(t, admin)
	defer dropBackfillReplica(t, admin)

	if err := runBackfillMigrationAs(t, dsn, backfillOwnerRole); err != nil {
		t.Fatalf("the migration must run as the role that owns the table: %v", err)
	}

	var model string
	query := fmt.Sprintf("SELECT model FROM %s.evo_core_agents", backfillTestSchema)
	if err := admin.QueryRow(query).Scan(&model); err != nil {
		t.Fatalf("reading the agent back: %v", err)
	}
	if model != defaultRepairModel {
		t.Errorf("agent is on %q; the backfill must move it to %q", model, defaultRepairModel)
	}

	var forced bool
	forcedQuery := fmt.Sprintf("SELECT relforcerowsecurity FROM pg_class WHERE oid = '%s.evo_core_agents'::regclass", backfillTestSchema)
	if err := admin.QueryRow(forcedQuery).Scan(&forced); err != nil {
		t.Fatalf("reading the row security state back: %v", err)
	}
	if !forced {
		t.Error("the migration left row level security unforced; tenant isolation would be open to the owner")
	}
}

func TestBackfillMigrationFailsLoudlyForANonOwner(t *testing.T) {
	admin, dsn := openBackfillSuperuser(t)
	defer admin.Close()
	provisionBackfillReplica(t, admin)
	defer dropBackfillReplica(t, admin)

	if err := runBackfillMigrationAs(t, dsn, backfillOtherRole); err == nil {
		t.Fatal("a role that cannot cross the policy must fail, not update zero rows in silence")
	}

	var model string
	query := fmt.Sprintf("SELECT model FROM %s.evo_core_agents", backfillTestSchema)
	if err := admin.QueryRow(query).Scan(&model); err != nil {
		t.Fatalf("reading the agent back: %v", err)
	}
	if model != retiredModel {
		t.Errorf("agent is on %q; a failed migration must leave the row untouched", model)
	}
}
