package processor

import (
	"context"
	"fmt"

	"evo-ai-core-service/internal/utils/stringutils"
	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/service/config"
	mcpmodel "evo-ai-core-service/pkg/mcp_server/model"

	"github.com/google/uuid"
)

type ConfigProcessor struct {
	generateAPIKey func() string
	getMCPServer   func(ctx context.Context, id uuid.UUID) (*mcpmodel.McpServer, error)
}

func NewConfigProcessor(
	generateAPIKey func() string,
	getMCPServer func(ctx context.Context, id uuid.UUID) (*mcpmodel.McpServer, error),
) ConfigProcessor {
	return ConfigProcessor{
		generateAPIKey: generateAPIKey,
		getMCPServer:   getMCPServer,
	}
}

func (p ConfigProcessor) ProcessAgentConfig(ctx context.Context, agent *model.Agent, existingConfig map[string]interface{}) error {
	agentConfig := stringutils.JSONToInterfaceMap(agent.Config)
	if agentConfig == nil {
		agentConfig = make(map[string]interface{})
	}

	processedConfig := make(map[string]interface{})

	// Ensure API key exists - preserve existing on update or generate if not present
	if apiKey, ok := agentConfig["api_key"].(string); ok && apiKey != "" {
		// Use the API key from the request if provided and non-empty
		processedConfig["api_key"] = apiKey
	} else if existingConfig != nil {
		// UPDATE: preserve existing API key if request doesn't have one
		if existingAPIKey, ok := existingConfig["api_key"].(string); ok && existingAPIKey != "" {
			processedConfig["api_key"] = existingAPIKey
		} else {
			// Generate only if no existing key
			processedConfig["api_key"] = p.generateAPIKey()
		}
	} else {
		// CREATE: generate new API key
		processedConfig["api_key"] = p.generateAPIKey()
	}

	commonFields := []string{
		"output_key",
		"enable_exit_loop",
		"load_memory",
		"preload_memory",
		"load_knowledge",
		"knowledge_tags",
		"planner",
		"output_schema",
		"agents_exit_loop",
		"tools",
		"custom_tools",
		"agent_tools",
		"sub_agents",
		// Message handling fields
		"message_wait_time",
		"message_signature",
		"enable_text_segmentation",
		"max_characters_per_segment",
		"min_segment_size",
		"character_delay_ms",
		// Behavior settings
		"transfer_to_human",
		"use_emojis",
		"allow_reminders",
		"allow_contact_edit",
		"contact_edit_config",
		"timezone",
		"send_as_reply",
		// Workflow automation fields
		"inactivity_actions",
		"transfer_rules",
		"pipeline_rules",
		// External sharing configuration (A2A)
		"external_sharing",
	}

	for _, field := range commonFields {
		if value, exists := agentConfig[field]; exists {
			processedConfig[field] = value
		}
	}

	if preload, ok := agentConfig["preload_memory"].(bool); ok && preload {
		if load, ok := agentConfig["load_memory"].(bool); !ok || !load {
			return fmt.Errorf("preload_memory requires load_memory to be enabled")
		}
	}

	if schema, exists := agentConfig["output_schema"]; exists && schema != nil {
		if err := p.validateOutputSchema(schema); err != nil {
			return fmt.Errorf("invalid output_schema: %w", err)
		}
	}

	if toolIDs, exists := agentConfig["custom_tool_ids"]; exists && toolIDs != nil {
		if ids, ok := toolIDs.([]interface{}); ok {
			strIDs := make([]string, len(ids))
			for i, id := range ids {
				strIDs[i] = fmt.Sprintf("%v", id)
			}
			processedConfig["custom_tool_ids"] = strIDs
		}
	}

	if servers, exists := agentConfig["mcp_servers"]; exists && servers != nil {
		processedServers, err := p.processMCPServers(ctx, servers)
		if err != nil {
			return fmt.Errorf("failed to process MCP servers: %w", err)
		}
		processedConfig["mcp_servers"] = processedServers
	}

	if tools, exists := agentConfig["tools"]; exists && tools != nil {
		processedTools, err := p.processTools(tools)
		if err != nil {
			return fmt.Errorf("failed to process tools: %w", err)
		}
		processedConfig["tools"] = processedTools
	}

	// Validate external agent provider if type is external
	if agent.Type == "external" {
		provider, ok := agentConfig["provider"].(string)
		if !ok || provider == "" {
			return fmt.Errorf("provider is required for external type agents")
		}
		validProviders := []string{"flowise", "n8n", "typebot", "dify", "openai"}
		valid := false
		for _, vp := range validProviders {
			if provider == vp {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid provider: %s. Must be one of: %v", provider, validProviders)
		}
		// Preserve provider in config
		processedConfig["provider"] = provider
	}

	return config.UpdateConfig(agent, processedConfig)
}

func (p ConfigProcessor) validateOutputSchema(schema interface{}) error {
	schemaMap, ok := schema.(map[string]interface{})
	if !ok {
		return fmt.Errorf("schema must be a dictionary")
	}

	// Allow empty schema objects
	if len(schemaMap) == 0 {
		return nil
	}

	validTypes := map[string]bool{
		"string":  true,
		"number":  true,
		"integer": true,
		"boolean": true,
		"array":   true,
		"object":  true,
	}

	for fieldName, fieldConfig := range schemaMap {
		fieldConfigMap, ok := fieldConfig.(map[string]interface{})
		if !ok {
			return fmt.Errorf("field config for '%s' must be a dictionary", fieldName)
		}

		fieldType, exists := fieldConfigMap["type"]
		if !exists {
			return fmt.Errorf("field '%s' must have a 'type' property", fieldName)
		}

		fieldTypeStr, ok := fieldType.(string)
		if !ok {
			return fmt.Errorf("field '%s' type must be a string", fieldName)
		}

		if !validTypes[fieldTypeStr] {
			validTypesList := []string{"string", "integer", "number", "boolean", "array", "object"}
			return fmt.Errorf("field '%s' has invalid type '%s'. Valid types: %v", fieldName, fieldTypeStr, validTypesList)
		}
	}

	return nil
}

func (p ConfigProcessor) processMCPServers(ctx context.Context, servers interface{}) ([]map[string]interface{}, error) {
	serverList, ok := servers.([]interface{})
	if !ok {
		return nil, fmt.Errorf("mcp_servers must be a list")
	}

	// OAuth integration IDs that don't require database lookup
	oauthIntegrationIDs := map[string]bool{
		"github":          true,
		"notion":          true,
		"stripe":          true,
		"linear":          true,
		"paypal":          true,
		"hubspot":         true,
		"monday":          true,
		"atlassian":       true,
		"asana":           true,
		"canva":           true,
		"supabase":        true,
		"google_calendar": true,
	}

	processedServers := make([]map[string]interface{}, 0)
	for _, server := range serverList {
		serverMap, ok := server.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid server configuration")
		}

		serverID, ok := serverMap["id"].(string)
		if !ok {
			return nil, fmt.Errorf("server id is required")
		}

		// Check if this is an OAuth integration (string ID) or custom MCP server (UUID)
		if oauthIntegrationIDs[serverID] {
			// OAuth integration - validate structure but don't require database lookup
			environments, ok := serverMap["environments"].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("server environments must be a dictionary")
			}

			processedServers = append(processedServers, map[string]interface{}{
				"id":           serverID,
				"environments": environments,
				"tools":        serverMap["tools"],
			})
			continue
		}

		// Custom MCP server - requires UUID and database lookup
		uuid, err := uuid.Parse(serverID)
		if err != nil {
			return nil, fmt.Errorf("invalid server id: %v (must be UUID or OAuth integration: github, notion, stripe, linear, paypal, hubspot, monday, atlassian, asana, canva, supabase, google_calendar)", err)
		}

		mcpServer, err := p.getMCPServer(ctx, uuid)
		if err != nil {
			return nil, fmt.Errorf("MCP server not found: %s", serverID)
		}

		environments, ok := serverMap["environments"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("server environments must be a dictionary")
		}

		// EVO-2250 story 2.4 AC7: an env var whose value lives in the vault is
		// referenced by name here (credential_refs: {ENV_NAME: credential id}).
		// The runtime resolves it; this service only carries the reference.
		credentialRefs, _ := serverMap["credential_refs"].(map[string]interface{})

		mcpServerResponse := mcpServer.ToResponse()
		for envKey := range mcpServerResponse.Environments {
			if _, exists := environments[envKey]; exists {
				continue
			}
			// A required key satisfied by a vault reference is provided, just
			// not inline: demanding a plaintext value here would make the vault
			// unusable for exactly the secrets it exists to hold.
			if _, referenced := credentialRefs[envKey]; referenced {
				continue
			}
			return nil, fmt.Errorf("environment variable '%s' not provided for MCP server %s", envKey, mcpServer.Name)
		}

		processed := map[string]interface{}{
			"id":           serverID,
			"environments": serverMap["environments"],
			"tools":        serverMap["tools"],
		}

		// Carried ONLY when present: this allowlist is what reaches the agent
		// config, so a field left out here is silently dropped and the runtime
		// resolution downstream never sees a reference to resolve.
		if len(credentialRefs) > 0 {
			processed["credential_refs"] = credentialRefs
		}

		processedServers = append(processedServers, processed)
	}

	return processedServers, nil
}

func (p ConfigProcessor) processTools(tools interface{}) ([]map[string]interface{}, error) {
	toolList, ok := tools.([]interface{})
	if !ok {
		return nil, fmt.Errorf("tools must be a list")
	}

	processedTools := make([]map[string]interface{}, 0)
	for _, tool := range toolList {
		toolMap, ok := tool.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid tool configuration")
		}

		processedTool := map[string]interface{}{
			"id":          toolMap["id"],
			"name":        toolMap["name"],
			"description": toolMap["description"],
			"tags":        toolMap["tags"],
			"examples":    toolMap["examples"],
			"inputModes":  toolMap["inputModes"],
			"outputModes": toolMap["outputModes"],
			"config":      toolMap["config"],
		}

		processedTools = append(processedTools, processedTool)
	}

	return processedTools, nil
}
