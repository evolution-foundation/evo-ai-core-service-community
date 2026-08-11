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

// Check reports whether the current request may create another AI agent.
// Community build: always nil (no enforcement).
func Check(_ context.Context) error { return nil }
