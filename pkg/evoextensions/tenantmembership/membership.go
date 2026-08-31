// Package tenantmembership is the fail-closed gate that decides whether a
// caller may bind app.current_tenant_id for the tenant named in the
// X-Evo-Tenant-Id header.
//
// It is deliberately free of build tags and of any dependency on the
// enterprise licensing SDK, so the check and its tests compile and run in
// the default community build. The enterprise wiring adapts it onto the
// SDK Authorizer interface.
package tenantmembership

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
)

const (
	MembershipTable = "evo_enterprise_tenant_memberships"
	TenantsTable    = "evo_enterprise_tenants"
	UsersTable      = "users"
)

// ErrDenied is the single denial sentinel. Both "not a member" and any
// infrastructure failure during the lookup collapse onto it, so a caller
// mapping it to HTTP 403 can never fall through to a bound scope.
var ErrDenied = errors.New("tenantmembership: denied")

// allowQuery mirrors the gem allow-set (engine.rb tenant_membership_check
// and Roles.global_role_applies?). A caller is allowed when:
//
//	(a) a per-tenant membership row exists for (user, tenant);
//	(b) the user holds a global evolution_admin membership, which cuts
//	    across tenants unconditionally;
//	(c) the user holds ANY other global membership and the requested tenant
//	    belongs to that user's agency. Global rows other than
//	    evolution_admin are agency-team projections, so the role is not
//	    matched here: agency_owner and agency_support alike hold one global
//	    row and reach every tenant of their own agency, and only it.
//
// Rule (c) nil-guards both sides of the agency bridge, so a missing
// agency_id never matches a missing agency_id.
//
// The table carries no status column: the presence of the row is the
// membership. Anything outside the allow-set yields zero rows and denies.
const allowQuery = `
SELECT 1
WHERE
  EXISTS (
    SELECT 1 FROM ` + MembershipTable + `
    WHERE user_id = $1
      AND (tenant_id = $2 OR (tenant_id IS NULL AND role = 'evolution_admin'))
  )
  OR EXISTS (
    SELECT 1
    FROM ` + MembershipTable + ` m
    JOIN ` + UsersTable + ` u ON u.id = m.user_id
    JOIN ` + TenantsTable + ` t ON t.id = $2
    WHERE m.user_id = $1
      AND m.tenant_id IS NULL
      AND u.agency_id IS NOT NULL
      AND t.agency_id IS NOT NULL
      AND t.agency_id = u.agency_id
  )
LIMIT 1`

// Querier is the minimal surface of the membership lookup, kept narrow so
// the gate can be exercised without a live database.
type Querier interface {
	// Exists reports whether the allow-set query matched a row.
	Exists(ctx context.Context, query, userID, tenantID string) (bool, error)
}

// SQLQuerier adapts a *sql.DB onto Querier.
type SQLQuerier struct {
	db *sql.DB
}

func NewSQLQuerier(db *sql.DB) *SQLQuerier {
	return &SQLQuerier{db: db}
}

func (q *SQLQuerier) Exists(ctx context.Context, query, userID, tenantID string) (bool, error) {
	if q == nil || q.db == nil {
		return false, errors.New("tenantmembership: nil database handle")
	}
	var one int
	err := q.db.QueryRowContext(ctx, query, userID, tenantID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Checker answers whether a user may scope requests to a tenant.
type Checker interface {
	Allowed(ctx context.Context, userID, tenantID string) error
}

// SQLChecker resolves membership against the gem-owned tables.
//
// The lookup runs on the unbound pool on purpose: the membership tables
// are not RLS-scoped and must be readable before deciding whether to bind
// a tenant at all.
type SQLChecker struct {
	db Querier
}

func NewSQLChecker(db Querier) *SQLChecker {
	return &SQLChecker{db: db}
}

// Allowed returns nil only when the allow-set matched. Missing identifiers,
// no matching row, and query failures all return ErrDenied; the caller must
// not bind a tenant scope in any of those cases.
func (c *SQLChecker) Allowed(ctx context.Context, userID, tenantID string) error {
	if userID == "" || tenantID == "" {
		return ErrDenied
	}
	if c.db == nil {
		log.Printf("tenantmembership: no database handle configured, denying user=%s tenant=%s", userID, tenantID)
		return ErrDenied
	}

	allowed, err := c.db.Exists(ctx, allowQuery, userID, tenantID)
	if err != nil {
		log.Printf("tenantmembership: lookup failed for user=%s tenant=%s, denying: %v", userID, tenantID, err)
		return fmt.Errorf("%w: membership lookup failed: %v", ErrDenied, err)
	}
	if !allowed {
		return fmt.Errorf("%w: user=%s is not a member of tenant=%s", ErrDenied, userID, tenantID)
	}
	return nil
}
