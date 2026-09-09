package service

// CRM-573: the processor refuses an `llm` agent with an empty `model`
// (src/schemas/schemas.py, @validator("model")), so the core must refuse it at
// write time instead of persisting a row that can never run.

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

// modelRequiredFakeRepo records every write so a test can assert that a rejected
// payload reached no write at all — "returned an error" is not the same claim as
// "nothing was persisted", and the defect is the persistence.
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

// Update mirrors what the real repository does — GORM Updates(struct), which skips zero
// values — instead of replacing the row. A fake that overwrote everything would make a
// partial edit look like data loss and would hide the very semantics this file relies on.
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

// modelRequiredFakeEvolution keeps Create/Update past the repository without a
// bot backend. It reports failure, the path the service already tolerates, so the
// bot round-trip does not add writes the persistence assertions would misread.
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

// assertModelRejection pins what the caller actually receives: the UI and Darwin
// only ever see the code, the status and the message. A rejection that reaches
// them as a 500 INTERNAL_ERROR is the silent failure this card is about.
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
	// Same sentence the processor raises, so the two services do not disagree
	// about why the same payload is invalid.
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

// Whitespace is not a model. Trimming it here keeps the core's answer identical to
// the processor's, which treats "   " as truthy but hands it to LiteLLM as a name
// no provider resolves.
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

// AC4 guard at the validation layer: the repair path for flow agents starts from a
// row with no model, so a blanket required-model rule would make those agents
// unwritable. The sanitizeAgent tests cover the repair itself.
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
		if err := svc.validateCreate(context.Background(), &agent); err != nil {
			t.Errorf("type %q was rejected for having no model: %v", agentType, err)
		}
	}
}

// The update that has to be refused is the one that would LEAVE the row without a model:
// the agent already stored broken (agente_teste_minimo, SUPORTEEVO-24) being edited
// without picking one. The row cannot be saved back into a state that cannot run.
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

// THE REGRESSION GUARD, and the reason the rule is checked against the merged state.
// This API supports partial updates: the repository persists with GORM Updates(struct),
// which skips zero values, so a field the client leaves out keeps what the row had —
// verified against a live server. Validating the incoming payload instead would reject
// this rename, which works today and which Darwin depends on.
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

	// name and type travel (the handler binds them as required); model does not.
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

// The import path builds its agents itself and never touches the handler binding,
// so it is a third door onto the same table and needs its own proof. Only the
// invalid agent is asserted: the loop writes agent by agent, so an earlier valid
// entry of the same file is already persisted when the rejection fires.
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
