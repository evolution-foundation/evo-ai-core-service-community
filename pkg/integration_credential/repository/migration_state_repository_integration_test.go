//go:build integration

// Integration test that runs the tool and MCP pending-secret counts, and the
// migration state built on them, against a schema built by every migration of
// the service, so a filter on a column a migration dropped fails here instead
// of in the migration-state endpoint.
//
// Run with:
//
//	EVO_TENANT_TEST_DATABASE_URL=postgres://evo_app:evo_app_pwd@localhost:5442/evo_community?sslmode=disable \
//	go test -tags=integration -run TestPendingInlineSecrets ./pkg/integration_credential/repository/...
package repository_test

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"evo-ai-core-service/pkg/integration_credential/model"
	"evo-ai-core-service/pkg/integration_credential/repository"
	"evo-ai-core-service/pkg/integration_credential/service"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const migrationStateTestSchema = "migration_state_it"

func openMigratedSchema(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv(envReferenceDatabaseURL)
	if dsn == "" {
		t.Skipf("%s not set — skipping integration", envReferenceDatabaseURL)
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open %s: %v", envReferenceDatabaseURL, err)
	}
	dropSchema := func() {
		if err := admin.Exec("DROP SCHEMA IF EXISTS " + migrationStateTestSchema + " CASCADE").Error; err != nil {
			t.Errorf("drop schema: %v", err)
		}
	}
	dropSchema()
	if err := admin.Exec("CREATE SCHEMA " + migrationStateTestSchema).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		dropSchema()
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	// public stays on the path for uuid_generate_v4; the tables are created
	// first in the test schema, so unqualified names resolve there.
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", envReferenceDatabaseURL, err)
	}
	query := u.Query()
	query.Set("search_path", migrationStateTestSchema+",public")
	u.RawQuery = query.Encode()

	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open replica: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	migrations, err := filepath.Glob(filepath.Join("..", "..", "..", "migrations", "*.up.sql"))
	if err != nil || len(migrations) == 0 {
		t.Fatalf("no migrations found (%v)", err)
	}
	sort.Strings(migrations)
	for _, path := range migrations {
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration: %v", err)
		}
		if err := db.Exec(string(sql)).Error; err != nil {
			t.Fatalf("apply %s: %v", filepath.Base(path), err)
		}
	}

	for _, table := range []string{"evo_core_custom_tools", "evo_core_custom_mcp_servers"} {
		var schema string
		if err := db.Raw("SELECT relnamespace::regnamespace::text FROM pg_class WHERE oid = ?::regclass", table).
			Scan(&schema).Error; err != nil {
			t.Fatalf("resolve %s: %v", table, err)
		}
		if schema != migrationStateTestSchema {
			t.Fatalf("%s resolves to %q, outside the test schema", table, schema)
		}
		if db.Migrator().HasColumn(table, "is_active") {
			t.Fatalf("%s still has is_active after the migrations", table)
		}
	}

	return db
}

func TestPendingInlineSecretsCountsToolsAndMcpsOnTheMigratedSchema(t *testing.T) {
	db := openMigratedSchema(t)
	repo := repository.NewMigrationStateRepository(db)
	state := service.NewMigrationState(repo)

	// Empty stores are retired, which a failed read can never answer.
	retired, err := state.Retired(context.Background())
	if err != nil {
		t.Fatalf("Retired on empty stores: %v", err)
	}
	for _, consumer := range []string{model.ConsumerCustomTools, model.ConsumerCustomMcpServers} {
		if !retired[consumer] {
			t.Errorf("%s: not retired with nothing inline", consumer)
		}
	}

	exec := func(sql string, args ...interface{}) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	tool := func(name, headers, refs string) {
		exec(`INSERT INTO evo_core_custom_tools
			(name, method, endpoint, headers, path_params, query_params, body_params, error_handling, values, credential_refs)
			VALUES (?, 'GET', 'https://example.test', ?::json, '{}', '{}', '{}', '{}', '{}', ?::jsonb)`,
			name, headers, refs)
	}
	mcp := func(name, headers, refs string) {
		exec(`INSERT INTO evo_core_custom_mcp_servers (name, url, headers, credential_refs)
			VALUES (?, 'https://example.test', ?::json, ?::jsonb)`,
			name, headers, refs)
	}
	ref := `{"Authorization": "6f1c2a8e-5b7d-4c3a-9e2f-0a1b2c3d4e5f"}`

	tool("inline bearer", `{"Authorization": "Bearer abc"}`, `{}`)
	tool("inline key, upper case", `{"X-API-KEY": "abc"}`, `{}`)
	tool("replaced by a reference", `{"Authorization": "Bearer abc"}`, ref)
	tool("not an auth header", `{"Content-Type": "application/json"}`, `{}`)
	tool("empty auth header", `{"x-api-key": ""}`, `{}`)
	tool("no headers", `{}`, `{}`)

	mcp("inline token", `{"x-auth-token": "abc"}`, `{}`)
	mcp("replaced by a reference", `{"Authorization": "Bearer abc"}`, ref)
	mcp("not an auth header", `{"Accept": "text/event-stream"}`, `{}`)

	for consumer, want := range map[string]int64{
		model.ConsumerCustomTools:      2,
		model.ConsumerCustomMcpServers: 1,
	} {
		got, err := repo.PendingInlineSecrets(context.Background(), consumer)
		if err != nil {
			t.Fatalf("%s: %v", consumer, err)
		}
		if got != want {
			t.Errorf("%s: %d inline secrets pending, want %d", consumer, got, want)
		}
	}

	retired, err = state.Retired(context.Background())
	if err != nil {
		t.Fatalf("Retired with inline secrets: %v", err)
	}
	for _, consumer := range []string{model.ConsumerCustomTools, model.ConsumerCustomMcpServers} {
		if retired[consumer] {
			t.Errorf("%s: retired while secrets are still inline", consumer)
		}
	}
}
