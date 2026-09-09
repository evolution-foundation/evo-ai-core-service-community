package service

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

// Records every write so a test can assert a rejected payload reached none.
type modelRequiredFakeRepo struct {
	repository.AgentRepository
	stored       *model.Agent
	createCalled bool
	updateCalled bool
}

func (f *modelRequiredFakeRepo) Create(_ context.Context, agent model.Agent) (*model.Agent, error) {
	f.createCalled = true
	created := agent
	if created.ID == uuid.Nil {
		created.ID = uuid.New()
	}
	f.stored = &created
	return &created, nil
}

func (f *modelRequiredFakeRepo) GetByID(_ context.Context, _ uuid.UUID) (*model.Agent, error) {
	if f.stored == nil {
		return nil, errors.New("agent not found")
	}
	snapshot := *f.stored
	return &snapshot, nil
}

// Merges like GORM Updates(struct): zero values do not overwrite the row.
func (f *modelRequiredFakeRepo) Update(_ context.Context, agent *model.Agent, _ uuid.UUID) (*model.Agent, error) {
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

	f.stored = &merged
	return &merged, nil
}

// Fails the bot round-trip (tolerated by the service) so it adds no writes.
type modelRequiredFakeEvolution struct {
	EvolutionService
}

func (modelRequiredFakeEvolution) CreateAgentBot(_ context.Context, _ *model.Agent, _ string) (*model.AgentBot, error) {
	return nil, errors.New("no evolution backend in tests")
}

func (modelRequiredFakeEvolution) UpdateAgentBot(_ context.Context, _ *model.Agent, _ string) (*model.AgentBot, error) {
	return nil, errors.New("no evolution backend in tests")
}

func serviceForModelValidation(repo *modelRequiredFakeRepo) *agentService {
	return &agentService{
		agentRepository:  repo,
		evolutionService: modelRequiredFakeEvolution{},
		configProcessor: processor.NewConfigProcessor(
			func() string { return "generated-key" },
			func(_ context.Context, _ uuid.UUID) (*mcpmodel.McpServer, error) {
				return nil, nil
			},
		),
	}
}

func assertModelRejection(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("an llm agent with an empty model was accepted")
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
	if !strings.Contains(message, "Model is required for llm type agents") {
		t.Errorf("message = %q, want it to carry the processor's own wording", message)
	}
}

func TestCreate_LLMWithoutModelIsRejectedAndPersistsNothing(t *testing.T) {
	repo := &modelRequiredFakeRepo{}
	svc := serviceForModelValidation(repo)

	_, err := svc.Create(context.Background(), model.Agent{
		Name:   "agente_teste_minimo",
		Type:   model.AgentTypeLLM,
		Config: `{}`,
	})

	assertModelRejection(t, err)

	if repo.createCalled {
		t.Error("the agent was written to the database anyway — an unrunnable agent " +
			"reached production exactly this way")
	}
}

func TestCreate_LLMWithBlankModelIsRejected(t *testing.T) {
	repo := &modelRequiredFakeRepo{}
	svc := serviceForModelValidation(repo)

	_, err := svc.Create(context.Background(), model.Agent{
		Name:   "agente",
		Type:   model.AgentTypeLLM,
		Model:  "   ",
		Config: `{}`,
	})

	assertModelRejection(t, err)

	if repo.createCalled {
		t.Error("a whitespace-only model was persisted")
	}
}

func TestCreate_LLMWithModelIsAccepted(t *testing.T) {
	repo := &modelRequiredFakeRepo{}
	svc := serviceForModelValidation(repo)

	created, err := svc.Create(context.Background(), model.Agent{
		Name:   "agente",
		Type:   model.AgentTypeLLM,
		Model:  "openai/gpt-4.1-mini",
		Config: `{}`,
	})
	if err != nil {
		t.Fatalf("a valid llm agent was rejected: %v", err)
	}

	if !repo.createCalled {
		t.Fatal("the valid agent was not persisted")
	}
	if created.Model != "openai/gpt-4.1-mini" {
		t.Errorf("persisted model = %q, want %q", created.Model, "openai/gpt-4.1-mini")
	}
}

// Flow agents arrive without a model and are repaired by sanitizeAgent.
func TestValidateCreate_FlowTypesWithoutModelStayAllowed(t *testing.T) {
	svc := serviceForModelValidation(&modelRequiredFakeRepo{})

	for _, agentType := range []string{
		model.AgentTypeSequential,
		model.AgentTypeParallel,
		model.AgentTypeLoop,
		model.AgentTypeA2A,
		model.AgentTypeWorkflow,
		model.AgentTypeTask,
		model.AgentTypeExternal,
	} {
		agent := model.Agent{Name: "agente", Type: agentType, Config: `{}`}
		if agentType == model.AgentTypeA2A {
			agent.CardURL = "https://parceiro.example.com/.well-known/agent.json"
		}
		if err := svc.validateCreate(context.Background(), &agent); err != nil {
			t.Errorf("type %q was rejected for having no model: %v", agentType, err)
		}
	}
}

func TestUpdate_CannotSaveAnLLMAgentThatStillHasNoModel(t *testing.T) {
	repo := &modelRequiredFakeRepo{}
	svc := serviceForModelValidation(repo)

	existing := &model.Agent{
		ID:     uuid.New(),
		Name:   "agente_quebrado",
		Type:   model.AgentTypeLLM,
		Model:  "",
		Config: `{"api_key":"stored-key"}`,
	}
	repo.stored = existing
	repo.updateCalled = false

	_, err := svc.Update(context.Background(), &model.Agent{
		Name:   "nome_novo",
		Type:   model.AgentTypeLLM,
		Config: `{}`,
	}, existing.ID)

	assertModelRejection(t, err)

	if repo.updateCalled {
		t.Error("the update wrote a row that still cannot run")
	}
}

// Partial update: a rename that does not resend `model` must keep working.
func TestUpdate_PartialEditOfAnLLMAgentKeepsWorking(t *testing.T) {
	repo := &modelRequiredFakeRepo{}
	svc := serviceForModelValidation(repo)

	existing := &model.Agent{
		ID:     uuid.New(),
		Name:   "agente",
		Type:   model.AgentTypeLLM,
		Model:  "openai/gpt-4.1-mini",
		Config: `{"api_key":"stored-key"}`,
	}
	repo.stored = existing

	if _, err := svc.Update(context.Background(), &model.Agent{
		Name:   "nome_novo",
		Type:   model.AgentTypeLLM,
		Config: `{}`,
	}, existing.ID); err != nil {
		t.Fatalf("a rename that does not resend the model was rejected: %v", err)
	}

	if repo.stored.Model != "openai/gpt-4.1-mini" {
		t.Errorf("stored model became %q; an omitted field must keep its value", repo.stored.Model)
	}
	if repo.stored.Name != "nome_novo" {
		t.Errorf("stored name is %q; the rename did not take effect", repo.stored.Name)
	}
}

func TestUpdate_LLMWithModelStillPasses(t *testing.T) {
	repo := &modelRequiredFakeRepo{}
	svc := serviceForModelValidation(repo)

	existing := &model.Agent{
		ID:     uuid.New(),
		Name:   "agente",
		Type:   model.AgentTypeLLM,
		Model:  "openai/gpt-4.1-mini",
		Config: `{"api_key":"stored-key"}`,
	}
	repo.stored = existing

	updated, err := svc.Update(context.Background(), &model.Agent{
		Name:   "agente",
		Type:   model.AgentTypeLLM,
		Model:  "perplexity/sonar-pro",
		Config: `{}`,
	}, existing.ID)
	if err != nil {
		t.Fatalf("a valid llm update was rejected: %v", err)
	}

	if updated.Model != "perplexity/sonar-pro" {
		t.Errorf("updated model = %q, want %q", updated.Model, "perplexity/sonar-pro")
	}
}

// Only the invalid agent is asserted: the loop writes agent by agent, so the valid
// entry before it is already persisted when the rejection fires.
func TestImportAgents_RejectsLLMWithoutModelAndDoesNotImportIt(t *testing.T) {
	repo := &modelRequiredFakeRepo{}
	svc := serviceForModelValidation(repo)

	_, err := svc.ImportAgentsFromJSON(context.Background(), model.AgentImportRequest{
		AgentData: []map[string]interface{}{
			{"name": "valido", "type": "llm", "model": "openai/gpt-4.1-mini"},
			{"name": "sem_modelo", "type": "llm"},
		},
	})

	assertModelRejection(t, err)

	if repo.stored != nil && repo.stored.Name == "sem_modelo" {
		t.Error("the modelless agent was imported")
	}
}
