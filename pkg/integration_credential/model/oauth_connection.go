package model

import (
	"fmt"
	"time"
)

// Connection states, derived at display time from the owner store and never
// stored: a copy goes stale on the first rotation.
// // ⚠️ No "refresh failed" state on purpose. Nothing records a failed renewal —
// it only reaches the log, and at least one provider returns the stale token on
// error — so the badge would be a guess that sends people reconnecting a
// healthy integration.
const (
	ConnectionStatusConnected = "connected"
	ConnectionStatusExpiring  = "expiring"
	ConnectionStatusExpired   = "expired"
)

// expiringWindow is how far ahead an expiry counts as "expiring soon". The
// owner store refreshes lazily, at the moment of use, so this window is a
// display hint and not a deadline.
const expiringWindow = 24 * time.Hour

// OwnerStoreAgentIntegration is the only owner this story knows. The column is
// a string so a future story can point at another store without a migration.
const OwnerStoreAgentIntegration = "agent_integration"

// connectionProviders are the OAuth connections of an AGENT. Channel
// credentials belong to the EvoHub domain and never appear here.
// // An allowlist rather than "anything with an access_token": `provider` is an
// unvalidated VARCHAR, and the table also holds `<provider>_credentials`
// satellite rows that would double-list an account already shown.
var connectionProviders = map[string]bool{
	"github":          true,
	"notion":          true,
	"stripe":          true,
	"linear":          true,
	"monday":          true,
	"atlassian":       true,
	"asana":           true,
	"hubspot":         true,
	"paypal":          true,
	"canva":           true,
	"supabase":        true,
	"google_calendar": true,
	"google_sheets":   true,
}

// IsConnectionProvider reports whether a provider from the agent integration
// table represents a user-facing OAuth connection.
func IsConnectionProvider(provider string) bool {
	return connectionProviders[provider]
}

// DeriveConnectionStatus turns the owner store's expiry into a display state.
// An unknown or unparseable expiry reads as connected: the token is there, and
// the owner refreshes it lazily when it is used.
func DeriveConnectionStatus(expiresAt string, now time.Time) string {
	if expiresAt == "" {
		return ConnectionStatusConnected
	}

	expiry, err := parseOwnerTimestamp(expiresAt)
	if err != nil {
		return ConnectionStatusConnected
	}

	switch {
	case !expiry.After(now):
		return ConnectionStatusExpired
	case expiry.Sub(now) <= expiringWindow:
		return ConnectionStatusExpiring
	default:
		return ConnectionStatusConnected
	}
}

// parseOwnerTimestamp accepts both shapes the owner store writes: a naive UTC
// timestamp (what the token refresh persists) and a zoned RFC3339 one.
func parseOwnerTimestamp(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC(), nil
	}
	return time.Parse("2006-01-02T15:04:05", value)
}

// OAuthConnection is a live connection read from the owner store. It carries
// metadata only: no token ever crosses into this struct.
type OAuthConnection struct {
	IntegrationID string
	AgentID       string
	AgentName     string
	Provider      string
	ExpiresAt     string
}

// ToVaultRow builds the reference row the vault persists. It is the negative
// space that matters: no value, no hint, only where the secret actually lives.
func (c OAuthConnection) ToVaultRow() IntegrationCredential {
	ownerStore := OwnerStoreAgentIntegration
	ownerRef := c.IntegrationID

	return IntegrationCredential{
		Name:        c.vaultRowName(),
		Provider:    c.Provider,
		Kind:        KindOAuth,
		ValueFormat: ValueFormatScalar,
		Scope:       ScopeAccount,
		IsActive:    true,
		OwnerStore:  &ownerStore,
		OwnerRef:    &ownerRef,
	}
}

// vaultRowName is deterministic so re-running the sync never renames a row.
// The unique index is (scope, name), so the integration id keeps two agents
// connected to the same provider from colliding.
func (c OAuthConnection) vaultRowName() string {
	label := c.AgentName
	if label == "" {
		label = c.AgentID
	}
	return fmt.Sprintf("%s - %s (%s)", c.Provider, label, shortID(c.IntegrationID))
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// ToResponse decorates the stored reference row with the state read live from
// the owner store. The mirrored fields are computed on every listing and are
// never persisted.
func (c OAuthConnection) ToResponse(row IntegrationCredential, now time.Time) *IntegrationCredentialResponse {
	response := row.ToResponse()
	response.ConnectionStatus = DeriveConnectionStatus(c.ExpiresAt, now)
	response.ConnectionExpiresAt = c.ExpiresAt
	response.AgentID = c.AgentID
	response.AgentName = c.AgentName
	return response
}
