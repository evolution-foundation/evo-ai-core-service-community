package service

// A processor rejection has to reach the client as a 4xx that says what went wrong:
// a bare 500 leaves the user guessing which field or which card URL to fix.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apierrors "evo-ai-core-service/internal/httpclient/errors"
	errorsPostgres "evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/pkg/agent/client/a2a"
	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/service/processor"
	"evo-ai-core-service/pkg/agent/service/validator"
	mcpmodel "evo-ai-core-service/pkg/mcp_server/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const cardBodyMarker = "internal-page-that-must-not-leak"

func missingCardServer(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, cardBodyMarker, http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/.well-known/agent.json"
}

func unreachableCardURL(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL + "/.well-known/agent.json"
	server.Close()
	return url
}

func serviceForProcessErrors(repo *cardURLFakeRepo, getMCPServer func(context.Context, uuid.UUID) (*mcpmodel.McpServer, error)) *agentService {
	return serviceWithLookups(repo, getMCPServer, func(_ context.Context, _ uuid.UUID) (*model.Agent, error) {
		return nil, gorm.ErrRecordNotFound
	})
}

func serviceWithLookups(
	repo *cardURLFakeRepo,
	getMCPServer func(context.Context, uuid.UUID) (*mcpmodel.McpServer, error),
	getAgent func(context.Context, uuid.UUID) (*model.Agent, error),
) *agentService {
	if getMCPServer == nil {
		getMCPServer = func(_ context.Context, _ uuid.UUID) (*mcpmodel.McpServer, error) {
			return nil, errorsPostgres.MapDBError(gorm.ErrRecordNotFound, mcpmodel.MCPServerErrors)
		}
	}

	agentValidator := validator.NewAgentValidator(getAgent)

	return &agentService{
		agentRepository:  repo,
		evolutionService: cardURLFakeEvolution{},
		a2aProcessor:     processor.NewA2AProcessor(a2a.NewClient()),
		flowProcessor:    processor.NewFlowProcessor(agentValidator, func() string { return "generated-key" }),
		taskProcessor:    processor.NewTaskProcessor(agentValidator, func() string { return "generated-key" }),
		configProcessor:  processor.NewConfigProcessor(func() string { return "generated-key" }, getMCPServer),
	}
}

func assertClientError(t *testing.T, err error, wantStatus int, wantCode string, wantInMessage ...string) {
	t.Helper()

	if err == nil {
		t.Fatal("the request was accepted")
	}

	code, message, status := apierrors.HandleError(err)
	if status != wantStatus || code != wantCode {
		t.Fatalf("got %d %s (%q), want %d %s", status, code, message, wantStatus, wantCode)
	}
	for _, want := range wantInMessage {
		if !strings.Contains(message, want) {
			t.Errorf("message %q does not say %q", message, want)
		}
	}
	if strings.Contains(message, "Failed to process agent") {
		t.Errorf("message %q still carries the generic wrapper", message)
	}
}

func TestCreate_A2AWhoseCardURLAnswers404IsRejectedWithTheFetchReason(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForProcessErrors(repo, nil)
	cardURL := missingCardServer(t)

	_, err := svc.Create(context.Background(), model.Agent{
		Name:    "agente_a2a",
		Type:    model.AgentTypeA2A,
		CardURL: cardURL,
		Config:  "{}",
	})

	assertClientError(t, err, http.StatusUnprocessableEntity, apierrors.BusinessRuleViolation,
		"failed to fetch agent card", cardURL, "404")

	_, message, _ := apierrors.HandleError(err)
	if strings.Contains(message, cardBodyMarker) {
		t.Errorf("the body served by card_url was echoed back: %q", message)
	}
	if repo.createCalled {
		t.Error("an agent without a card was written")
	}
}

func TestCreate_A2AWhoseCardURLIsUnreachableIsRejectedWithTheFetchReason(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForProcessErrors(repo, nil)
	cardURL := unreachableCardURL(t)

	_, err := svc.Create(context.Background(), model.Agent{
		Name:    "agente_a2a",
		Type:    model.AgentTypeA2A,
		CardURL: cardURL,
		Config:  "{}",
	})

	assertClientError(t, err, http.StatusUnprocessableEntity, apierrors.BusinessRuleViolation,
		"failed to fetch agent card", "could not reach "+cardURL)

	_, message, _ := apierrors.HandleError(err)
	if strings.Contains(message, "connection refused") || strings.Contains(message, "dial tcp") {
		t.Errorf("the transport error was echoed back: %q", message)
	}

	if repo.createCalled {
		t.Error("an agent without a card was written")
	}
}

// A 200 is not enough: the body has to be a card, or the agent is saved with nothing to run.
func TestCreate_A2AWhoseCardURLAnswers200WithoutACardIsRejected(t *testing.T) {
	for name, body := range map[string]string{
		"html":         "<html>" + cardBodyMarker + "</html>",
		"empty body":   "",
		"json null":    "null",
		"empty object": "{}",
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			t.Cleanup(server.Close)

			repo := &cardURLFakeRepo{}
			svc := serviceForProcessErrors(repo, nil)

			_, err := svc.Create(context.Background(), model.Agent{
				Name:    "agente_a2a",
				Type:    model.AgentTypeA2A,
				CardURL: server.URL,
				Config:  "{}",
			})

			assertClientError(t, err, http.StatusUnprocessableEntity, apierrors.BusinessRuleViolation,
				"failed to fetch agent card", server.URL, "did not return a JSON agent card")

			if repo.createCalled {
				t.Error("an agent without a card was written")
			}
		})
	}
}

func TestCreate_ConfigTheProcessorRejectsIs400NamingTheField(t *testing.T) {
	missingServer := uuid.New().String()

	cases := []struct {
		name          string
		agent         model.Agent
		wantInMessage []string
	}{
		{
			name:          "invalid provider",
			agent:         model.Agent{Name: "externo", Type: model.AgentTypeExternal, Config: `{"provider":"zapier"}`},
			wantInMessage: []string{"provider", "zapier"},
		},
		{
			name:          "missing provider",
			agent:         model.Agent{Name: "externo", Type: model.AgentTypeExternal, Config: `{}`},
			wantInMessage: []string{"provider is required"},
		},
		{
			name: "unknown MCP server",
			agent: model.Agent{Name: "llm", Type: model.AgentTypeLLM, Model: "openai/gpt-4.1-mini",
				Config: `{"mcp_servers":[{"id":"` + missingServer + `","environments":{}}]}`},
			wantInMessage: []string{"mcp_servers", "MCP server not found", missingServer},
		},
		{
			name: "MCP server entry without an id",
			agent: model.Agent{Name: "llm", Type: model.AgentTypeLLM, Model: "openai/gpt-4.1-mini",
				Config: `{"mcp_servers":[{"environments":{}}]}`},
			wantInMessage: []string{"server at index 0 has no id"},
		},
		{
			name: "preload_memory without load_memory",
			agent: model.Agent{Name: "llm", Type: model.AgentTypeLLM, Model: "openai/gpt-4.1-mini",
				Config: `{"preload_memory":true}`},
			wantInMessage: []string{"preload_memory requires load_memory"},
		},
		{
			name: "output_schema field without type",
			agent: model.Agent{Name: "llm", Type: model.AgentTypeLLM, Model: "openai/gpt-4.1-mini",
				Config: `{"output_schema":{"resposta":{}}}`},
			wantInMessage: []string{"output_schema", "resposta"},
		},
		{
			name:          "sub-agent that does not exist",
			agent:         model.Agent{Name: "fluxo", Type: model.AgentTypeSequential, Config: `{"sub_agents":["` + missingServer + `"]}`},
			wantInMessage: []string{"sub-agent not found", missingServer},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &cardURLFakeRepo{}
			svc := serviceForProcessErrors(repo, nil)

			_, err := svc.Create(context.Background(), tc.agent)

			assertClientError(t, err, http.StatusBadRequest, apierrors.ValidationError, tc.wantInMessage...)
			if repo.createCalled {
				t.Error("the rejected agent was written")
			}
		})
	}
}

// A lookup that fails for any reason other than "not there" is ours, not the user's.
func TestCreate_MCPLookupOutageStaysAServerError(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForProcessErrors(repo, func(_ context.Context, _ uuid.UUID) (*mcpmodel.McpServer, error) {
		return nil, errors.New("connection reset by peer")
	})

	_, err := svc.Create(context.Background(), model.Agent{
		Name:   "llm",
		Type:   model.AgentTypeLLM,
		Model:  "openai/gpt-4.1-mini",
		Config: `{"mcp_servers":[{"id":"` + uuid.New().String() + `","environments":{}}]}`,
	})

	if err == nil {
		t.Fatal("the request was accepted")
	}
	code, message, status := apierrors.HandleError(err)
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d %s, want 500 for a lookup outage", status, code)
	}
	if strings.Contains(message, "invalid mcp_servers") {
		t.Errorf("a failure of ours is announced as invalid input: %q", message)
	}
	if repo.createCalled {
		t.Error("the agent was written despite the failed lookup")
	}
}

func TestCreate_SubAgentLookupOutageStaysAServerErrorWithoutTheDriverText(t *testing.T) {
	repo := &cardURLFakeRepo{}
	driverText := "failed to connect to `host=db.internal user=evo_app database=evo_community`"
	svc := serviceWithLookups(repo, nil, func(_ context.Context, _ uuid.UUID) (*model.Agent, error) {
		return nil, errors.New(driverText)
	})

	_, err := svc.Create(context.Background(), model.Agent{
		Name:   "fluxo",
		Type:   model.AgentTypeSequential,
		Config: `{"sub_agents":["` + uuid.New().String() + `"]}`,
	})

	if err == nil {
		t.Fatal("the request was accepted")
	}
	_, message, status := apierrors.HandleError(err)
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for a lookup outage", status)
	}
	if strings.Contains(message, "db.internal") {
		t.Errorf("the driver error was echoed back: %q", message)
	}
	if repo.createCalled {
		t.Error("the agent was written despite the failed lookup")
	}
}

func TestUpdate_A2AWhoseNewCardURLAnswers404IsRejectedAndTheRowIsIntact(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForProcessErrors(repo, nil)

	existing := &model.Agent{
		ID:      uuid.New(),
		Name:    "agente_a2a",
		Type:    model.AgentTypeA2A,
		CardURL: "https://parceiro.example.com/.well-known/agent.json",
		Config:  "{}",
	}
	repo.stored = existing
	cardURL := missingCardServer(t)

	_, err := svc.Update(context.Background(), &model.Agent{
		Name:    "agente_a2a",
		Type:    model.AgentTypeA2A,
		CardURL: cardURL,
		Config:  "{}",
	}, existing.ID)

	assertClientError(t, err, http.StatusUnprocessableEntity, apierrors.BusinessRuleViolation,
		"failed to fetch agent card", cardURL, "404")

	if repo.updateCalled {
		t.Error("the rejected edit was written")
	}
	if repo.stored.CardURL != existing.CardURL {
		t.Errorf("stored card_url became %q", repo.stored.CardURL)
	}
}

func TestUpdate_ConfigTheProcessorRejectsIs400AndTheRowIsIntact(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForProcessErrors(repo, nil)

	existing := &model.Agent{
		ID:     uuid.New(),
		Name:   "externo",
		Type:   model.AgentTypeExternal,
		Config: `{"api_key":"stored-key","provider":"n8n"}`,
	}
	repo.stored = existing

	_, err := svc.Update(context.Background(), &model.Agent{
		Name:   "externo",
		Type:   model.AgentTypeExternal,
		Config: `{"provider":"zapier"}`,
	}, existing.ID)

	assertClientError(t, err, http.StatusBadRequest, apierrors.ValidationError, "provider", "zapier")

	if repo.updateCalled {
		t.Error("the rejected edit was written")
	}
	if repo.stored.Config != existing.Config {
		t.Errorf("stored config became %q", repo.stored.Config)
	}
}

func TestImportAgents_EntryWhoseCardURLAnswers404ImportsNothing(t *testing.T) {
	repo := &cardURLFakeRepo{}
	svc := serviceForProcessErrors(repo, nil)

	_, err := svc.ImportAgentsFromJSON(context.Background(), model.AgentImportRequest{
		AgentData: []map[string]interface{}{
			{"name": "llm_ok", "type": "llm", "model": "openai/gpt-4.1-mini"},
			{"name": "agente_a2a", "type": "a2a", "card_url": missingCardServer(t)},
		},
	})

	assertClientError(t, err, http.StatusUnprocessableEntity, apierrors.BusinessRuleViolation, "failed to fetch agent card")

	if repo.createCalled {
		t.Error("an entry before the rejected one was written")
	}
}

// Raises what Postgres raises when the unique index on name is hit.
type duplicateNameRepo struct {
	cardURLFakeRepo
	taken string
}

func (f *duplicateNameRepo) Create(ctx context.Context, agent model.Agent) (*model.Agent, error) {
	if agent.Name == f.taken {
		return nil, &pgconn.PgError{
			Code:           "23505",
			Message:        `duplicate key value violates unique constraint "idx_evo_core_agents_name_unique"`,
			ConstraintName: "idx_evo_core_agents_name_unique",
			TableName:      "evo_core_agents",
		}
	}
	return f.cardURLFakeRepo.Create(ctx, agent)
}

// A name already taken is the caller's to fix, and the import is the same write as Create.
func TestImportAgents_NameAlreadyTakenAnswersTheSameAsCreate(t *testing.T) {
	entry := []map[string]interface{}{
		{"name": "ja_existe", "type": "llm", "model": "openai/gpt-4.1-mini"},
	}

	importRepo := &duplicateNameRepo{taken: "ja_existe"}
	importSvc := serviceForProcessErrors(&importRepo.cardURLFakeRepo, nil)
	importSvc.agentRepository = importRepo

	_, importErr := importSvc.ImportAgentsFromJSON(context.Background(), model.AgentImportRequest{AgentData: entry})

	createRepo := &duplicateNameRepo{taken: "ja_existe"}
	createSvc := serviceForProcessErrors(&createRepo.cardURLFakeRepo, nil)
	createSvc.agentRepository = createRepo

	_, createErr := createSvc.Create(context.Background(), model.Agent{
		Name:   "ja_existe",
		Type:   model.AgentTypeLLM,
		Model:  "openai/gpt-4.1-mini",
		Config: "{}",
	})

	wantCode, wantMessage, wantStatus := apierrors.HandleError(createErr)
	assertClientError(t, importErr, wantStatus, wantCode, wantMessage)

	_, message, _ := apierrors.HandleError(importErr)
	if strings.Contains(message, "SQLSTATE") || strings.Contains(message, "idx_evo_core_agents_name_unique") {
		t.Errorf("the constraint the driver raised was echoed back: %q", message)
	}
}
