//go:build enterprise

package agentquota

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	apierrors "evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/pkg/evoextensions/runtimecontext"
)

// quotaExceededCode mirrors the licensing gem's code for the same refusal.
const quotaExceededCode = "QUOTA_EXCEEDED"

// Check enforces the tenant's plan `agents` limit before `additional` agents are
// created. It cannot live in Rails: the SPA posts to this service directly, and
// the CRM Rails proxy lands here too.
//
// The reads run on the request-bound, GUC-carrying connection so the COUNT
// respects the RLS on evo_core_agents. Semantics mirror the gem's
// QuotaCheckService(:agents): an absent/blank/non-integer limit, or no active
// subscription, means UNLIMITED; 0 blocks everything.
//
// Infra errors fail OPEN — a transient DB hiccup must not block agent creation.
// That is the opposite of tenantscope, which fails CLOSED on the same missing
// connection, so every non-enforcing exit logs (see logSkip).
//
// KNOWN LIMITATION (TOCTOU, accepted): the count is read here and the INSERT
// happens afterwards, with no shared lock. Two concurrent creates at
// count == limit-1 both pass, so a tenant can end up one over its plan. This is
// parity with the gem, which has the same race; closing it on the Go side alone
// would make this path stricter than the Ruby one for no gain.
func Check(ctx context.Context, additional int) error {
	tenantID := runtimecontext.IDFromContext(ctx)
	if tenantID == "" {
		// Not an anomaly: community/standalone requests carry no tenant.
		return nil
	}

	conn, ok := runtimecontext.ConnFromContext(ctx)
	if !ok {
		logSkip(tenantID, "no scope-bound connection — cannot read the tenant's rows")
		return nil
	}

	limit, enforce := tenantAgentLimit(ctx, conn, tenantID)
	if !enforce {
		return nil // unlimited — tenantAgentLimit already logged anything abnormal
	}

	var count int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM evo_core_agents WHERE tenant_id = $1`, tenantID,
	).Scan(&count); err != nil {
		logSkip(tenantID, fmt.Sprintf("counting agents failed, allowing the create: %v", err))
		return nil
	}

	return evaluate(count, additional, limit)
}

// logSkip records that the quota did NOT enforce, and why. A limit that is
// configured, believed and silently absent is the bug this card was opened for.
// The prefix matches wire_enterprise.go so the enterprise wiring stays greppable
// under one term.
func logSkip(tenantID, reason string) {
	log.Printf("enterprise agent quota: NOT enforced for tenant %s — %s", tenantID, reason)
}

// tenantAgentLimit resolves the tenant's plan `agents` limit. enforce is false
// when the limit is UNLIMITED: no active subscription, the `agents` key is
// absent/null/blank, or the value is not an integer — mirroring
// QuotaCheckService#raw_limit + #coerce_limit. The explicit tenant_id filter is
// mandatory: the subscriptions RLS policy is permissive when the GUC is empty.
func tenantAgentLimit(ctx context.Context, conn runtimecontext.ScopedConn, tenantID string) (limit int, enforce bool) {
	var raw sql.NullString
	err := conn.QueryRowContext(ctx, `
		SELECT p.limits ->> 'agents'
		FROM evo_enterprise_tenant_subscriptions s
		JOIN evo_enterprise_tenant_plans p ON p.id = s.tenant_plan_id
		WHERE s.tenant_id = $1
		  AND s.status <> 'canceled'
		LIMIT 1`, tenantID).Scan(&raw)
	switch {
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		// Split from ErrNoRows because the two mean opposite things: no
		// subscription is normal, a failed read means the quota is off silently.
		logSkip(tenantID, fmt.Sprintf("reading the plan limit failed, treating as unlimited: %v", err))
		return 0, false
	case err != nil, !raw.Valid, raw.String == "":
		// No active subscription / no `agents` key / blank → unlimited by design.
		return 0, false
	}

	value, convErr := strconv.Atoi(raw.String)
	if convErr != nil {
		// Configured but unusable; the gem warns here too.
		logSkip(tenantID, fmt.Sprintf("plan limit %q is not an integer, treating as unlimited", raw.String))
		return 0, false
	}
	return value, true
}

// evaluate is the pure quota decision, split from the DB reads so the
// reject/allow boundary is unit-testable without a database. It rejects when the
// request would take the tenant past the limit; limit 0 blocks everything.
//
// A non-positive `additional` counts as 1, so a caller that cannot say how many
// it creates gets no free pass. An empty import never reaches here — ImportAgents
// skips the gate when the payload is empty.
func evaluate(count, additional, limit int) error {
	if additional < 1 {
		additional = 1
	}
	if count+additional <= limit {
		return nil
	}
	return apierrors.New(
		quotaExceededCode,
		fmt.Sprintf("agents limit reached (%d/%d, requested %d)", count, limit, additional),
		http.StatusUnprocessableEntity,
	)
}
