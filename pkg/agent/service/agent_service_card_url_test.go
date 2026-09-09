package service

// card_url is required for a2a agents on create, update and import; the update rule
// is checked against the merged state because the repository does partial updates.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	apierrors "evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/repository"
	"evo-ai-core-service/pkg/agent/service/processor"
	mcpmodel "evo-ai-core-service/pkg/mcp_server/model"

	"github.com/google/uuid"
)

// Records every write.
type cardURLFakeRepo struct {
	repository.AgentRepository
	stored       *model.Agent
	createCalled bool
	updateCalled bool
}

func (f *cardURLFakeRepo) Create(_ context.Context, agent model.Agent) (*model.Agent, error) {
	f.createCalled = true
	created := agent
	if created.ID == uuid.Nil {
		created.ID = uuid.New()
	}
	f.stored = &created
	return &created, nil
}

func (f *cardURLFakeRepo) GetByID(_ context.Context, _ uuid.UUID) (*model.Agent, error) {
	if f.stored == nil {
		return nil, errors.New("agent not found")
	}
	snapshot := *f.stored
	return &snapshot, nil
}

// Mirrors GORM Updates(struct): zero values keep the stored field.
func (f *cardURLFakeRepo) Update(_ context.Context, agent *model.Agent, _ uuid.UUID) (*model.Agent, error) {
	f.updateCalled = true

	merged := *f.stored
	if agent.Name != "" {
		merged.Name = agent.Name
	}
	if agent.Type != "" {
		merged.Type = agent.Type
	}
	if agent.Model != "" {
		merged.Model = agent.Model
	}
	if agent.CardURL != "" {
		merged.CardURL = agent.CardURL
	}

	f.stored = &merged
	return &merged, nil
}

type cardURLFakeEvolution struct {
	EvolutionService
}

func (cardURLFakeEvolution) CreateAgentBot(_ context.Context, _ *model.Agent, _ string) (*model.AgentBot, error) {
	return nil, errors.New("no evolution backend in tests")
}

func (cardURLFakeEvolution) UpdateAgentBot(_ context.Context, _ *model.Agent, _ string) (*model.AgentBot, error) {
	return nil, errors.New("no evolution backend in tests")
}

// Replaces the network fetch.
type cardURLFakeA2AProcessor struct {
	created int
}

func (p *cardURLFakeA2AProcessor) Create(_ context.Context, agent *model.Agent) error {
	// Same refusal as the real processor.
	if agent.CardURL == "" {
		return errors.New("card_url is required for a2a type agents")
	}
	p.created++
	return nil
}

func (p *cardURLFakeA2AProcessor) Update(_ context.Context, _, _ *model.Agent) error {
	return nil
}

func serviceForCardURLValidation(repo *cardURLFakeRepo) *agentService {
	return &agentService{
		agentRepository:  repo,
		evolutionService: cardURLFakeEvolution{},
		a2aProcessor:     &cardURLFakeA2AProcessor{},
		configProcessor: processor.NewConfigProcessor(
			func() string { return "generated-key" },
			func(_ context.Context, _ uuid.UUID) (*mcpmodel.McpServer, error) {
				return nil, nil
			},
		),
	}
}

// Asserts a 400 VALIDATION_ERROR naming the field.
func assertCardURLRejection(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("an a2a agent with an empty card_url was accepted")
	}

	var apiErr *apierrors.ApiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("rejection is a plain error (%v) — the handler maps it to 500 "+
			"INTERNAL_ERROR and the user is told nothing actionable", err)
	}

	code, message, status := apierrors.HandleError(err)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", status, http.StatusBadRequest)
	}
	if code != apierrors.ValidationError {
		t.Errorf("code = %q, want %q", code, apierrors.ValidationError)
	}
	if !strings.Contains(message, "card_url is required for a2a type agents") {
		t.Errorf("message = %q, want the wording the processor uses", message)
	}
}

func TestCreate_A2AWithoutCardURLIsRejectedAndPersistsNothing(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForCardURLValidation(repo)

	_, err := svc.Create(context.Background(), model.Agent{
		Name: "agente_a2a",
		Type: model.AgentTypeA2A,
	})

	assertCardURLRejection(t, err)

	if repo.createCalled {
		t.Error("the repository was asked to write an agent that cannot run")
	}
	if repo.stored != nil {
		t.Errorf("an agent was persisted anyway: %+v", repo.stored)
	}
}

func TestCreate_A2AWithBlankCardURLIsRejected(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForCardURLValidation(repo)

	// Whitespace is not a url.
	_, err := svc.Create(context.Background(), model.Agent{
		Name:    "agente_a2a",
		Type:    model.AgentTypeA2A,
		CardURL: "   ",
	})

	assertCardURLRejection(t, err)

	if repo.createCalled {
		t.Error("a whitespace card_url reached the repository")
	}
}

func TestUpdate_CannotSwitchTypeToA2AWhenTheRowHasNoCardURL(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForCardURLValidation(repo)

	existing := &model.Agent{
		ID:     uuid.New(),
		Name:   "era_llm",
		Type:   model.AgentTypeLLM,
		Model:  "openai/gpt-4.1-mini",
		Config: "{}",
	}
	repo.stored = existing

	_, err := svc.Update(context.Background(), &model.Agent{
		Name:   "era_llm",
		Type:   model.AgentTypeA2A,
		Config: "{}",
	}, existing.ID)

	assertCardURLRejection(t, err)

	if repo.updateCalled {
		t.Error("the type switch was written with no card_url")
	}
	if repo.stored.Type != model.AgentTypeLLM {
		t.Errorf("stored type became %q; the rejected update must leave the row alone", repo.stored.Type)
	}
}

func TestUpdate_TypeSwitchToA2AIsAllowedWhenTheRowAlreadyCarriesACard(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForCardURLValidation(repo)

	existing := &model.Agent{
		ID:      uuid.New(),
		Name:    "ja_tem_card",
		Type:    model.AgentTypeLLM,
		CardURL: "https://parceiro.example.com/.well-known/agent.json",
		Config:  "{}",
	}
	repo.stored = existing

	_, err := svc.Update(context.Background(), &model.Agent{
		Name:   "ja_tem_card",
		Type:   model.AgentTypeA2A,
		Config: "{}",
	}, existing.ID)
	if err != nil {
		t.Errorf("the switch was rejected even though the row has a card_url: %v", err)
	}
}

// Partial updates that do not resend card_url must keep working.
func TestUpdate_PartialEditOfAnA2AAgentIsNotRejected(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForCardURLValidation(repo)

	existing := &model.Agent{
		ID:      uuid.New(),
		Name:    "agente_a2a",
		Type:    model.AgentTypeA2A,
		CardURL: "https://parceiro.example.com/.well-known/agent.json",
		Config:  "{}",
	}
	repo.stored = existing

	for _, payload := range []*model.Agent{
		// only the name travels
		{Name: "nome_novo", Config: "{}"},
		// type travels, card_url does not
		{Name: "nome_novo", Type: model.AgentTypeA2A, Config: "{}"},
		// empty card_url means "keep" under partial-update semantics
		{Name: "nome_novo", Type: model.AgentTypeA2A, CardURL: "", Config: "{}"},
	} {
		if _, err := svc.Update(context.Background(), payload, existing.ID); err != nil {
			t.Errorf("partial update %+v was rejected: %v", payload, err)
		}
	}
}

// Whitespace is not a zero value for GORM, so it must be refused before the write.
func TestUpdate_WhitespaceCardURLOnAnA2AAgentIsRejected(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForCardURLValidation(repo)

	existing := &model.Agent{
		ID:      uuid.New(),
		Name:    "agente_a2a",
		Type:    model.AgentTypeA2A,
		CardURL: "https://parceiro.example.com/.well-known/agent.json",
		Config:  "{}",
	}
	repo.stored = existing

	_, err := svc.Update(context.Background(), &model.Agent{
		Name:    "agente_a2a",
		Type:    model.AgentTypeA2A,
		CardURL: "   ",
		Config:  "{}",
	}, existing.ID)

	assertCardURLRejection(t, err)

	if repo.updateCalled {
		t.Error("a whitespace card_url reached the repository")
	}
	if repo.stored.CardURL != existing.CardURL {
		t.Errorf("stored card_url became %q", repo.stored.CardURL)
	}
}

func TestImportAgents_RejectsA2AWithoutCardURLAndImportsNothing(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForCardURLValidation(repo)

	_, err := svc.ImportAgentsFromJSON(context.Background(), model.AgentImportRequest{
		AgentData: []map[string]interface{}{
			{"name": "sem_card_url", "type": "a2a"},
		},
	})

	assertCardURLRejection(t, err)

	if repo.createCalled {
		t.Error("the import wrote an agent that cannot run")
	}
}

// A rejected entry leaves the whole batch unwritten.
func TestImportAgents_BatchWithAnInvalidA2AEntryImportsNothing(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForCardURLValidation(repo)

	_, err := svc.ImportAgentsFromJSON(context.Background(), model.AgentImportRequest{
		AgentData: []map[string]interface{}{
			{"name": "llm_ok", "type": "llm", "model": "openai/gpt-4.1-mini"},
			{"name": "sem_card_url", "type": "a2a"},
		},
	})

	assertCardURLRejection(t, err)

	if repo.createCalled {
		t.Error("an entry before the rejected one was written")
	}
}

// Other types stay creatable without card_url.
func TestValidateCardURL_OtherTypesWithoutCardURLStayAllowed(t *testing.T) {
	for _, agentType := range []string{
		model.AgentTypeLLM,
		model.AgentTypeSequential,
		model.AgentTypeParallel,
		model.AgentTypeLoop,
		model.AgentTypeWorkflow,
		model.AgentTypeTask,
		model.AgentTypeExternal,
	} {
		if err := validateCardURLForType(&model.Agent{Name: "agente", Type: agentType}); err != nil {
			t.Errorf("type %q was rejected for having no card_url: %v", agentType, err)
		}
	}
}

func TestValidateCardURL_A2AWithCardURLPasses(t *testing.T) {
	err := validateCardURLForType(&model.Agent{
		Name:    "agente_a2a",
		Type:    model.AgentTypeA2A,
		CardURL: "https://parceiro.example.com/.well-known/agent.json",
	})
	if err != nil {
		t.Errorf("a2a agent with a card_url was rejected: %v", err)
	}
}

// The /.well-known/agent.json suffix is not enforced; create fetches the card instead.
func TestValidateCardURL_SuffixIsNotEnforcedHere(t *testing.T) {
	err := validateCardURLForType(&model.Agent{
		Name:    "agente_a2a",
		Type:    model.AgentTypeA2A,
		CardURL: "https://parceiro.example.com/cards/agente",
	})
	if err != nil {
		t.Errorf("the suffix must not be enforced by this validation: %v", err)
	}
}
