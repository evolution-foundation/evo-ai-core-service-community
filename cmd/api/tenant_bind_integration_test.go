//go:build integration && enterprise

package main

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// Proves the bind is real: the value the gate authorized is readable as
// app.current_tenant_id inside the transaction handed to the request, and
// is gone once the transaction ends. Read-only — it opens a transaction,
// reads a session setting and rolls back, so it never writes data.
func TestSQLBinderSetsTenantGUC(t *testing.T) {
	dsn := os.Getenv(envDatabaseURL)
	if dsn == "" {
		t.Skipf("%s not set — skipping integration", envDatabaseURL)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("postgres unreachable: %v", err)
	}

	const tenantID = "11111111-2222-3333-4444-555555555555"
	bound, release, err := newSQLBinder(db).bind(ctx, tenantID)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	tx, ok := txFromContext(bound)
	if !ok {
		t.Fatal("bound context carries no transaction: the scoped conn would never reach runtimecontext")
	}

	var guc string
	if err := tx.QueryRowContext(ctx, `SELECT current_setting('app.current_tenant_id', true)`).Scan(&guc); err != nil {
		t.Fatalf("read GUC: %v", err)
	}
	if guc != tenantID {
		t.Fatalf("app.current_tenant_id = %q, want %q", guc, tenantID)
	}

	release(false)

	// is_local=true means the GUC dies with the transaction; the pooled
	// connection must not carry it to the next request.
	var after string
	if err := db.QueryRowContext(ctx, `SELECT current_setting('app.current_tenant_id', true)`).Scan(&after); err != nil {
		t.Fatalf("read GUC after release: %v", err)
	}
	if after == tenantID {
		t.Fatal("app.current_tenant_id survived the transaction: it would leak onto the next request")
	}
}
