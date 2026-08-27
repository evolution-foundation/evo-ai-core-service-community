package processor

import (
	"context"
	"fmt"
	"testing"

	"evo-ai-core-service/internal/utils/stringutils"
	"evo-ai-core-service/pkg/agent/model"
	mcpmodel "evo-ai-core-service/pkg/mcp_server/model"

	"github.com/google/uuid"
)

// CRM-305: PUT /agents/{id} used to rebuild the config from the request alone
// (only api_key survived), so a one-toggle edit wiped custom_tool_ids,
// mcp_servers, tools and every other stored key — 200, no warning. These tests
// pin the merge contract: keys the request does not send are preserved, keys it
// sends win, and clearing is explicit (null / empty list), never implicit.

// A processor whose MCP catalog REFUSES lookups: preserving stored mcp_servers
// must not re-resolve them, or a toggle update starts failing whenever a
// referenced server has since left the catalog.
func processorWithDeadCatalog() ConfigProcessor {
	return NewConfigProcessor(
		func() string { return "generated-key" },
		func(_ context.Context, _ uuid.UUID) (*mcpmodel.McpServer, error) {
			return nil, fmt.Errorf("catalog must not be consulted for stored servers")
		},
	)
}

func storedConfig() map[string]interface{} {
	return stringutils.JSONToInterfaceMap(`{
		"api_key": "stored-key",
		"custom_tool_ids": ["22222222-2222-2222-2222-222222222222"],
		"mcp_servers": [{"id": "` + serverID + `", "environments": {"TOKEN": "t"}, "tools": ["search"]}],
		"tools": [{"id": "tool-1", "name": "http"}],
		"message_wait_time": 30,
		"enable_text_segmentation": true,
		"inactivity_actions": [{"after_minutes": 10}]
	}`)
}

func processUpdate(t *testing.T, requestConfig string) map[string]interface{} {
	t.Helper()
	agent := &model.Agent{Type: model.AgentTypeLLM, Config: requestConfig}
	if err := processorWithDeadCatalog().ProcessAgentConfig(context.Background(), agent, storedConfig()); err != nil {
		t.Fatalf("update rejected: %v", err)
	}
	return stringutils.JSONToInterfaceMap(agent.Config)
}

func TestOneToggleUpdatePreservesStoredConfig(t *testing.T) {
	final := processUpdate(t, `{"use_emojis": true}`)

	if final["use_emojis"] != true {
		t.Error("the one key the request sent did not land")
	}
	for _, key := range []string{"custom_tool_ids", "mcp_servers", "tools", "message_wait_time", "enable_text_segmentation", "inactivity_actions"} {
		if final[key] == nil {
			t.Errorf("%s was wiped by an update that never mentioned it", key)
		}
	}
	if final["api_key"] != "stored-key" {
		t.Errorf("api_key = %v, want the stored one", final["api_key"])
	}
	if servers, ok := final["mcp_servers"].([]interface{}); !ok || len(servers) != 1 {
		t.Errorf("mcp_servers lost its entry: %v", final["mcp_servers"])
	}
}

func TestOmittedConfigKeepsEverything(t *testing.T) {
	// The worst case from the card: request without config left the agent with
	// only an api_key.
	final := processUpdate(t, "")

	for _, key := range []string{"custom_tool_ids", "mcp_servers", "tools", "message_wait_time", "enable_text_segmentation", "inactivity_actions"} {
		if final[key] == nil {
			t.Errorf("%s did not survive an update with no config at all", key)
		}
	}
}

func TestSentKeysWinAndEmptyListClears(t *testing.T) {
	final := processUpdate(t, `{"message_wait_time": 5, "custom_tool_ids": []}`)

	if fmt.Sprintf("%v", final["message_wait_time"]) != "5" {
		t.Errorf("message_wait_time = %v, want the request's 5", final["message_wait_time"])
	}
	if ids, ok := final["custom_tool_ids"].([]interface{}); !ok || len(ids) != 0 {
		t.Errorf("an explicit [] must clear custom_tool_ids, got %v", final["custom_tool_ids"])
	}
	// Unsent neighbours still stand.
	if final["mcp_servers"] == nil || final["enable_text_segmentation"] != true {
		t.Error("clearing one key dragged unsent keys with it")
	}
}

func TestPreloadMemoryValidatesAgainstStoredLoadMemory(t *testing.T) {
	existing := map[string]interface{}{"api_key": "stored-key", "load_memory": true}
	agent := &model.Agent{Type: model.AgentTypeLLM, Config: `{"preload_memory": true}`}

	if err := processorWithDeadCatalog().ProcessAgentConfig(context.Background(), agent, existing); err != nil {
		t.Fatalf("a partial update was rejected against the stored load_memory: %v", err)
	}

	final := stringutils.JSONToInterfaceMap(agent.Config)
	if final["preload_memory"] != true || final["load_memory"] != true {
		t.Errorf("effective pair not persisted: %v", agent.Config)
	}
}

func TestExternalProviderPreservedOnPartialUpdate(t *testing.T) {
	existing := map[string]interface{}{"api_key": "stored-key", "provider": "flowise"}
	agent := &model.Agent{Type: model.AgentTypeExternal, Config: `{"use_emojis": true}`}

	if err := processorWithDeadCatalog().ProcessAgentConfig(context.Background(), agent, existing); err != nil {
		t.Fatalf("update without resending provider was rejected: %v", err)
	}
	if final := stringutils.JSONToInterfaceMap(agent.Config); final["provider"] != "flowise" {
		t.Errorf("provider = %v, want the stored flowise", final["provider"])
	}
}

func TestCreateStillGeneratesFromScratch(t *testing.T) {
	// existingConfig == nil is the CREATE path: no backfill source, api_key generated.
	agent := &model.Agent{Type: model.AgentTypeLLM, Config: `{"use_emojis": true}`}

	if err := processorWithDeadCatalog().ProcessAgentConfig(context.Background(), agent, nil); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	final := stringutils.JSONToInterfaceMap(agent.Config)
	if final["api_key"] != "generated-key" {
		t.Errorf("api_key = %v, want a generated one on create", final["api_key"])
	}
	if final["custom_tool_ids"] != nil {
		t.Error("create invented keys out of nowhere")
	}
}
