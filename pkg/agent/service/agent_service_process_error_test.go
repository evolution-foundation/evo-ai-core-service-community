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
	if getMCPServer == nil {
		getMCPServer = func(_ context.Context, _ uuid.UUID) (*mcpmodel.McpServer, error) {
			return nil, errorsPostgres.MapDBError(gorm.ErrRecordNotFound, mcpmodel.MCPServerErrors)
		}
	}

	agentValidator := validator.NewAgentValidator(func(ctx context.Context, id uuid.UUID) (*model.Agent, error) {
		return nil, gorm.ErrRecordNotFound
	})

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
		"failed to fetch agent card", cardURL)

	if repo.createCalled {
		t.Error("an agent without a card was written")
	}
}

func TestCreate_A2AWhoseCardURLServesHTMLIsRejectedWithTheFetchReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>" + cardBodyMarker + "</html>"))
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
	if _, _, status := apierrors.HandleError(err); status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for a lookup outage", status)
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
