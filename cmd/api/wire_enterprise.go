//go:build enterprise

package main

import (
	"log"
	"net/http"

	"evo-ai-core-service/pkg/evoextensions/runtimecontext"

	"github.com/evolution-foundation/evo-enterprise-licensing-go/tenant"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// enterpriseScope is the package-level handle to the enterprise
// runtimecontext.Scope. Future GO-* stories (carimbo de tenant em
// INSERT, propagação no client) read CurrentID from this instance.
var enterpriseScope runtimecontext.Scope

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

	scope := tenant.NewEnterpriseScope(tenant.NewSQLAuthorizer(sqlDB))
	enterpriseScope = scope

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
