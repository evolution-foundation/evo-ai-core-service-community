package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"evo-ai-core-service/pkg/agent_integration/model"
	"evo-ai-core-service/pkg/agent_integration/repository"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// stubRepository stands in for the database so the merge semantics can be
// exercised without Postgres.
type stubRepository struct {
	repository.AgentIntegrationRepository
	stored   *model.AgentIntegration
	upserted model.AgentIntegration
	getErr   error
}

func (s *stubRepository) Upsert(_ context.Context, integration model.AgentIntegration) (*model.AgentIntegration, error) {
	s.upserted = integration
	return &integration, nil
}

func (s *stubRepository) GetByAgentAndProvider(_ context.Context, _ uuid.UUID, _ string) (*model.AgentIntegration, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.stored, nil
}

// stubCredentialLookup answers whether a vault reference is usable, without
// reaching the credentials table.
type stubCredentialLookup struct {
	active map[string]string // id -> kind
	asked  []string
}

func (s *stubCredentialLookup) KindOfActive(_ context.Context, id string) (string, bool) {
	s.asked = append(s.asked, id)
	kind, ok := s.active[id]
	return kind, ok
}

func storedConfig(t *testing.T, fields map[string]interface{}) *model.AgentIntegration {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &model.AgentIntegration{Provider: "dify", Config: datatypes.JSON(raw)}
}

func upsertedConfig(t *testing.T, repo *stubRepository) map[string]interface{} {
	t.Helper()
	var config map[string]interface{}
	if err := json.Unmarshal(repo.upserted.Config, &config); err != nil {
		t.Fatalf("unmarshal upserted config: %v", err)
	}
	return config
}

func newService(repo *stubRepository, lookup *stubCredentialLookup) AgentIntegrationService {
	return NewAgentIntegrationService(repo, lookup)
}

// The regression this story must not introduce: the response stopped carrying
// apiKey, so a screen saving the object it received sends a config WITHOUT it.
// A plain overwrite would erase the stored secret.
func TestUpsertKeepsStoredSecretWhenSaveOmitsIt(t *testing.T) {
	repo := &stubRepository{stored: storedConfig(t, map[string]interface{}{
		"apiUrl": "https://dify.example.com",
		"apiKey": "app-dify-abcdef9c1d",
	})}
	service := newService(repo, &stubCredentialLookup{})

	_, err := service.Upsert(context.Background(), uuid.New(), model.AgentIntegrationRequest{
		Provider: "dify",
		Config:   map[string]interface{}{"apiUrl": "https://dify.example.com", "botType": "chatBot"},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	config := upsertedConfig(t, repo)
	if config["apiKey"] != "app-dify-abcdef9c1d" {
		t.Errorf("stored secret was erased by a save that never carried it: %v", config["apiKey"])
	}
	if config["botType"] != "chatBot" {
		t.Errorf("new field was not written: %v", config["botType"])
	}
}

func TestUpsertAcceptsADeliberateRotation(t *testing.T) {
	repo := &stubRepository{stored: storedConfig(t, map[string]interface{}{"apiKey": "app-dify-velha"})}
	service := newService(repo, &stubCredentialLookup{})

	_, err := service.Upsert(context.Background(), uuid.New(), model.AgentIntegrationRequest{
		Provider: "dify",
		Config:   map[string]interface{}{"apiKey": "app-dify-nova"},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if got := upsertedConfig(t, repo)["apiKey"]; got != "app-dify-nova" {
		t.Errorf("rotation was ignored: %v", got)
	}
}

func TestUpsertOnFirstSaveHasNothingToPreserve(t *testing.T) {
	repo := &stubRepository{stored: nil}
	service := newService(repo, &stubCredentialLookup{})

	_, err := service.Upsert(context.Background(), uuid.New(), model.AgentIntegrationRequest{
		Provider: "dify",
		Config:   map[string]interface{}{"apiKey": "app-dify-nova"},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if got := upsertedConfig(t, repo)["apiKey"]; got != "app-dify-nova" {
		t.Errorf("first save lost the secret: %v", got)
	}
}

func TestUpsertAcceptsAStaticVaultReference(t *testing.T) {
	id := uuid.New().String()
	repo := &stubRepository{}
	lookup := &stubCredentialLookup{active: map[string]string{id: "static"}}
	service := newService(repo, lookup)

	_, err := service.Upsert(context.Background(), uuid.New(), model.AgentIntegrationRequest{
		Provider: "dify",
		Config:   map[string]interface{}{"credential_id": id},
	})
	if err != nil {
		t.Fatalf("upsert with a valid reference: %v", err)
	}

	if got := upsertedConfig(t, repo)["credential_id"]; got != id {
		t.Errorf("credential_id was not persisted: %v", got)
	}
}

// Negative proof: an oauth credential carries no value (the column is NULL by
// database CHECK), so pointing an external agent at one would authenticate
// with nothing. Story 2.5 is what gives oauth rows their meaning.
func TestUpsertRejectsAnOAuthReference(t *testing.T) {
	id := uuid.New().String()
	repo := &stubRepository{}
	lookup := &stubCredentialLookup{active: map[string]string{id: "oauth"}}
	service := newService(repo, lookup)

	_, err := service.Upsert(context.Background(), uuid.New(), model.AgentIntegrationRequest{
		Provider: "github",
		Config:   map[string]interface{}{"credential_id": id},
	})

	if err == nil {
		t.Fatal("an oauth reference was accepted")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "oauth") {
		t.Errorf("error does not mention oauth: %v", err)
	}
	if repo.upserted.Provider != "" {
		t.Error("the repository was reached with an oauth reference")
	}
}

func TestUpsertRejectsAnUnknownOrInactiveReference(t *testing.T) {
	repo := &stubRepository{}
	lookup := &stubCredentialLookup{active: map[string]string{}}
	service := newService(repo, lookup)

	_, err := service.Upsert(context.Background(), uuid.New(), model.AgentIntegrationRequest{
		Provider: "dify",
		Config:   map[string]interface{}{"credential_id": uuid.New().String()},
	})

	if err == nil {
		t.Fatal("an unresolvable reference was accepted")
	}
	if repo.upserted.Provider != "" {
		t.Error("the repository was reached with an unresolvable reference")
	}
}

func TestUpsertRejectsAMalformedReference(t *testing.T) {
	repo := &stubRepository{}
	lookup := &stubCredentialLookup{}
	service := newService(repo, lookup)

	_, err := service.Upsert(context.Background(), uuid.New(), model.AgentIntegrationRequest{
		Provider: "dify",
		Config:   map[string]interface{}{"credential_id": "nao-e-uuid"},
	})

	if err == nil {
		t.Fatal("a malformed credential_id was accepted")
	}
	if len(lookup.asked) != 0 {
		t.Error("a malformed id reached the credentials lookup")
	}
}

// Without a reference nothing changes: the inline path is the fallback that
// keeps every unmigrated installation working until story 2.6 runs.
func TestUpsertWithoutReferenceNeverConsultsTheVault(t *testing.T) {
	repo := &stubRepository{}
	lookup := &stubCredentialLookup{}
	service := newService(repo, lookup)

	_, err := service.Upsert(context.Background(), uuid.New(), model.AgentIntegrationRequest{
		Provider: "dify",
		Config:   map[string]interface{}{"apiKey": "app-dify-inline"},
	})
	if err != nil {
		t.Fatalf("inline upsert: %v", err)
	}

	if len(lookup.asked) != 0 {
		t.Errorf("the vault was consulted for an inline credential: %v", lookup.asked)
	}
	if got := upsertedConfig(t, repo)["apiKey"]; got != "app-dify-inline" {
		t.Errorf("inline value was not persisted: %v", got)
	}
}
