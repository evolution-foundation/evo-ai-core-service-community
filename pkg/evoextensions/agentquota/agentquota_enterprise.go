//go:build enterprise

package agentquota

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	apierrors "evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/pkg/evoextensions/runtimecontext"
)

// quotaExceededCode matches the licensing gem's code so the CRM frontend's
// existing QUOTA_EXCEEDED handling (localized toast) fires unchanged.
const quotaExceededCode = "QUOTA_EXCEEDED"

// Check enforces the tenant's plan `agents` limit before an AI agent is created.
//
// It is the single seam every create path funnels through (the SPA posts to
// /evoai → this Go service directly, and the CRM Rails proxy also lands here),
// so a Rails before_action can never cover it — the enforcement must live here.
//
// The reads run on the request-bound, GUC-carrying connection so the COUNT
// respects the fail-closed RLS on evo_core_agents. Semantics mirror the
// licensing gem's QuotaCheckService(:agents): an absent/blank/non-integer limit
// (or no active subscription) means UNLIMITED; 0 blocks everything; the create
// is rejected when the current count already meets the limit (count >= limit).
//
// Infra errors fail OPEN — a transient DB hiccup must not block agent creation;
// the enforcement itself (count vs a resolved integer limit) is deterministic.
func Check(ctx context.Context) error {
	tenantID := runtimecontext.IDFromContext(ctx)
	if tenantID == "" {
		return nil // no tenant bound (standalone/unscoped) — nothing to enforce
	}

	conn, ok := runtimecontext.ConnFromContext(ctx)
	if !ok {
		return nil // no scope-bound connection — cannot read the tenant's rows
	}

	limit, enforce := tenantAgentLimit(ctx, conn, tenantID)
	if !enforce {
		return nil // unlimited
	}

	var count int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM evo_core_agents WHERE tenant_id = $1`, tenantID,
	).Scan(&count); err != nil {
		return nil // fail open on count error
	}

	return evaluate(count, limit)
}

// tenantAgentLimit resolves the tenant's plan `agents` limit. enforce is false
// when the limit is UNLIMITED: no active subscription, the `agents` key is
// absent/null/blank, or the value is not an integer — mirroring
// QuotaCheckService#raw_limit + #coerce_limit. Only a non-"canceled"
// subscription imposes a limit; the explicit tenant_id filter is mandatory
// because the subscriptions RLS policy is permissive when the GUC is empty.
func tenantAgentLimit(ctx context.Context, conn runtimecontext.ScopedConn, tenantID string) (limit int, enforce bool) {
	var raw sql.NullString
	err := conn.QueryRowContext(ctx, `
		SELECT p.limits ->> 'agents'
		FROM evo_enterprise_tenant_subscriptions s
		JOIN evo_enterprise_tenant_plans p ON p.id = s.tenant_plan_id
		WHERE s.tenant_id = $1
		  AND s.status <> 'canceled'
		LIMIT 1`, tenantID).Scan(&raw)
	if err != nil || !raw.Valid || raw.String == "" {
		return 0, false // no row / null / blank → unlimited
	}

	value, convErr := strconv.Atoi(raw.String)
	if convErr != nil {
		return 0, false // malformed → unlimited
	}
	return value, true
}

// evaluate is the pure quota decision, kept separate from the DB reads so the
// reject/allow boundary is unit-testable without a database. Reject when the
// tenant already meets the limit (count >= limit); limit 0 blocks everything.
func evaluate(count, limit int) error {
	if count < limit {
		return nil
	}
	return apierrors.New(
		quotaExceededCode,
		fmt.Sprintf("agents limit reached (%d/%d)", count, limit),
		http.StatusUnprocessableEntity,
	)
}
