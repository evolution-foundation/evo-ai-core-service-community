//go:build enterprise

package main

import (
	"context"
	"errors"
	"testing"

	"evo-ai-core-service/pkg/evoextensions/tenantmembership"

	"github.com/evolution-foundation/evo-enterprise-licensing-go/tenant"
)

// fakeChecker stands in for the membership gate.
type fakeChecker struct{ err error }

func (f fakeChecker) Allowed(context.Context, string, string) error { return f.err }

// fakeBinder records whether the scope bind was attempted. Opening the
// transaction and setting app.current_tenant_id is the only thing that
// happens after the gate allows, so the call count is the in-process proxy
// for "the GUC was set".
type fakeBinder struct {
	calls    int
	gotScope string
	err      error
}

func (f *fakeBinder) bind(ctx context.Context, tenantID string) (context.Context, tenant.ReleaseFunc, error) {
	f.calls++
	f.gotScope = tenantID
	if f.err != nil {
		return ctx, noopRelease, f.err
	}
	return tenant.WithTenantID(ctx, tenantID), func(bool) {}, nil
}

func TestEnforcedAuthorizerBindsForMember(t *testing.T) {
	b := &fakeBinder{}
	auth := newEnforcedAuthorizer(fakeChecker{err: nil}, b)

	ctx, release, err := auth.Authorize(context.Background(), "user-1", "tenant-1")
	if err != nil {
		t.Fatalf("expected a member to be authorized, got %v", err)
	}
	if b.calls != 1 {
		t.Fatalf("expected the scope to be bound once, got %d", b.calls)
	}
	if b.gotScope != "tenant-1" {
		t.Fatalf("bound tenant %q, want tenant-1", b.gotScope)
	}
	if got := tenant.TenantIDFromContext(ctx); got != "tenant-1" {
		t.Fatalf("context carries tenant %q, want tenant-1", got)
	}
	release(true)
}

// The M1 profile: a global agency_support over a tenant of its own agency
// is admitted by the gate, and nothing downstream may overturn that. The
// SDK authorizer used to re-decide membership here with an allow-set that
// pins role = 'agency_owner' and denied this exact caller; it is no longer
// in the chain, so a gate allow always reaches the bind.
func TestGateAllowIsNotOverriddenDownstream(t *testing.T) {
	b := &fakeBinder{}
	auth := newEnforcedAuthorizer(fakeChecker{err: nil}, b)

	if _, _, err := auth.Authorize(context.Background(), "agency-support-user", "same-agency-tenant"); err != nil {
		t.Fatalf("a gate allow must not be overturned downstream, got %v", err)
	}
	if b.calls != 1 {
		t.Fatalf("expected the scope to be bound for an allowed agency-team global, got %d calls", b.calls)
	}
}

// The security claim: a denial must return before the bind, so no
// transaction ever carries a tenant the caller cannot access.
func TestEnforcedAuthorizerNeverBindsScopeWhenDenied(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"not a member", tenantmembership.ErrDenied},
		{"lookup failure", errors.New("connection refused")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &fakeBinder{}
			auth := newEnforcedAuthorizer(fakeChecker{err: tc.err}, b)

			ctx, release, err := auth.Authorize(context.Background(), "user-1", "foreign-tenant")
			if !errors.Is(err, tenant.ErrForbidden) {
				t.Fatalf("expected tenant.ErrForbidden, got %v", err)
			}
			if b.calls != 0 {
				t.Fatalf("scope must not be bound on denial, binder ran %d time(s)", b.calls)
			}
			if got := tenant.TenantIDFromContext(ctx); got != "" {
				t.Fatalf("denied request must carry no tenant, got %q", got)
			}
			if release == nil {
				t.Fatal("expected a non-nil release func")
			}
			release(false)
		})
	}
}

// A bind failure must surface rather than yielding an unbound-but-allowed
// request.
func TestEnforcedAuthorizerPropagatesBindFailure(t *testing.T) {
	b := &fakeBinder{err: errors.New("begin tx failed")}
	auth := newEnforcedAuthorizer(fakeChecker{err: nil}, b)

	if _, _, err := auth.Authorize(context.Background(), "user-1", "tenant-1"); err == nil {
		t.Fatal("expected the bind failure to surface")
	}
}
