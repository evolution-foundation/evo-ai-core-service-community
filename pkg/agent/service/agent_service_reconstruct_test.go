package service

// EVO-2126: the hydration must stay in memory. sanitizeAgent also persists, so the
// read paths run it first — these tests pin both the invariant and that ordering.

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

	if repo.updateCalled {
		t.Error("agentRepository.Update was called — reconstruct must be in-memory only, never persist")
	}

	if !strings.Contains(agent.Config, "custom_tools") || !strings.Contains(agent.Config, "weather") {
		t.Errorf("agent.Config was not hydrated in memory: %s", agent.Config)
	}
}

func TestReconstructCustomConfigurations_SkipsWhenCustomToolsPresent(t *testing.T) {
	// The frontend always sends custom_tools, so the guard must skip the expansion.
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
	// The write-back was a single shared block, so the MCP path is covered too.
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

	// Pre-existing, not fixed here: model.CustomMcpServer tags every field `json:"-"`,
	// so the key lands but the data never does. Pinned so the assertion above is not
	// read as proof that the MCP payload is populated.
	if !strings.Contains(agent.Config, `"custom_mcp_servers":[{}]`) {
		t.Errorf("expected the known-empty MCP payload; the branch changed shape: %s", agent.Config)
	}
	if strings.Contains(agent.Config, "notion") {
		t.Error("MCP payload is now populated — good news, but update this test and the EVO-2126 notes")
	}
}

// Drives the real GetByID: with the sanitize/hydrate order flipped, the name fix
// persists the expanded tool and re-freezes the copy.
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
	if !strings.Contains(agent.Config, "weather") {
		t.Errorf("response not hydrated: %s", agent.Config)
	}
}

// The flow-to-LLM rewrite must not persist an empty custom_tools placeholder: the
// guard keys on presence, so the agent would lose its tools on every later read.
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

// A page of agents must share one catalog read instead of firing one per agent.
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
