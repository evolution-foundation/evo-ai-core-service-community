//go:build !enterprise

// Package agentquota is the agent-creation quota extension point.
//
// The community release is single-tenant with no plan limits, so Check is a
// no-op. The enterprise build (agentquota_enterprise.go) enforces the tenant's
// plan `agents` limit before an AI agent is created. This mirrors the neutral
// "community no-op / enterprise fills" idiom already used by runtimecontext,
// plugin and the installRuntimeScope wiring.
package agentquota

import "context"

// Check reports whether the current request may create `additional` AI agents.
//
// `additional` is part of the signature rather than assumed to be 1 because the
// bulk path (POST /agents/import) creates N agents in one request, and a seam
// that can only answer for one at a time cannot gate it. The licensing gem
// settled this already — QuotaCheckService#call takes `additional:` for exactly
// this reason, citing contacts#import.
//
// Community build: always nil (no enforcement).
func Check(_ context.Context, _ int) error { return nil }
