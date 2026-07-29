package model

import (
	"testing"
	"time"
)

func TestDeriveConnectionStatus(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		expiresAt string
		want      string
	}{
		{"no expiry known", "", ConnectionStatusConnected},
		{"far in the future", "2026-12-31T00:00:00", ConnectionStatusConnected},
		{"inside the warning window", "2026-07-29T18:00:00", ConnectionStatusExpiring},
		{"exactly now", "2026-07-29T12:00:00", ConnectionStatusExpired},
		{"already past", "2026-07-01T00:00:00", ConnectionStatusExpired},
		{"unparseable is treated as unknown", "amanha", ConnectionStatusConnected},
		{"RFC3339 with zone", "2026-12-31T00:00:00Z", ConnectionStatusConnected},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveConnectionStatus(tc.expiresAt, now); got != tc.want {
				t.Errorf("DeriveConnectionStatus(%q) = %q, want %q", tc.expiresAt, got, tc.want)
			}
		})
	}
}

// The status is DERIVED at display time from the owner store, never stored.
// A copy would go stale on the first rotation and show "connected" for a dead
// integration, which is the failure this whole story exists to avoid.
func TestConnectionStatusFollowsTheOwnerStore(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	before := DeriveConnectionStatus("2026-07-29T13:00:00", now)
	if before != ConnectionStatusExpiring {
		t.Fatalf("status before rotation = %q, want expiring", before)
	}

	// The owner store refreshed the token and wrote a new expiry. Nothing was
	// written to the vault.
	after := DeriveConnectionStatus("2026-10-29T13:00:00", now)
	if after != ConnectionStatusConnected {
		t.Errorf("status after rotation = %q, want connected", after)
	}
}

func TestIsConnectionProvider(t *testing.T) {
	connections := []string{"github", "notion", "stripe", "linear", "monday", "atlassian", "asana", "hubspot", "paypal", "canva", "supabase", "google_calendar", "google_sheets"}
	for _, provider := range connections {
		if !IsConnectionProvider(provider) {
			t.Errorf("IsConnectionProvider(%q) = false, want true", provider)
		}
	}

	// Satellite credential rows are not connections: they hold the token of a
	// connection that is already listed, so emitting them would double-list the
	// same Google account.
	for _, provider := range []string{"google_calendar_credentials", "google_sheets_credentials"} {
		if IsConnectionProvider(provider) {
			t.Errorf("IsConnectionProvider(%q) = true, want false (satellite row)", provider)
		}
	}

	// Static platform credentials belong to the static section, not here.
	for _, provider := range []string{"dify", "flowise", "n8n", "typebot", "openai", "elevenlabs", "knowledge_nexus"} {
		if IsConnectionProvider(provider) {
			t.Errorf("IsConnectionProvider(%q) = true, want false (static provider)", provider)
		}
	}

	// Channel credentials belong to the EvoHub domain and must never surface
	// in this vault, not even as a reference (AC8).
	for _, provider := range []string{"whatsapp", "instagram", "facebook", "gmail", "outlook", "microsoft"} {
		if IsConnectionProvider(provider) {
			t.Errorf("IsConnectionProvider(%q) = true, want false (channel domain)", provider)
		}
	}
}

// Negative proof: whatever the owner store holds, the row the vault persists
// carries no secret at all. `value` stays NULL by database CHECK, and the sync
// must never try to fill it.
func TestOAuthRowFromConnectionNeverCarriesASecret(t *testing.T) {
	connection := OAuthConnection{
		IntegrationID: "11111111-1111-1111-1111-111111111111",
		AgentID:       "22222222-2222-2222-2222-222222222222",
		AgentName:     "Agente do GitHub",
		Provider:      "github",
		ExpiresAt:     "2026-12-31T00:00:00",
	}

	row := connection.ToVaultRow()

	if row.Value != "" {
		t.Errorf("the vault row carries a value (%q); oauth rows point, they never hold", row.Value)
	}
	if row.ValueHint != "" {
		t.Errorf("the vault row carries a hint (%q), which is derived from a secret", row.ValueHint)
	}
	if row.Kind != KindOAuth {
		t.Errorf("kind = %q, want oauth", row.Kind)
	}
	if row.OwnerStore == nil || *row.OwnerStore != OwnerStoreAgentIntegration {
		t.Errorf("owner_store = %v, want %q", row.OwnerStore, OwnerStoreAgentIntegration)
	}
	if row.OwnerRef == nil || *row.OwnerRef != connection.IntegrationID {
		t.Errorf("owner_ref = %v, want the integration row id", row.OwnerRef)
	}
	if row.Scope != ScopeAccount {
		t.Errorf("scope = %q, want account", row.Scope)
	}
}

func TestOAuthRowNameIsStableAcrossSyncs(t *testing.T) {
	connection := OAuthConnection{
		IntegrationID: "11111111-1111-1111-1111-111111111111",
		AgentID:       "22222222-2222-2222-2222-222222222222",
		AgentName:     "Agente do GitHub",
		Provider:      "github",
	}

	first := connection.ToVaultRow().Name
	second := connection.ToVaultRow().Name

	if first != second {
		t.Errorf("name is not deterministic: %q then %q", first, second)
	}
	if first == "" {
		t.Error("name is empty; the column is NOT NULL")
	}
}

// Without the agent name the row still has to be usable: the screen falls back
// to owner_ref for the label, but the disconnect button needs agent_id.
func TestOAuthRowKeepsAgentIdentityForTheDisconnectPath(t *testing.T) {
	connection := OAuthConnection{
		IntegrationID: "11111111-1111-1111-1111-111111111111",
		AgentID:       "22222222-2222-2222-2222-222222222222",
		Provider:      "github",
	}

	response := connection.ToResponse(connection.ToVaultRow(), time.Now())

	if response.AgentID != connection.AgentID {
		t.Errorf("agent_id = %q, want %q: the disconnect call needs it", response.AgentID, connection.AgentID)
	}
	if response.ConnectionStatus == "" {
		t.Error("connection_status is empty")
	}
}
