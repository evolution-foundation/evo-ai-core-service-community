//go:build enterprise

package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sync"

	"evo-ai-core-service/pkg/evoextensions/tenantmembership"

	"github.com/evolution-foundation/evo-enterprise-licensing-go/tenant"
)

type tenantTxKey struct{}

// txFromContext returns the GUC-carrying transaction bound by
// enforcedAuthorizer. The SDK's own accessor cannot see it: the SDK binds
// its tx under an unexported key, so a tx opened here is invisible to
// tenant.TxFromContext and must travel under a key of our own.
func txFromContext(ctx context.Context) (*sql.Tx, bool) {
	if ctx == nil {
		return nil, false
	}
	tx, ok := ctx.Value(tenantTxKey{}).(*sql.Tx)
	return tx, ok && tx != nil
}

// binder opens the per-request transaction carrying app.current_tenant_id.
// Separated from the membership decision so tests can observe whether a
// bind was attempted without reaching Postgres.
type binder interface {
	bind(ctx context.Context, tenantID string) (context.Context, tenant.ReleaseFunc, error)
}

// enforcedAuthorizer is the authoritative membership decision for the core
// service.
//
// It deliberately does NOT delegate to tenant.SQLAuthorizer. That authorizer
// re-decides membership with its own allow-set, which still pins
// role = 'agency_owner' on the agency bridge, so an agency-team global such
// as agency_support was refused there even after this gate allowed it. The
// divergence stayed invisible while EVO_LICENSING_PERMISSIVE_MEMBERSHIP was
// set, because the SDK then bound the tenant regardless of its own verdict.
//
// The gate decides; the binder only opens the transaction and sets the GUC.
type enforcedAuthorizer struct {
	checker tenantmembership.Checker
	binder  binder
}

func newEnforcedAuthorizer(checker tenantmembership.Checker, b binder) *enforcedAuthorizer {
	return &enforcedAuthorizer{checker: checker, binder: b}
}

func (a *enforcedAuthorizer) Authorize(ctx context.Context, userID, tenantID string) (context.Context, tenant.ReleaseFunc, error) {
	if err := a.checker.Allowed(ctx, userID, tenantID); err != nil {
		return ctx, noopRelease, tenant.ErrForbidden
	}
	return a.binder.bind(ctx, tenantID)
}

// sqlBinder mirrors the SDK bind semantics (authorizer.go BeginTx +
// set_config with is_local=true): one transaction per request, the GUC
// scoped to it, cleared automatically on commit or rollback.
type sqlBinder struct {
	db *sql.DB
}

func newSQLBinder(db *sql.DB) *sqlBinder {
	return &sqlBinder{db: db}
}

func (b *sqlBinder) bind(ctx context.Context, tenantID string) (context.Context, tenant.ReleaseFunc, error) {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return ctx, noopRelease, err
	}

	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.current_tenant_id', $1, true)`, tenantID); err != nil {
		_ = tx.Rollback()
		return ctx, noopRelease, err
	}

	bound := context.WithValue(tenant.WithTenantID(ctx, tenantID), tenantTxKey{}, tx)

	var once sync.Once
	release := func(success bool) {
		once.Do(func() {
			if success {
				if err := tx.Commit(); err != nil {
					log.Printf("tenant: commit failed for tenant=%s: %v", tenantID, err)
					if rollErr := tx.Rollback(); rollErr != nil && !errors.Is(rollErr, sql.ErrTxDone) {
						log.Printf("tenant: rollback after failed commit also failed for tenant=%s: %v", tenantID, rollErr)
					}
				}
				return
			}
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				log.Printf("tenant: rollback failed for tenant=%s: %v", tenantID, err)
			}
		})
	}
	return bound, release, nil
}

func noopRelease(bool) {}
