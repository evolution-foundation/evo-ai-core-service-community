package tenantmembership

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeQuerier records whether the lookup ran and returns a canned verdict.
type fakeQuerier struct {
	allowed  bool
	err      error
	calls    int
	gotUser  string
	gotScope string
}

func (f *fakeQuerier) Exists(_ context.Context, _, userID, tenantID string) (bool, error) {
	f.calls++
	f.gotUser = userID
	f.gotScope = tenantID
	return f.allowed, f.err
}

func TestAllowedWhenMembershipRowExists(t *testing.T) {
	q := &fakeQuerier{allowed: true}
	checker := NewSQLChecker(q)

	if err := checker.Allowed(context.Background(), "user-1", "tenant-1"); err != nil {
		t.Fatalf("expected member to be allowed, got %v", err)
	}
	if q.calls != 1 {
		t.Fatalf("expected exactly one lookup, got %d", q.calls)
	}
	if q.gotUser != "user-1" || q.gotScope != "tenant-1" {
		t.Fatalf("lookup received (%s, %s), want (user-1, tenant-1)", q.gotUser, q.gotScope)
	}
}

func TestDeniedWhenNotAMember(t *testing.T) {
	checker := NewSQLChecker(&fakeQuerier{allowed: false})

	err := checker.Allowed(context.Background(), "user-1", "foreign-tenant")
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("expected ErrDenied for a non-member, got %v", err)
	}
}

// evolution_admin, agency_owner and agency_support are branches of the
// single allow-set statement, so they surface here as the row that
// statement returns. The SQL itself is asserted by TestAllowQueryShape.
func TestAllowedForGlobalRoles(t *testing.T) {
	for _, role := range []string{"evolution_admin", "agency_owner", "agency_support"} {
		t.Run(role, func(t *testing.T) {
			checker := NewSQLChecker(&fakeQuerier{allowed: true})

			if err := checker.Allowed(context.Background(), "admin-1", "any-tenant"); err != nil {
				t.Fatalf("expected %s to be allowed, got %v", role, err)
			}
		})
	}
}

func TestDeniedOnDatabaseError(t *testing.T) {
	checker := NewSQLChecker(&fakeQuerier{allowed: true, err: errors.New("connection refused")})

	err := checker.Allowed(context.Background(), "user-1", "tenant-1")
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("expected a database error to deny, got %v", err)
	}
}

// A lookup failure must deny even when the driver also reports allowed:
// no query may run under a tenant the caller cannot be proven to hold.
func TestDatabaseErrorOutranksAllowedVerdict(t *testing.T) {
	checker := NewSQLChecker(&fakeQuerier{allowed: true, err: errors.New("boom")})

	if err := checker.Allowed(context.Background(), "user-1", "tenant-1"); !errors.Is(err, ErrDenied) {
		t.Fatalf("expected ErrDenied, got %v", err)
	}
}

func TestDeniedOnMissingIdentifiers(t *testing.T) {
	cases := []struct {
		name     string
		userID   string
		tenantID string
	}{
		{"no tenant header", "user-1", ""},
		{"no authenticated user", "", "tenant-1"},
		{"neither", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeQuerier{allowed: true}
			checker := NewSQLChecker(q)

			err := checker.Allowed(context.Background(), tc.userID, tc.tenantID)
			if !errors.Is(err, ErrDenied) {
				t.Fatalf("expected ErrDenied, got %v", err)
			}
			if q.calls != 0 {
				t.Fatalf("no lookup should run without both identifiers, got %d", q.calls)
			}
		})
	}
}

func TestDeniedWithoutQuerier(t *testing.T) {
	checker := NewSQLChecker(nil)

	if err := checker.Allowed(context.Background(), "user-1", "tenant-1"); !errors.Is(err, ErrDenied) {
		t.Fatalf("expected ErrDenied with no querier, got %v", err)
	}
}

// The leak was an environment-driven fail-open. The gate must deny a
// non-member regardless of EVO_LICENSING_PERMISSIVE_MEMBERSHIP.
func TestPermissiveEnvDoesNotReopenTheLeak(t *testing.T) {
	for _, value := range []string{"true", "1", "yes", "on"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("EVO_LICENSING_PERMISSIVE_MEMBERSHIP", value)
			checker := NewSQLChecker(&fakeQuerier{allowed: false})

			err := checker.Allowed(context.Background(), "user-1", "foreign-tenant")
			if !errors.Is(err, ErrDenied) {
				t.Fatalf("permissive env %q must not authorize a non-member, got %v", value, err)
			}
		})
	}
}

// The gem admits ANY global membership into its own agency's tenants, not
// just agency_owner (engine.rb tenant_membership_check, mirrored by
// Roles.global_role_applies?). Pinning the role in SQL would 403 a global
// agency_support on the very accounts it exists to operate, so the
// agency-bridge branch must carry no role predicate — and must nil-guard
// both sides of the agency comparison.
func TestAllowQueryShape(t *testing.T) {
	agencyBranch := allowQuery[strings.Index(allowQuery, "OR EXISTS"):]

	if strings.Contains(agencyBranch, "m.role") {
		t.Error("the agency-bridge branch must not pin a role: a global agency_support reaches its own agency's tenants")
	}
	for _, guard := range []string{"u.agency_id IS NOT NULL", "t.agency_id IS NOT NULL"} {
		if !strings.Contains(agencyBranch, guard) {
			t.Errorf("missing nil-guard %q: a null agency_id must never match a null agency_id", guard)
		}
	}
	if !strings.Contains(agencyBranch, "t.agency_id = u.agency_id") {
		t.Error("the agency bridge must compare tenant.agency_id against users.agency_id")
	}
	// evolution_admin stays the only role that cuts across agencies.
	if !strings.Contains(allowQuery, "tenant_id IS NULL AND role = 'evolution_admin'") {
		t.Error("evolution_admin must remain the only unconditional global role")
	}
}
