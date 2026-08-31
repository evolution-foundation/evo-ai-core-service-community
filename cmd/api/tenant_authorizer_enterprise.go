//go:build enterprise

package main

import (
	"context"

	"evo-ai-core-service/pkg/evoextensions/tenantmembership"

	"github.com/evolution-foundation/evo-enterprise-licensing-go/tenant"
)

// enforcedAuthorizer gates the SDK authorizer behind an unconditional
// membership check.
//
// The SDK authorizer honours EVO_LICENSING_PERMISSIVE_MEMBERSHIP, which is
// set true across the ecosystem: with it on, a membership miss still bound
// app.current_tenant_id to whatever X-Evo-Tenant-Id asked for. This wrapper
// resolves membership first and only delegates on success, so a denial
// returns before any transaction is opened and the GUC is never set.
type enforcedAuthorizer struct {
	checker tenantmembership.Checker
	inner   tenant.Authorizer
}

func newEnforcedAuthorizer(checker tenantmembership.Checker, inner tenant.Authorizer) *enforcedAuthorizer {
	return &enforcedAuthorizer{checker: checker, inner: inner}
}

func (a *enforcedAuthorizer) Authorize(ctx context.Context, userID, tenantID string) (context.Context, tenant.ReleaseFunc, error) {
	if err := a.checker.Allowed(ctx, userID, tenantID); err != nil {
		return ctx, func(bool) {}, tenant.ErrForbidden
	}
	return a.inner.Authorize(ctx, userID, tenantID)
}
