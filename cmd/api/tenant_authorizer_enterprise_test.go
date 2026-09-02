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

// fakeInner records whether the SDK authorizer was reached. Reaching it is
// what opens the transaction and sets app.current_tenant_id, so the call
// count is the in-process proxy for "the GUC was set".
type fakeInner struct{ calls int }

func (f *fakeInner) Authorize(ctx context.Context, _, _ string) (context.Context, tenant.ReleaseFunc, error) {
	f.calls++
	return ctx, func(bool) {}, nil
}

func TestEnforcedAuthorizerDelegatesForMember(t *testing.T) {
	inner := &fakeInner{}
	auth := newEnforcedAuthorizer(fakeChecker{err: nil}, inner)

	if _, _, err := auth.Authorize(context.Background(), "user-1", "tenant-1"); err != nil {
		t.Fatalf("expected a member to be authorized, got %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected the scope-binding authorizer to run once, got %d", inner.calls)
	}
}

// The security claim of this fix: a denial must return before the inner
// authorizer runs, so no transaction binds the tenant.
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
			inner := &fakeInner{}
			auth := newEnforcedAuthorizer(fakeChecker{err: tc.err}, inner)

			_, release, err := auth.Authorize(context.Background(), "user-1", "foreign-tenant")
			if !errors.Is(err, tenant.ErrForbidden) {
				t.Fatalf("expected tenant.ErrForbidden, got %v", err)
			}
			if inner.calls != 0 {
				t.Fatalf("scope must not be bound on denial, inner ran %d time(s)", inner.calls)
			}
			if release == nil {
				t.Fatal("expected a non-nil release func")
			}
			release(false)
		})
	}
}
