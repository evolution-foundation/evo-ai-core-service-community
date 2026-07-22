package service

// EVO-2126: reconstructCustomConfigurations must hydrate the agent config in memory
// only and never write it back. It runs on reads (GetByID/List); persisting froze a
// copy of the tool into the agent so catalog edits/deletes never reached it.
//
// The read paths also call sanitizeAgent, which DOES persist (name/type fixes). It runs
// BEFORE the hydration precisely so its Update can never carry the expanded copy back
// into the DB — the tests below pin that ordering.

import (
	"context"
	"strings"
	"testing"

	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/repository"
	mcpModel "evo-ai-core-service/pkg/custom_mcp_server/model"
	customMCPServer "evo-ai-core-service/pkg/custom_mcp_server/service"
	customToolModel "evo-ai-core-service/pkg/custom_tool/model"
	customTool "evo-ai-core-service/pkg/custom_tool/service"

	"github.com/google/uuid"
)

// fakeAgentRepo embeds the interface (nil) and records what Update persisted.
type fakeAgentRepo struct {
	repository.AgentRepository
	stored          *model.Agent
	updateCalled    bool
	persistedConfig string
}

func (f *fakeAgentRepo) GetByID(_ context.Context, _ uuid.UUID) (*model.Agent, error) {
	return f.stored, nil
}

func (f *fakeAgentRepo) Update(_ context.Context, agent *model.Agent, _ uuid.UUID) (*model.Agent, error) {
	f.updateCalled = true
	f.persistedConfig = agent.Config
	return agent, nil
}

// fakeCustomToolService embeds the interface (nil) and returns one tool by id.
type fakeCustomToolService struct {
	customTool.CustomToolService
	tool      customToolModel.CustomToolResponse
	listCalls int
}

func (f *fakeCustomToolService) List(_ context.Context, _ customToolModel.CustomToolListRequest) (*customToolModel.CustomToolListResponse, error) {
	f.listCalls++
	return &customToolModel.CustomToolListResponse{Items: []customToolModel.CustomToolResponse{f.tool}}, nil
}

func (f *fakeCustomToolService) ConvertToHTTPTool(tool customToolModel.CustomToolResponse) map[string]interface{} {
	return map[string]interface{}{"name": tool.Name, "endpoint": tool.Endpoint}
}

// fakeMCPServerService embeds the interface (nil) and returns one server by id.
type fakeMCPServerService struct {
	customMCPServer.CustomMcpServerService
	server *mcpModel.CustomMcpServer
}

func (f *fakeMCPServerService) GetByAgentConfig(_ context.Context, _ []uuid.UUID) ([]*mcpModel.CustomMcpServer, error) {
	return []*mcpModel.CustomMcpServer{f.server}, nil
}

func TestReconstructCustomConfigurations_DoesNotPersist(t *testing.T) {
	toolID := uuid.New()
	repo := &fakeAgentRepo{}
	svc := &agentService{
		agentRepository:   repo,
		customToolService: &fakeCustomToolService{tool: customToolModel.CustomToolResponse{ID: toolID, Name: "weather", Endpoint: "https://x/api"}},
	}

	agent := &model.Agent{
		ID:     uuid.New(),
		Config: `{"custom_tool_ids":["` + toolID.String() + `"]}`, // no custom_tools key -> expansion runs
	}

	if err := svc.reconstructCustomConfigurations(context.Background(), agent, nil); err != nil {
		t.Fatalf("reconstructCustomConfigurations returned error: %v", err)
	}

	// The bug (EVO-2126): a read must never write a frozen copy back to the DB.
	if repo.updateCalled {
		t.Error("agentRepository.Update was called — reconstruct must be in-memory only, never persist")
	}

	// Behaviour preserved: the response is still hydrated in memory (fresh from the
	// catalog on every read), so the API/frontend sees the expanded tool.
	if !strings.Contains(agent.Config, "custom_tools") || !strings.Contains(agent.Config, "weather") {
		t.Errorf("agent.Config was not hydrated in memory: %s", agent.Config)
	}
}

func TestReconstructCustomConfigurations_SkipsWhenCustomToolsPresent(t *testing.T) {
	// The frontend always sends custom_tools; the guard must skip expansion — it must
	// not touch the repo, not read the catalog, and not alter the config it was given.
	repo := &fakeAgentRepo{}
	tools := &fakeCustomToolService{tool: customToolModel.CustomToolResponse{ID: uuid.New(), Name: "weather", Endpoint: "https://x/api"}}
	svc := &agentService{agentRepository: repo, customToolService: tools}

	original := `{"custom_tool_ids":["` + uuid.New().String() + `"],"custom_tools":{"http_tools":[]}}`
	agent := &model.Agent{ID: uuid.New(), Config: original}

	if err := svc.reconstructCustomConfigurations(context.Background(), agent, nil); err != nil {
		t.Fatalf("returned error: %v", err)
	}
	if repo.updateCalled {
		t.Error("Update must not be called when custom_tools already exists")
	}
	if tools.listCalls != 0 {
		t.Errorf("catalog was read %d time(s) although the guard should have skipped", tools.listCalls)
	}
	if agent.Config != original {
		t.Errorf("config must be left untouched when the guard skips:\n got %s\nwant %s", agent.Config, original)
	}
}

func TestReconstructCustomConfigurations_MCPServers_DoesNotPersist(t *testing.T) {
	// Same freeze bug on the MCP-server path: the write-back is a single shared block,
	// so removing it must cover custom_mcp_server_ids too.
	serverID := uuid.New()
	repo := &fakeAgentRepo{}
	svc := &agentService{
		agentRepository:        repo,
		customMCPServerService: &fakeMCPServerService{server: &mcpModel.CustomMcpServer{ID: serverID, Name: "notion"}},
	}

	agent := &model.Agent{
		ID:     uuid.New(),
		Config: `{"custom_mcp_server_ids":["` + serverID.String() + `"]}`, // no custom_mcp_servers key
	}

	if err := svc.reconstructCustomConfigurations(context.Background(), agent, nil); err != nil {
		t.Fatalf("returned error: %v", err)
	}
	if repo.updateCalled {
		t.Error("agentRepository.Update was called on the MCP path — must be in-memory only")
	}
	if !strings.Contains(agent.Config, "custom_mcp_servers") {
		t.Errorf("agent.Config MCP servers not hydrated in memory: %s", agent.Config)
	}

	// Pre-existing, NOT fixed here: model.CustomMcpServer tags every field `json:"-"`,
	// so the branch marshals the servers to empty objects. The key lands, the data
	// never does. Pinned so nobody reads the assertion above as proof that the MCP
	// payload is populated. Fixing it means marshalling ToResponse() instead, which
	// would newly expose the server Headers (auth tokens) in the agent read response —
	// a payload/security decision outside this fix.
	if !strings.Contains(agent.Config, `"custom_mcp_servers":[{}]`) {
		t.Errorf("expected the known-empty MCP payload; the branch changed shape: %s", agent.Config)
	}
	if strings.Contains(agent.Config, "notion") {
		t.Error("MCP payload is now populated — good news, but update this test and the EVO-2126 notes")
	}
}

// Exercises the real read path (GetByID) to pin the sanitize-then-hydrate ordering,
// not just the two functions in isolation. sanitizeAgent persists (here: an invalid
// name) and, while it ran AFTER the hydration, its Update wrote the expanded tool
// (endpoint included) back into the DB — the same freeze, through the back door.
func TestGetByID_NeverPersistsHydratedConfig(t *testing.T) {
	toolID := uuid.New()
	repo := &fakeAgentRepo{
		stored: &model.Agent{
			ID:     uuid.New(),
			Name:   "My Agent", // the space makes sanitizeAgent rewrite the name and persist
			Type:   model.AgentTypeLLM,
			Config: `{"custom_tool_ids":["` + toolID.String() + `"]}`,
		},
	}
	svc := &agentService{
		agentRepository:   repo,
		customToolService: &fakeCustomToolService{tool: customToolModel.CustomToolResponse{ID: toolID, Name: "weather", Endpoint: "https://x/api"}},
	}

	agent, err := svc.GetByID(context.Background(), repo.stored.ID)
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}

	if !repo.updateCalled {
		t.Fatal("precondition failed: sanitizeAgent should have persisted the name fix")
	}
	if strings.Contains(repo.persistedConfig, "http_tools") || strings.Contains(repo.persistedConfig, "https://x/api") {
		t.Errorf("the frozen copy leaked into the DB through sanitizeAgent: %s", repo.persistedConfig)
	}
	// ...while the response the caller gets is still hydrated.
	if !strings.Contains(agent.Config, "weather") {
		t.Errorf("response not hydrated: %s", agent.Config)
	}
}

// A flow agent with no sub_agents is rewritten to LLM and persisted by sanitizeAgent.
// It must not leave an empty custom_tools placeholder behind: the hydration guard keys
// on presence, so the agent would silently lose its tools on every later read.
func TestSanitizeAgent_KeepsIDBearingConfigExpandable(t *testing.T) {
	toolID := uuid.New()
	repo := &fakeAgentRepo{}
	svc := &agentService{
		agentRepository:   repo,
		customToolService: &fakeCustomToolService{tool: customToolModel.CustomToolResponse{ID: toolID, Name: "weather", Endpoint: "https://x/api"}},
	}

	agent := &model.Agent{
		ID:     uuid.New(),
		Name:   "flow_agent",
		Type:   model.AgentTypeSequential,
		Config: `{"custom_tool_ids":["` + toolID.String() + `"],"sub_agents":[]}`,
	}

	if err := svc.sanitizeAgent(context.Background(), agent); err != nil {
		t.Fatalf("sanitizeAgent returned error: %v", err)
	}
	if !repo.updateCalled {
		t.Fatal("precondition failed: sanitizeAgent should have converted and persisted the agent")
	}
	if strings.Contains(repo.persistedConfig, "custom_tools") {
		t.Errorf("an empty custom_tools placeholder was persisted and will block hydration forever: %s", repo.persistedConfig)
	}

	if err := svc.reconstructCustomConfigurations(context.Background(), agent, nil); err != nil {
		t.Fatalf("reconstructCustomConfigurations returned error: %v", err)
	}
	if !strings.Contains(agent.Config, "weather") {
		t.Errorf("converted agent lost its tools on read: %s", agent.Config)
	}
}

// Hydration is no longer persisted, so it re-reads the catalog on every read. Listing a
// page must share one catalog read across all agents instead of firing one per agent.
func TestReconstructCustomConfigurations_SharesCatalogReadAcrossAgents(t *testing.T) {
	toolID := uuid.New()
	tools := &fakeCustomToolService{tool: customToolModel.CustomToolResponse{ID: toolID, Name: "weather", Endpoint: "https://x/api"}}
	svc := &agentService{agentRepository: &fakeAgentRepo{}, customToolService: tools}

	cache := &customToolCache{}
	for i := 0; i < 5; i++ {
		agent := &model.Agent{ID: uuid.New(), Config: `{"custom_tool_ids":["` + toolID.String() + `"]}`}
		if err := svc.reconstructCustomConfigurations(context.Background(), agent, cache); err != nil {
			t.Fatalf("agent %d: %v", i, err)
		}
		if !strings.Contains(agent.Config, "weather") {
			t.Fatalf("agent %d not hydrated: %s", i, agent.Config)
		}
	}

	if tools.listCalls != 1 {
		t.Errorf("catalog read %d times for 5 agents, want 1 (N+1 on List)", tools.listCalls)
	}
}
