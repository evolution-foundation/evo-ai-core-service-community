package service

// EVO-2126: reconstructCustomConfigurations must hydrate the agent config in memory
// only and never write it back. It runs on reads (GetByID/List); persisting froze a
// copy of the tool into the agent so catalog edits/deletes never reached it.

import (
	"context"
	"strings"
	"testing"

	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/repository"
	customToolModel "evo-ai-core-service/pkg/custom_tool/model"
	customTool "evo-ai-core-service/pkg/custom_tool/service"

	"github.com/google/uuid"
)

// fakeAgentRepo embeds the interface (nil) and only records Update calls.
type fakeAgentRepo struct {
	repository.AgentRepository
	updateCalled bool
}

func (f *fakeAgentRepo) Update(_ context.Context, _ *model.Agent, _ uuid.UUID) (*model.Agent, error) {
	f.updateCalled = true
	return nil, nil
}

// fakeCustomToolService embeds the interface (nil) and returns one tool by id.
type fakeCustomToolService struct {
	customTool.CustomToolService
	tool customToolModel.CustomToolResponse
}

func (f *fakeCustomToolService) List(_ context.Context, _ customToolModel.CustomToolListRequest) (*customToolModel.CustomToolListResponse, error) {
	return &customToolModel.CustomToolListResponse{Items: []customToolModel.CustomToolResponse{f.tool}}, nil
}

func (f *fakeCustomToolService) ConvertToHTTPTool(tool customToolModel.CustomToolResponse) map[string]interface{} {
	return map[string]interface{}{"name": tool.Name, "endpoint": tool.Endpoint}
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

	if err := svc.reconstructCustomConfigurations(context.Background(), agent); err != nil {
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
	// The frontend always sends custom_tools; the guard must skip expansion (and thus
	// never touch the repo) when the key already exists.
	repo := &fakeAgentRepo{}
	svc := &agentService{agentRepository: repo, customToolService: &fakeCustomToolService{}}

	agent := &model.Agent{
		ID:     uuid.New(),
		Config: `{"custom_tool_ids":["` + uuid.New().String() + `"],"custom_tools":{"http_tools":[]}}`,
	}

	if err := svc.reconstructCustomConfigurations(context.Background(), agent); err != nil {
		t.Fatalf("returned error: %v", err)
	}
	if repo.updateCalled {
		t.Error("Update must not be called when custom_tools already exists")
	}
}
