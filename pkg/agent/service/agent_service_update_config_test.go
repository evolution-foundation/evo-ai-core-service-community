package service

// CRM-305: the update merges the stored config, and `current` arrives hydrated from
// GetByID — so these pin that the merge never persists a read-only expansion.

import (
	"context"
	"strings"
	"testing"

	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/service/processor"
	mcpmodel "evo-ai-core-service/pkg/mcp_server/model"

	"github.com/google/uuid"
)

func serviceWithConfigProcessor() *agentService {
	return &agentService{
		configProcessor: processor.NewConfigProcessor(
			func() string { return "generated-key" },
			func(_ context.Context, _ uuid.UUID) (*mcpmodel.McpServer, error) {
				return nil, nil
			},
		),
	}
}

func TestProcessAgentUpdate_DoesNotPersistHydratedCopies(t *testing.T) {
	toolID := uuid.New()
	serverID := uuid.New()
	current := &model.Agent{
		Type: model.AgentTypeLLM,
		// The shape GetByID returns: *_ids as stored, the expansions added in memory.
		Config: `{"api_key":"stored-key",` +
			`"custom_tool_ids":["` + toolID.String() + `"],` +
			`"custom_tools":{"http_tools":[{"name":"weather","endpoint":"https://x/api"}]},` +
			`"custom_mcp_server_ids":["` + serverID.String() + `"],` +
			`"custom_mcp_servers":[{}],` +
			`"allow_manage_labels":false}`,
	}
	request := &model.Agent{Type: model.AgentTypeLLM, Config: `{"use_emojis":true}`}

	if err := serviceWithConfigProcessor().processAgentUpdate(context.Background(), current, request); err != nil {
		t.Fatalf("processAgentUpdate returned error: %v", err)
	}

	if strings.Contains(request.Config, "http_tools") || strings.Contains(request.Config, "https://x/api") {
		t.Errorf("the frozen tool copy was written back by the update: %s", request.Config)
	}
	if strings.Contains(request.Config, "custom_mcp_servers") {
		t.Errorf("the hydrated MCP expansion was written back by the update: %s", request.Config)
	}
	if !strings.Contains(request.Config, toolID.String()) || !strings.Contains(request.Config, serverID.String()) {
		t.Errorf("the *_ids the expansions derive from were lost: %s", request.Config)
	}
	if !strings.Contains(request.Config, `"allow_manage_labels":false`) {
		t.Errorf("an unsent stored key was not preserved: %s", request.Config)
	}
}

func TestDropHydratedCopies_KeepsAnExpansionWithNoIDsBehindIt(t *testing.T) {
	// Without a *_ids list the expansion never ran, so the value is the agent's own.
	config := map[string]interface{}{
		"custom_tools":       map[string]interface{}{"http_tools": []interface{}{}},
		"custom_mcp_servers": []interface{}{map[string]interface{}{"id": "notion"}},
	}

	dropHydratedCopies(config)

	if config["custom_tools"] == nil || config["custom_mcp_servers"] == nil {
		t.Errorf("dropped a value no hydration could have produced: %v", config)
	}
}
