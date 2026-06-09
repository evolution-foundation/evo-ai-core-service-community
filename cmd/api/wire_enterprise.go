//go:build enterprise

package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/evolution-foundation/evo-enterprise-licensing-go/tenant"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// installRuntimeScope swaps the no-op community Default() scope for
// an EnterpriseScope backed by the gem-owned membership table, then
// registers the tenant.Middleware on the v1 router group *after* the
// EvoAuth middleware so that user_id is already in ctx when the
// membership check runs.
func installRuntimeScope(v1 *gin.RouterGroup, db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("enterprise wiring: cannot reach underlying *sql.DB: %v", err)
	}

	// Fail-fast: the membership table is owned by
	// evo-enterprise-licensing-ruby. If the gem migration hasn't been
	// applied yet, every enterprise request would hit `relation does
	// not exist` and the middleware would surface it as 403. Detecting
	// the missing table at boot makes the failure mode obvious instead
	// of looking like a flood of legitimate auth denials.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := sqlDB.ExecContext(ctx,
		`SELECT 1 FROM `+tenant.MembershipTable+` LIMIT 0`); err != nil {
		log.Fatalf("enterprise wiring: membership table %q unreachable — "+
			"apply the evo-enterprise-licensing-ruby migration before booting enterprise: %v",
			tenant.MembershipTable, err)
	}

	scope := tenant.NewEnterpriseScope(tenant.NewSQLAuthorizer(sqlDB))

	mw := tenant.Middleware(scope, nil) // nil → DefaultUserIDExtractor reads ctx.Value("user_id")
	v1.Use(ginAdapter(mw))
	log.Println("enterprise wiring: tenant middleware installed on /api/v1")
}

// ginAdapter bridges a net/http middleware into the gin chain. It
// runs the wrapped handler in-process so that:
//   - 403 short-circuits abort the gin chain,
//   - the request context carrying the bound tenant id + dedicated
//     pgx conn propagates to downstream gin handlers,
//   - the ReleaseFunc fires when the wrapped handler returns.
func ginAdapter(mw func(http.Handler) http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var aborted bool
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			c.Request = r
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
