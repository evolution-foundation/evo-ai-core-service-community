//go:build integration && enterprise

// Integration test for EVO-1625 (GO-5): proves the enterprise wire in
// cmd/api (installRuntimeScope + ginAdapter) plugs Row-Level Security
// fail-closed at the gin layer.
//
// Mirrors evo-enterprise-licensing-ruby/spec/integration/rls_leak_spec.rb
// and evo-enterprise-licensing-go/tenant/integration_test.go (SDK), but
// drives the full gin → tenant.Middleware → handler chain instead of the
// raw http.Handler, which is exactly the surface SDK tests do NOT cover.
//
// Run with:
//
//	EVO_TENANT_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/evo_community?sslmode=disable \
//	go test -tags="integration enterprise" ./cmd/api/...
//
// The test connects as superuser to provision a synthetic RLS table
// (tenant_test_rls_demo) and the gem-owned membership table
// (evo_enterprise_tenant_memberships, the name installRuntimeScope's
// boot-check expects), then connects through a NOSUPERUSER NOBYPASSRLS
// role (mirroring F0.2 / EVO-1620) for the assertions.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/evolution-foundation/evo-enterprise-licensing-go/tenant"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	envDatabaseURL    = "EVO_TENANT_TEST_DATABASE_URL"
	envAllowDestroy   = "EVO_TENANT_TEST_ALLOW_DESTRUCTIVE"
	testRole          = "evo_app_tenant_test"
	testRolePass      = "evo_app_tenant_test"
	testRLSTable      = "tenant_test_rls_demo"
)

func openSuperuser(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(envDatabaseURL)
	if dsn == "" {
		t.Skipf("%s not set — skipping integration", envDatabaseURL)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql.Open superuser: %v", err)
	}
	db.SetMaxOpenConns(4)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("postgres unreachable: %v", err)
	}
	return db
}

func openAppGorm(t *testing.T) *gorm.DB {
	t.Helper()
	superDSN := os.Getenv(envDatabaseURL)
	appDSN, err := swapDSNUser(superDSN, testRole, testRolePass)
	if err != nil {
		t.Fatalf("swap DSN: %v", err)
	}
	db, err := gorm.Open(postgres.Open(appDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open app role: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("gorm DB(): %v", err)
	}
	sqlDB.SetMaxOpenConns(4)
	return db
}

// swapDSNUser rewrites the user:pass segment of a postgres:// URL.
// Kept string-y on purpose; the test DSN format is documented above.
func swapDSNUser(dsn, user, pass string) (string, error) {
	const prefix = "postgres://"
	if !strings.HasPrefix(dsn, prefix) {
		return "", fmt.Errorf("DSN must start with %q", prefix)
	}
	rest := dsn[len(prefix):]
	at := strings.Index(rest, "@")
	if at < 0 {
		return "", fmt.Errorf("DSN has no user@host segment")
	}
	return prefix + user + ":" + pass + "@" + rest[at+1:], nil
}

// guardDestructive refuses to run if the target database appears to hold
// real data: it aborts when tenant.MembershipTable already exists with
// rows, unless EVO_TENANT_TEST_ALLOW_DESTRUCTIVE=1 is set. This catches
// the foot-gun where EVO_TENANT_TEST_DATABASE_URL is accidentally pointed
// at a shared dev/staging DB — provision() would otherwise DROP the
// canonical evo_enterprise_tenant_memberships table.
func guardDestructive(t *testing.T, super *sql.DB) {
	t.Helper()
	if os.Getenv(envAllowDestroy) == "1" {
		return
	}
	ctx := context.Background()
	var exists bool
	if err := super.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
		tenant.MembershipTable,
	).Scan(&exists); err != nil {
		t.Fatalf("guard: probe membership table existence: %v", err)
	}
	if !exists {
		return
	}
	var count int
	if err := super.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM %s`, tenant.MembershipTable),
	).Scan(&count); err != nil {
		t.Fatalf("guard: count membership rows: %v", err)
	}
	if count > 0 {
		t.Fatalf("guard: refusing to drop %s — table has %d row(s); "+
			"point %s at a disposable database, or set %s=1 to override",
			tenant.MembershipTable, count, envDatabaseURL, envAllowDestroy)
	}
}

func provision(t *testing.T, super *sql.DB) {
	t.Helper()
	guardDestructive(t, super)
	ctx := context.Background()
	stmts := []string{
		// ALTER (not just CREATE IF NOT EXISTS) so a pre-existing role
		// with a different password gets reset to the known credential —
		// otherwise openAppGorm fails with a confusing auth error.
		fmt.Sprintf(`DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN
				CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS;
			ELSE
				ALTER ROLE %s WITH LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS;
			END IF;
		END $$`, testRole, testRole, testRolePass, testRole, testRolePass),

		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,

		fmt.Sprintf(`DROP TABLE IF EXISTS %s`, testRLSTable),
		fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tenant.MembershipTable),

		// Membership table uses the canonical name so installRuntimeScope's
		// boot-check (SELECT 1 FROM tenant.MembershipTable LIMIT 0) passes
		// and SQLAuthorizer's prod query hits the right relation. F0.1 §11
		// says evo_core_* / evo_enterprise_* tables are gem-owned in prod;
		// the test owns them only for the duration of provision/teardown.
		fmt.Sprintf(`CREATE TABLE %s (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id uuid NOT NULL,
			tenant_id uuid NULL,
			role text NOT NULL
		)`, tenant.MembershipTable),

		fmt.Sprintf(`CREATE TABLE %s (
			id int PRIMARY KEY,
			tenant_id uuid NOT NULL,
			payload text NOT NULL
		)`, testRLSTable),

		fmt.Sprintf(`ALTER TABLE %s ENABLE ROW LEVEL SECURITY`, testRLSTable),
		fmt.Sprintf(`ALTER TABLE %s FORCE ROW LEVEL SECURITY`, testRLSTable),
		fmt.Sprintf(`CREATE POLICY tenant_iso ON %s
			USING      (tenant_id::text = current_setting('app.current_tenant_id', true))
			WITH CHECK (tenant_id::text = current_setting('app.current_tenant_id', true))`, testRLSTable),

		fmt.Sprintf(`GRANT SELECT, INSERT ON %s TO %s`, testRLSTable, testRole),
		fmt.Sprintf(`GRANT SELECT ON %s TO %s`, tenant.MembershipTable, testRole),
	}
	for _, s := range stmts {
		if _, err := super.ExecContext(ctx, s); err != nil {
			t.Fatalf("provision %q: %v", s, err)
		}
	}
}

func teardown(super *sql.DB) {
	ctx := context.Background()
	_, _ = super.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, testRLSTable))
	_, _ = super.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tenant.MembershipTable))
}

func seed(t *testing.T, super *sql.DB, userID, tenantA, tenantB string) {
	t.Helper()
	ctx := context.Background()
	if _, err := super.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (user_id, tenant_id, role) VALUES ($1, $2, 'tenant_user')`, tenant.MembershipTable),
		userID, tenantA,
	); err != nil {
		t.Fatalf("seed membership A: %v", err)
	}
	if _, err := super.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (id, tenant_id, payload) VALUES (1, $1, 'A')`, testRLSTable),
		tenantA,
	); err != nil {
		t.Fatalf("seed rls A: %v", err)
	}
	if _, err := super.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (id, tenant_id, payload) VALUES (2, $1, 'B')`, testRLSTable),
		tenantB,
	); err != nil {
		t.Fatalf("seed rls B: %v", err)
	}
}

// buildEngine assembles a minimal gin.Engine that mimics the wiring in
// main(): an upstream middleware that publishes user_id on the gin
// context (mirroring EvoAuth's contract), then installRuntimeScope on
// /api/v1, then a probe handler that uses the bound *sql.Tx to query
// the synthetic RLS table.
func buildEngine(t *testing.T, db *gorm.DB, userID string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	// Stand-in for EvoAuth: publishes user_id on both gin.Context.Keys
	// and request context (the gin adapter inside installRuntimeScope
	// reads from ctx.Value via DefaultUserIDExtractor).
	v1.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		//nolint:staticcheck,SA1029 // matches EvoAuth contract used by DefaultUserIDExtractor
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), "user_id", userID))
		c.Next()
	})
	installRuntimeScope(v1, db)
	v1.GET("/probe", func(c *gin.Context) {
		tx, ok := tenant.TxFromContext(c.Request.Context())
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no tx in ctx"})
			return
		}
		var bound string
		_ = tx.QueryRowContext(c.Request.Context(),
			`SELECT current_setting('app.current_tenant_id', true)`).Scan(&bound)
		rows, err := tx.QueryContext(c.Request.Context(),
			fmt.Sprintf(`SELECT id, payload FROM %s ORDER BY id`, testRLSTable))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()
		var seen []string
		for rows.Next() {
			var id int
			var payload string
			if err := rows.Scan(&id, &payload); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			seen = append(seen, fmt.Sprintf("%d=%s", id, payload))
		}
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"bound": bound, "rows": seen})
	})
	return r
}

func TestIntegration_RLSLeak_WireBindsAndFailsClosed(t *testing.T) {
	super := openSuperuser(t)
	defer super.Close()
	provision(t, super)
	// teardown via defer (LIFO) so it runs BEFORE super.Close fires.
	// t.Cleanup is executed by the test runner via a defer registered
	// inside tRunner before user code, so user defers run first and
	// cleanups last — registering teardown via t.Cleanup would call it
	// after super is already closed, silently no-op'ing the DROPs.
	defer teardown(super)

	userID := uuid.NewString()
	tenantA := uuid.NewString()
	tenantB := uuid.NewString()
	seed(t, super, userID, tenantA, tenantB)

	db := openAppGorm(t)
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	r := buildEngine(t, db, userID)

	t.Run("Authorized_TenantA_SeesOnlyA", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
		req.Header.Set(tenant.HeaderTenantID, tenantA)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status: want 200, got %d (body=%s)", rr.Code, rr.Body.String())
		}
		var body struct {
			Bound string   `json:"bound"`
			Rows  []string `json:"rows"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v (raw=%s)", err, rr.Body.String())
		}
		if body.Bound != tenantA {
			t.Fatalf("GUC: want %s, got %q", tenantA, body.Bound)
		}
		if len(body.Rows) != 1 || body.Rows[0] != "1=A" {
			t.Fatalf("RLS leak through gin wire: want [1=A], got %v", body.Rows)
		}
	})

	t.Run("Forbidden_TenantB_NoMembership", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
		req.Header.Set(tenant.HeaderTenantID, tenantB)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Fatalf("status: want 403, got %d (body=%s)", rr.Code, rr.Body.String())
		}
		// Defense in depth: prove the probe handler never ran. If the wire
		// ever regressed to "bind first, authorize later", a 403 + leaked
		// rows in the body would be a real leak the status check misses.
		body := rr.Body.String()
		if strings.Contains(body, `"rows"`) || strings.Contains(body, `"bound"`) {
			t.Fatalf("forbidden response leaked probe payload: %s", body)
		}
		if strings.Contains(body, "=B") || strings.Contains(body, "=A") {
			t.Fatalf("forbidden response leaked RLS rows: %s", body)
		}
	})

	t.Run("Forbidden_NoHeader", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Fatalf("status: want 403, got %d (body=%s)", rr.Code, rr.Body.String())
		}
	})

	t.Run("FailClosed_PoolWithoutBind_ZeroRows", func(t *testing.T) {
		// Query the app-role pool directly, bypassing the wire. With
		// FORCE ROW LEVEL SECURITY + a policy that compares against
		// current_setting('app.current_tenant_id', true) and no NULL
		// escape, an unbound session must see zero rows — not an error,
		// which would let a caller distinguish "tenant exists but empty"
		// from "no tenant bound" and reintroduce a leak.
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("gorm DB(): %v", err)
		}
		var n int
		if err := sqlDB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s`, testRLSTable)).Scan(&n); err != nil {
			t.Fatalf("count without bind: %v", err)
		}
		if n != 0 {
			t.Fatalf("fail-closed broken: want 0 rows without bind, got %d", n)
		}
	})

	t.Run("PolicyHasNoNullEscape", func(t *testing.T) {
		// Inspect pg_policy to make sure the policy expression doesn't
		// contain an `OR current_setting IS NULL` style escape that
		// would silently disable RLS on unbound sessions. Mirrors the
		// pg_policy check at the bottom of the Ruby rls_leak_spec.rb.
		rows, err := super.Query(`
			SELECT pg_get_expr(polqual, polrelid), pg_get_expr(polwithcheck, polrelid)
			FROM pg_policy p
			JOIN pg_class c ON c.oid = p.polrelid
			WHERE c.relname = $1
		`, testRLSTable)
		if err != nil {
			t.Fatalf("pg_policy query: %v", err)
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			count++
			var using, withCheck sql.NullString
			if err := rows.Scan(&using, &withCheck); err != nil {
				t.Fatalf("scan: %v", err)
			}
			for _, expr := range []sql.NullString{using, withCheck} {
				if !expr.Valid {
					continue
				}
				low := strings.ToLower(expr.String)
				if strings.Contains(low, "is null") {
					t.Fatalf("policy has NULL escape: %s", expr.String)
				}
				if strings.Contains(low, "or true") {
					t.Fatalf("policy has unconditional escape: %s", expr.String)
				}
			}
		}
		if count == 0 {
			t.Fatalf("no policies found on %s — RLS not configured", testRLSTable)
		}
	})
}
