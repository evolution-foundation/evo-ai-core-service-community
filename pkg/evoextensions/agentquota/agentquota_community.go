//go:build !enterprise

// Package agentquota gates agent creation on the tenant's plan limit. It is a
// private sub-package, not one of the three EXTENSION_POINTS.md contracts; the
// community/enterprise pair is swapped by build tag, as tenantfield already is.
//
// Community is single-tenant with no plan limits, so Check is a no-op.
package agentquota

import "context"

// Check reports whether the request may create `additional` AI agents.
//
// `additional` is in the signature because POST /agents/import creates N agents
// in one request, and a seam that only answers for one cannot gate it. The
// licensing gem takes the same parameter, for the same reason.
func Check(_ context.Context, _ int) error { return nil }
