package service

import (
	"context"
	"encoding/json"
	"errors"
	"evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/pkg/agent_integration/model"
	"evo-ai-core-service/pkg/agent_integration/repository"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// credentialKindOAuth mirrors the kind discriminator of the vault table
// (evo_core_integration_credentials), created by story 2.1.
const credentialKindOAuth = "oauth"

type AgentIntegrationService interface {
	Upsert(ctx context.Context, agentID uuid.UUID, request model.AgentIntegrationRequest) (*model.AgentIntegrationResponse, error)
	GetByProvider(ctx context.Context, agentID uuid.UUID, provider string) (*model.AgentIntegrationResponse, error)
	ListByAgent(ctx context.Context, agentID uuid.UUID) ([]*model.AgentIntegrationResponse, error)
	Delete(ctx context.Context, agentID uuid.UUID, provider string) error
}

// CredentialLookup answers whether a vault reference is usable. It is an
// interface so the service never imports the credentials package: the two are
// separate stores, and only the id travels between them.
type CredentialLookup interface {
	// KindOfActive returns the kind of an ACTIVE credential and whether one was
	// found at all. An inactive or missing credential reports false.
	KindOfActive(ctx context.Context, id string) (string, bool)
}

type agentIntegrationService struct {
	repository       repository.AgentIntegrationRepository
	credentialLookup CredentialLookup
}

func NewAgentIntegrationService(repository repository.AgentIntegrationRepository, credentialLookup CredentialLookup) AgentIntegrationService {
	return &agentIntegrationService{
		repository:       repository,
		credentialLookup: credentialLookup,
	}
}

func (s *agentIntegrationService) Upsert(ctx context.Context, agentID uuid.UUID, request model.AgentIntegrationRequest) (*model.AgentIntegrationResponse, error) {
	if err := s.validateCredentialReference(ctx, request.Config); err != nil {
		return nil, err
	}

	config, err := s.mergeWithStoredSecrets(ctx, agentID, request)
	if err != nil {
		return nil, err
	}

	// Convert map to datatypes.JSON
	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}

	integration := model.AgentIntegration{
		AgentID:  agentID,
		Provider: request.Provider,
		Config:   datatypes.JSON(configBytes),
	}

	result, err := s.repository.Upsert(ctx, integration)
	if err != nil {
		return nil, postgres.MapDBError(err, model.AgentIntegrationErrors)
	}

	return result.ToResponse(), nil
}

// validateCredentialReference refuses a vault reference the runtime could not
// honour, so the failure lands on the person configuring the agent instead of
// on the next conversation.
//
// An oauth credential is refused outright: its value column is NULL by database
// CHECK, because the vault points at the store that owns the token rather than
// copying it. Story 2.5 is what gives those rows meaning.
func (s *agentIntegrationService) validateCredentialReference(ctx context.Context, config map[string]interface{}) error {
	credentialID, present := model.CredentialIDFrom(config)
	if !present {
		return nil
	}

	if _, err := uuid.Parse(credentialID); err != nil {
		return fmt.Errorf("credential_id must be a valid uuid: %w", err)
	}

	if s.credentialLookup == nil {
		return errors.New("credential_id was provided but the credential vault is not available")
	}

	kind, found := s.credentialLookup.KindOfActive(ctx, credentialID)
	if !found {
		return errors.New("credential_id does not match an active integration credential")
	}

	if kind == credentialKindOAuth {
		return errors.New("credential_id points to an oauth credential, which holds no value: external agents need a static credential")
	}

	return nil
}

// mergeWithStoredSecrets keeps a secret the caller never sent.
//
// The upsert overwrites `config` wholesale, and since story 2.3 stopped
// returning the platform secrets, the screens round-trip a config without them.
// Without this merge, saving any unrelated field would erase the stored
// credential.
func (s *agentIntegrationService) mergeWithStoredSecrets(ctx context.Context, agentID uuid.UUID, request model.AgentIntegrationRequest) (map[string]interface{}, error) {
	stored, err := s.repository.GetByAgentAndProvider(ctx, agentID, request.Provider)
	if err != nil || stored == nil {
		// No stored row means nothing to preserve. A lookup error is not fatal
		// here: the upsert below reports the real problem.
		return request.Config, nil
	}

	var storedConfig map[string]interface{}
	if len(stored.Config) > 0 {
		if err := json.Unmarshal(stored.Config, &storedConfig); err != nil {
			return request.Config, nil
		}
	}

	return model.MergePreservedSecrets(request.Config, storedConfig), nil
}

func (s *agentIntegrationService) GetByProvider(ctx context.Context, agentID uuid.UUID, provider string) (*model.AgentIntegrationResponse, error) {
	integration, err := s.repository.GetByAgentAndProvider(ctx, agentID, provider)
	if err != nil {
		return nil, postgres.MapDBError(err, model.AgentIntegrationErrors)
	}

	return integration.ToResponse(), nil
}

func (s *agentIntegrationService) ListByAgent(ctx context.Context, agentID uuid.UUID) ([]*model.AgentIntegrationResponse, error) {
	integrations, err := s.repository.ListByAgent(ctx, agentID)
	if err != nil {
		return nil, postgres.MapDBError(err, model.AgentIntegrationErrors)
	}

	responses := make([]*model.AgentIntegrationResponse, len(integrations))
	for i, integration := range integrations {
		responses[i] = integration.ToResponse()
	}

	return responses, nil
}

func (s *agentIntegrationService) Delete(ctx context.Context, agentID uuid.UUID, provider string) error {
	err := s.repository.Delete(ctx, agentID, provider)
	if err != nil {
		return postgres.MapDBError(err, model.AgentIntegrationErrors)
	}

	return nil
}
