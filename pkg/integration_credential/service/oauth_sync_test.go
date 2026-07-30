package service

import (
	"context"
	"testing"
	"time"

	"evo-ai-core-service/pkg/integration_credential/model"
)

// stubConnectionReader stands in for the owner store
// (evo_core_agent_integrations joined with evo_core_agents).
type stubConnectionReader struct {
	connections []model.OAuthConnection
	calls       int
}

func (s *stubConnectionReader) LiveConnections(_ context.Context) ([]model.OAuthConnection, error) {
	s.calls++
	return s.connections, nil
}

// stubReferenceStore records what the sync persists, so the tests can assert on
// the row itself rather than on the response.
type stubReferenceStore struct {
	rows        map[string]model.IntegrationCredential // owner_ref -> row
	upserts     []model.IntegrationCredential
	deactivated []string
}

func newStubReferenceStore() *stubReferenceStore {
	return &stubReferenceStore{rows: map[string]model.IntegrationCredential{}}
}

func (s *stubReferenceStore) ListOAuthRows(_ context.Context) ([]model.IntegrationCredential, error) {
	rows := make([]model.IntegrationCredential, 0, len(s.rows))
	for _, row := range s.rows {
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *stubReferenceStore) UpsertOAuthRow(_ context.Context, row model.IntegrationCredential) error {
	s.upserts = append(s.upserts, row)
	s.rows[*row.OwnerRef] = row
	return nil
}

func (s *stubReferenceStore) DeactivateOAuthRow(_ context.Context, ownerRef string) error {
	s.deactivated = append(s.deactivated, ownerRef)
	row := s.rows[ownerRef]
	row.IsActive = false
	s.rows[ownerRef] = row
	return nil
}

func connection(integrationID, agentID, provider string) model.OAuthConnection {
	return model.OAuthConnection{
		IntegrationID: integrationID,
		AgentID:       agentID,
		AgentName:     "Agente " + provider,
		Provider:      provider,
		ExpiresAt:     "2026-12-31T00:00:00",
	}
}

func TestSyncCreatesOneRowPerLiveConnection(t *testing.T) {
	reader := &stubConnectionReader{connections: []model.OAuthConnection{
		connection("int-1", "agent-1", "github"),
		connection("int-2", "agent-2", "notion"),
	}}
	store := newStubReferenceStore()

	if err := NewOAuthSync(reader, store).Run(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if len(store.upserts) != 2 {
		t.Fatalf("upserted %d rows, want 2", len(store.upserts))
	}
	for _, row := range store.upserts {
		if row.Kind != model.KindOAuth {
			t.Errorf("row kind = %q, want oauth", row.Kind)
		}
	}
}

// Negative proof: the sync must never write a secret. Whatever the owner store
// holds, the persisted row carries no value and no hint, and `value` stays NULL
// by database CHECK.
func TestSyncNeverPersistsAValue(t *testing.T) {
	reader := &stubConnectionReader{connections: []model.OAuthConnection{
		connection("int-1", "agent-1", "github"),
	}}
	store := newStubReferenceStore()

	if err := NewOAuthSync(reader, store).Run(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	row := store.upserts[0]
	if row.Value != "" {
		t.Errorf("sync persisted a value (%q): the vault must never own a token", row.Value)
	}
	if row.ValueHint != "" {
		t.Errorf("sync persisted a hint (%q), which only exists for a secret", row.ValueHint)
	}
	if row.OwnerStore == nil || *row.OwnerStore != model.OwnerStoreAgentIntegration {
		t.Errorf("owner_store = %v, want agent_integration", row.OwnerStore)
	}
	if row.OwnerRef == nil || *row.OwnerRef != "int-1" {
		t.Errorf("owner_ref = %v, want the integration id", row.OwnerRef)
	}
}

// The sync runs on every listing, so it has to be a no-op when nothing changed.
func TestSyncIsIdempotent(t *testing.T) {
	reader := &stubConnectionReader{connections: []model.OAuthConnection{
		connection("int-1", "agent-1", "github"),
	}}
	store := newStubReferenceStore()
	sync := NewOAuthSync(reader, store)

	if err := sync.Run(context.Background()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstUpserts := len(store.upserts)

	if err := sync.Run(context.Background()); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if len(store.rows) != 1 {
		t.Errorf("second run produced %d rows, want 1", len(store.rows))
	}
	if len(store.upserts) != firstUpserts {
		t.Errorf("second run re-wrote an unchanged row (%d upserts, was %d)", len(store.upserts), firstUpserts)
	}
	if len(store.deactivated) != 0 {
		t.Errorf("second run deactivated a live connection: %v", store.deactivated)
	}
}

// A connection that disappeared from the owner store is deactivated, not
// deleted: the row is reversible if the owner comes back, and it never held a
// secret to begin with.
func TestSyncDeactivatesOrphanRows(t *testing.T) {
	reader := &stubConnectionReader{connections: []model.OAuthConnection{
		connection("int-1", "agent-1", "github"),
		connection("int-2", "agent-2", "notion"),
	}}
	store := newStubReferenceStore()
	sync := NewOAuthSync(reader, store)

	if err := sync.Run(context.Background()); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// The user disconnected notion through the existing flow.
	reader.connections = []model.OAuthConnection{connection("int-1", "agent-1", "github")}

	if err := sync.Run(context.Background()); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if len(store.deactivated) != 1 || store.deactivated[0] != "int-2" {
		t.Errorf("deactivated = %v, want [int-2]", store.deactivated)
	}
	if store.rows["int-2"].IsActive {
		t.Error("the orphan row is still active")
	}
	if !store.rows["int-1"].IsActive {
		t.Error("a live connection was deactivated")
	}
}

// A connection that comes back must be revived rather than duplicated: the
// natural key is (owner_store, owner_ref).
func TestSyncRevivesAReturningConnection(t *testing.T) {
	reader := &stubConnectionReader{connections: []model.OAuthConnection{connection("int-1", "agent-1", "github")}}
	store := newStubReferenceStore()
	sync := NewOAuthSync(reader, store)

	_ = sync.Run(context.Background())
	reader.connections = nil
	_ = sync.Run(context.Background())
	reader.connections = []model.OAuthConnection{connection("int-1", "agent-1", "github")}
	_ = sync.Run(context.Background())

	if len(store.rows) != 1 {
		t.Errorf("a returning connection produced %d rows, want 1", len(store.rows))
	}
	if !store.rows["int-1"].IsActive {
		t.Error("the returning connection was not reactivated")
	}
}

func TestSyncSkipsNonConnectionProviders(t *testing.T) {
	reader := &stubConnectionReader{connections: []model.OAuthConnection{
		connection("int-1", "agent-1", "github"),
		// Satellite credential row: holds the token of a connection already
		// listed, so emitting it would double-list the same Google account.
		connection("int-2", "agent-1", "google_calendar_credentials"),
		// Static platform credential: belongs to the static section.
		connection("int-3", "agent-2", "dify"),
		// Channel domain: EvoHub territory, never in this vault (AC8).
		connection("int-4", "agent-3", "whatsapp"),
	}}
	store := newStubReferenceStore()

	if err := NewOAuthSync(reader, store).Run(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if len(store.upserts) != 1 {
		t.Fatalf("upserted %d rows, want only the github connection", len(store.upserts))
	}
	if *store.upserts[0].OwnerRef != "int-1" {
		t.Errorf("upserted %q, want int-1", *store.upserts[0].OwnerRef)
	}
}

// The state shown comes from the owner store at listing time. Nothing about a
// rotation is written to the vault.
func TestDecorateReadsStateFromTheOwnerStoreWithoutWriting(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	reader := &stubConnectionReader{connections: []model.OAuthConnection{
		{IntegrationID: "int-1", AgentID: "agent-1", AgentName: "Agente", Provider: "github", ExpiresAt: "2026-07-29T13:00:00"},
	}}
	store := newStubReferenceStore()
	sync := NewOAuthSync(reader, store)

	if err := sync.Run(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	upsertsAfterSync := len(store.upserts)

	rows, _ := store.ListOAuthRows(context.Background())
	decorated, err := sync.Decorate(context.Background(), rows, now)
	if err != nil {
		t.Fatalf("decorate: %v", err)
	}

	if len(decorated) != 1 {
		t.Fatalf("decorated %d rows, want 1", len(decorated))
	}
	if decorated[0].ConnectionStatus != model.ConnectionStatusExpiring {
		t.Errorf("status = %q, want expiring", decorated[0].ConnectionStatus)
	}
	if decorated[0].AgentID != "agent-1" {
		t.Errorf("agent_id = %q, want agent-1: the disconnect call needs it", decorated[0].AgentID)
	}
	if decorated[0].AgentName != "Agente" {
		t.Errorf("agent_name = %q, want Agente", decorated[0].AgentName)
	}

	// The owner store rotated the token. Reading again reflects it, with no
	// write to the vault.
	reader.connections[0].ExpiresAt = "2026-10-29T13:00:00"
	decorated, _ = sync.Decorate(context.Background(), rows, now)

	if decorated[0].ConnectionStatus != model.ConnectionStatusConnected {
		t.Errorf("status after rotation = %q, want connected", decorated[0].ConnectionStatus)
	}
	if len(store.upserts) != upsertsAfterSync {
		t.Error("decorating wrote to the vault; the mirrored state must never be persisted")
	}
}

func TestDecorateMarksARowWhoseOwnerVanished(t *testing.T) {
	now := time.Now()
	reader := &stubConnectionReader{connections: []model.OAuthConnection{connection("int-1", "agent-1", "github")}}
	store := newStubReferenceStore()
	sync := NewOAuthSync(reader, store)
	_ = sync.Run(context.Background())

	rows, _ := store.ListOAuthRows(context.Background())
	reader.connections = nil

	decorated, err := sync.Decorate(context.Background(), rows, now)
	if err != nil {
		t.Fatalf("decorate: %v", err)
	}

	if decorated[0].ConnectionStatus != model.ConnectionStatusExpired {
		t.Errorf("status = %q, want expired for a vanished owner", decorated[0].ConnectionStatus)
	}
}
