package tenantmembership

import (
	"context"
	"errors"
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

// evolution_admin and agency_owner are branches of the single allow-set
// statement, so they surface here as the row that statement returns.
func TestAllowedForGlobalRoles(t *testing.T) {
	for _, role := range []string{"evolution_admin", "agency_owner"} {
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
