//go:build enterprise

package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"evo-ai-core-service/pkg/evoextensions/runtimecontext"
	"evo-ai-core-service/pkg/evoextensions/tenantmembership"
	"evo-ai-core-service/pkg/evoextensions/tenantscope"
	"evo-ai-core-service/pkg/evoextensions/tenantstamp"

	"github.com/evolution-foundation/evo-enterprise-licensing-go/tenant"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// installRuntimeScope swaps the no-op community Default() scope for an
// EnterpriseScope backed by the membership table, then registers the
// tenant middleware on the v1 group *after* EvoAuth, so user_id is already
// in ctx when the membership check runs.
func installRuntimeScope(v1 *gin.RouterGroup, db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("enterprise wiring: cannot reach underlying *sql.DB: %v", err)
	}

	// Fail-fast: without the enterprise migrations applied, every request
	// would hit `relation does not exist` and surface as a 403. Detecting it
	// at boot makes the failure mode obvious instead of looking like a flood
	// of legitimate auth denials.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := sqlDB.ExecContext(ctx,
		`SELECT 1 FROM `+tenant.MembershipTable+` LIMIT 0`); err != nil {
		log.Fatalf("enterprise wiring: membership table %q unreachable — "+
			"apply the enterprise migrations before booting: %v",
			tenant.MembershipTable, err)
	}

	// The SDK authorizer fails OPEN when EVO_LICENSING_PERMISSIVE_MEMBERSHIP
	// is set, which is the ecosystem default. Gate it behind a membership
	// check that ignores that flag, so a non-member is refused before any
	// transaction binds app.current_tenant_id.
	checker := tenantmembership.NewSQLChecker(tenantmembership.NewSQLQuerier(sqlDB))
	scope := tenant.NewEnterpriseScope(
		newEnforcedAuthorizer(checker, tenant.NewSQLAuthorizer(sqlDB)),
	)

	mw := tenant.Middleware(scope, nil) // nil → DefaultUserIDExtractor reads ctx.Value("user_id")
	v1.Use(ginAdapter(mw))
	log.Println("enterprise wiring: tenant middleware installed on /api/v1 (membership enforced)")

	// Stamp tenant_id on every INSERT into evo_core_* from the request
	// context. Fail-closed: with no tenant bound the field stays uuid.Nil
	// and the row-level security policy rejects the INSERT.
	if err := db.Use(tenantstamp.Plugin{}); err != nil {
		log.Fatalf("enterprise wiring: register tenant_stamp plugin: %v", err)
	}
	log.Println("enterprise wiring: tenant_stamp plugin registered")

	// Read-side symmetric of tenant_stamp: routes tenant-scoped reads onto
	// the per-request scope-bound tx and fails closed when unbound, so a read
	// never falls through to the global pool.
	if err := db.Use(tenantscope.Plugin{}); err != nil {
		log.Fatalf("enterprise wiring: register tenant_scope plugin: %v", err)
	}
	log.Println("enterprise wiring: tenant_scope plugin registered")
}

// ginAdapter bridges a net/http middleware into the gin chain. It runs the
// wrapped handler in-process so that 403 short-circuits abort the gin
// chain, the request context carrying the bound tenant id and its
// dedicated conn reaches downstream handlers, and the ReleaseFunc fires
// when the wrapped handler returns.
//
// It also bridges the bound tenant id onto the community runtimecontext
// key, so downstream community paths can read it without importing the
// enterprise SDK.
func ginAdapter(mw func(http.Handler) http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var aborted bool
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if tid := tenant.TenantIDFromContext(ctx); tid != "" {
				ctx = runtimecontext.WithID(ctx, tid)
				// Publish the per-request, GUC-carrying tx onto the neutral
				// runtimecontext bridge so the tenant_scope GORM read adapter
				// routes tenant-scoped reads onto it (RLS sees the GUC). *sql.Tx
				// satisfies runtimecontext.ScopedConn. Without this, reads run on
				// the pool with an empty GUC and the permissive RLS branch leaks
				// rows cross-tenant. (P0 apikeys cross-tenant leak fix.)
				if tx, ok := tenant.TxFromContext(ctx); ok {
					ctx = runtimecontext.WithConn(ctx, tx)
				}
				c.Request = r.WithContext(ctx)
			} else {
				c.Request = r
			}
			c.Next()
		})
		wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Intercept the 403 path: tenant.Middleware writes to w directly
			// and never calls next. We detect that by checking whether next
			// was invoked.
			invoked := false
			detector := http.HandlerFunc(func(rw http.ResponseWriter, rr *http.Request) {
				invoked = true
				next.ServeHTTP(rw, rr)
			})
			mw(detector).ServeHTTP(w, r)
			if !invoked {
				aborted = true
			}
		})
		wrapper.ServeHTTP(c.Writer, c.Request)
		if aborted {
			c.Abort()
		}
	}
}
