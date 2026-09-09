package service

// CRM-577 — an `a2a` agent is a pointer to an agent card served elsewhere, and `card_url`
// is that pointer. Without it the agent cannot run: the builder in the processor refuses
// it at execution time (evo-ai-processor-community,
// src/services/adk/agents/a2a_agent_builder.py:52, "card_url is required for a2a agents").
//
// Two things were wrong, both reproduced live against develop before this fix:
//
//  1. CREATE already refused it in processor/a2a.go, but the caller replaced the reason
//     with a generic "Failed to process agent" — the API answered 500 INTERNAL_ERROR and
//     the user was told nothing actionable.
//  2. UPDATE never checked, because A2AProcessor.Update only acts when CardURL is
//     non-empty. Switching an agent to a2a while the stored card_url is empty was written
//     and answered 200. The row became an a2a agent with no card.
//
// Two things that are NOT true, and were checked rather than assumed:
//
//   - Sending card_url empty does not blank the stored value. The repository persists with
//     GORM Updates(struct), which skips zero values, so an omitted or empty field keeps
//     what the row had. That is also why the update rule below is checked against the
//     MERGED state: this API supports partial updates and Darwin relies on them.
//   - "It comes back fine from GET" is not a defence: both services DERIVE a url when the
//     column is empty (forceReturnCardUrl here, card_url_property in the processor). The
//     derivation serves the response; execution reads the raw column.

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

// cardURLFakeRepo records every write, so a test can assert that a rejected payload
// reached no write at all — "returned an error" and "wrote nothing" are different claims,
// and the defect is the write.
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

// Update mirrors what the real repository does — GORM Updates(struct), which skips zero
// values — instead of replacing the row. A fake that overwrites everything would make a
// partial edit look like data loss and would quietly invalidate every persistence
// assertion in this file.
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

// cardURLFakeA2AProcessor stands in for the real one, which would fetch the agent card
// over the network. It is wired deliberately: without it, removing the validation under
// test makes the service dereference a nil processor and panic, and a panicking binary
// aborts the run instead of reporting which examples the missing validation breaks — a
// red proof has to be readable.
type cardURLFakeA2AProcessor struct {
	created int
}

func (p *cardURLFakeA2AProcessor) Create(_ context.Context, agent *model.Agent) error {
	// Same refusal the real processor performs, so the fake does not accidentally accept
	// what production rejects.
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

// What the caller actually receives: the UI and Darwin only see the code, the status and
// the message. A rejection that arrives as a 500 INTERNAL_ERROR tells the user nothing —
// and that generic 500 is half of what this card is about.
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

	// Whitespace is not a url. Without TrimSpace this is the payload that slips past a
	// naive emptiness check and lands in the database.
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

// The hole this card is really about, reproduced live against develop before the fix: an
// llm agent (card_url empty, as every non-a2a agent is) switched to a2a. Type is a
// non-empty string, so GORM writes it; card_url stays empty; the API answers 200. The row
// is now an a2a agent with no card, which the processor refuses to run.
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

// THE REGRESSION GUARD. This API supports partial updates: the repository persists with
// GORM Updates(struct), which skips zero values, and Darwin edits agents that way — its
// update_ai_agent tool requires only the id. A validation written against the payload
// instead of the merged state rejects this rename, breaking an edit that works today.
// Confirmed against develop: sending card_url empty does NOT blank the stored one.
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
		// só o nome muda, nem type nem card_url viajam
		{Name: "nome_novo", Config: "{}"},
		// o type viaja, o card_url não — o caso que quebraria com validação do payload cru
		{Name: "nome_novo", Type: model.AgentTypeA2A, Config: "{}"},
		// card_url vazio: sob a semântica parcial isso significa "mantenha", não "apague"
		{Name: "nome_novo", Type: model.AgentTypeA2A, CardURL: "", Config: "{}"},
	} {
		if _, err := svc.Update(context.Background(), payload, existing.ID); err != nil {
			t.Errorf("partial update %+v was rejected: %v", payload, err)
		}
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

// The regression a validation that is too broad would cause: no other type carries a
// card_url, and every one of them has to stay creatable without it.
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

// Deliberately NOT enforced: the API schema of the processor demands the
// /.well-known/agent.json suffix (schemas.py:102-108), but its execution path does not,
// and the core does something stronger at create time — it fetches the card and fails if
// the url does not serve one. Matching the string would reject a card served at another
// path that actually works. The divergence is recorded rather than hidden.
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
