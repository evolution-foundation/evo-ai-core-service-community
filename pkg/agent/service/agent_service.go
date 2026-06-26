package service

import (
	"context"
	"errors"
	"fmt"
	"log"

	errorsPostgres "evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/internal/utils/stringutils"
	"evo-ai-core-service/pkg/agent/client/a2a"
	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/repository"
	"evo-ai-core-service/pkg/agent/service/processor"
	"evo-ai-core-service/pkg/agent/service/validator"
	"evo-ai-core-service/pkg/agent/utils"
	apiKeyService "evo-ai-core-service/pkg/api_key/service"
	customMCPServer "evo-ai-core-service/pkg/custom_mcp_server/service"
	customToolModel "evo-ai-core-service/pkg/custom_tool/model"
	customTool "evo-ai-core-service/pkg/custom_tool/service"
	folderService "evo-ai-core-service/pkg/folder/service"
	mcpmodel "evo-ai-core-service/pkg/mcp_server/model"
	mcpserverservice "evo-ai-core-service/pkg/mcp_server/service"

	"github.com/google/uuid"
)

type AgentService interface {
	Create(ctx context.Context, request model.Agent) (*model.Agent, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.Agent, error)
	List(ctx context.Context, page int, pageSize int, filters []model.AgentListFilter, search string) (*model.AgentListResponse, error)
	Update(ctx context.Context, request *model.Agent, id uuid.UUID) (*model.Agent, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
	ImportAgents(ctx context.Context, request model.AgentImportRequest) ([]*model.AgentResponse, error)
	GetSharedAgent(ctx context.Context, id uuid.UUID, apiKey string) (*model.Agent, error)
	GetShareAgent(ctx context.Context, id uuid.UUID) (string, error)
	AssignFolder(ctx context.Context, id uuid.UUID, request *model.Agent) (*model.Agent, error)
	ListAgentsByFolderID(ctx context.Context, folderId uuid.UUID, page int, pageSize int) (*model.AgentListResponse, error)
}

type agentService struct {
	agentRepository        repository.AgentRepository
	folderService          folderService.FolderService
	apiKeyService          apiKeyService.ApiKeyService
	a2aProcessor           processor.BaseProcessor
	taskProcessor          processor.BaseProcessor
	flowProcessor          processor.BaseProcessor
	workProcessor          processor.BaseProcessor
	externalProcessor      processor.BaseProcessor
	configProcessor        processor.ConfigProcessor
	customToolService      customTool.CustomToolService
	customMCPServerService customMCPServer.CustomMcpServerService
	evolutionService       EvolutionService
	aiProcessorURL         string
}

func NewAgentService(
	agentRepository repository.AgentRepository,
	folderService folderService.FolderService,
	apiKeyService apiKeyService.ApiKeyService,
	mcpServerService mcpserverservice.McpServerService,
	customToolService customTool.CustomToolService,
	customMCPServerService customMCPServer.CustomMcpServerService,
	evolutionService EvolutionService,
	aiProcessorURL string,
) AgentService {
	validator := validator.NewAgentValidator(func(ctx context.Context, id uuid.UUID) (*model.Agent, error) {
		return agentRepository.GetByID(ctx, id)
	})

	a2aProcessor := processor.NewA2AProcessor(a2a.NewClient())
	workProcessor := processor.NewWorkflowProcessor(utils.GenerateAPIKey)
	taskProcessor := processor.NewTaskProcessor(validator, utils.GenerateAPIKey)
	flowProcessor := processor.NewFlowProcessor(validator, utils.GenerateAPIKey)
	configProcessor := processor.NewConfigProcessor(
		utils.GenerateAPIKey,
		func(ctx context.Context, id uuid.UUID) (*mcpmodel.McpServer, error) {
			return mcpServerService.GetByID(ctx, id)
		},
	)

	return &agentService{
		agentRepository:        agentRepository,
		folderService:          folderService,
		apiKeyService:          apiKeyService,
		a2aProcessor:           a2aProcessor,
		taskProcessor:          taskProcessor,
		flowProcessor:          flowProcessor,
		workProcessor:          workProcessor,
		configProcessor:        configProcessor,
		customToolService:      customToolService,
		customMCPServerService: customMCPServerService,
		evolutionService:       evolutionService,
		aiProcessorURL:         aiProcessorURL,
	}
}

func (s *agentService) Create(ctx context.Context, request model.Agent) (*model.Agent, error) {
	if err := s.validateCreate(ctx, &request); err != nil {
		return nil, errors.New("Validation failed for agent: " + err.Error())
	}

	if err := s.processAgentCreate(ctx, &request); err != nil {
		return nil, errors.New("Failed to process agent")
	}

	agent, err := s.agentRepository.Create(ctx, request)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.AgentErrors)
	}

	// Create Evolution bot synchronously to ensure it works with the current context
	evolutionBot, err := s.evolutionService.CreateAgentBot(ctx, agent, s.aiProcessorURL)
	if err != nil {
		log.Printf("Failed to create Evolution bot for agent %s: %v", agent.ID, err)
		// Don't fail the agent creation if bot creation fails
		// The user can manually sync later using the sync endpoint
	} else {
		agent.EvolutionBotID = &evolutionBot.ID
		agent.EvolutionBotSync = true
		agent, err = s.agentRepository.Update(ctx, agent, agent.ID)
		if err != nil {
			s.evolutionService.CleanupEvolutionBot(ctx, evolutionBot.ID)
			return nil, errorsPostgres.MapDBError(err, model.AgentErrors)
		}
	}

	return agent, nil
}

func (s *agentService) validateCreate(ctx context.Context, request *model.Agent) error {
	return s.validateRelatedEntities(ctx, request, true)
}

func (s *agentService) validateRelatedEntities(ctx context.Context, request *model.Agent, isCreate bool) error {
	if request.FolderID != nil {
		if _, err := s.folderService.GetByID(ctx, *request.FolderID); err != nil {
			return err
		}
	}

	if request.ApiKeyID != nil {
		if _, err := s.apiKeyService.GetByID(ctx, *request.ApiKeyID); err != nil {
			return err
		}
	}

	return nil
}

func (s *agentService) processAgentCreate(ctx context.Context, request *model.Agent) error {
	// Pass nil for existingConfig since this is a CREATE operation
	if err := s.configProcessor.ProcessAgentConfig(ctx, request, nil); err != nil {
		return errors.New("Failed to process agent config")
	}

	switch request.Type {
	case model.AgentTypeLLM:
		// LLM agents don't need additional processing beyond config validation
		return nil
	case model.AgentTypeExternal:
		// External agents are validated in configProcessor (provider validation)
		// No additional processing needed beyond config validation
		return nil
	case model.AgentTypeA2A:
		return s.a2aProcessor.Create(ctx, request)
	case model.AgentTypeWorkflow:
		return s.workProcessor.Create(ctx, request)
	case model.AgentTypeTask:
		return s.taskProcessor.Create(ctx, request)
	case model.AgentTypeSequential, model.AgentTypeParallel, model.AgentTypeLoop:
		return s.flowProcessor.Create(ctx, request)
	default:
		return errors.New("Unsupported agent type: " + request.Type)
	}
}

func (s *agentService) Update(ctx context.Context, request *model.Agent, id uuid.UUID) (*model.Agent, error) {
	current, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, errors.New("Failed to get current agent")
	}

	if err := s.validateRelatedEntities(ctx, request, false); err != nil {
		return nil, errors.New("Validation failed: " + err.Error())
	}

	if err := s.processAgentUpdate(ctx, current, request); err != nil {
		// Include the original error message for better debugging
		return nil, errors.New(fmt.Sprintf("Failed to process agent: %v", err))
	}

	agent, err := s.agentRepository.Update(ctx, request, id)
	if err != nil {
		return nil, errors.New("Failed to update agent")
	}

	// Update Evolution bot synchronously to ensure it works with the current context
	evolutionBot, err := s.evolutionService.UpdateAgentBot(ctx, agent, s.aiProcessorURL)
	if err != nil {
		log.Printf("Failed to update Evolution bot for agent %s: %v", agent.ID, err)
		// Don't fail the agent update if bot update fails
		// The user can manually sync later using the sync endpoint
	} else {
		agent.EvolutionBotID = &evolutionBot.ID
		agent.EvolutionBotSync = true
		agent, err = s.agentRepository.Update(ctx, agent, agent.ID)
		if err != nil {
			return nil, errorsPostgres.MapDBError(err, model.AgentErrors)
		}
	}

	return agent, nil
}

func (s *agentService) processAgentUpdate(ctx context.Context, current, request *model.Agent) error {
	// Parse current config to preserve existing values (especially api_key)
	currentConfig := stringutils.JSONToInterfaceMap(current.Config)

	if err := s.configProcessor.ProcessAgentConfig(ctx, request, currentConfig); err != nil {
		return errors.New(fmt.Sprintf("Failed to process agent config: %v", err))
	}

	switch {
	case request.Type == model.AgentTypeLLM:
		// LLM agents don't need additional processing beyond config validation
		return nil
	case request.Type == model.AgentTypeExternal || current.Type == model.AgentTypeExternal:
		// External agents are validated in configProcessor (provider validation)
		// No additional processing needed beyond config validation
		return nil
	case request.Type == model.AgentTypeA2A || current.Type == model.AgentTypeA2A:
		return s.a2aProcessor.Update(ctx, current, request)
	case request.Type == model.AgentTypeWorkflow:
		return s.workProcessor.Update(ctx, current, request)
	case request.Type == model.AgentTypeTask:
		return s.taskProcessor.Update(ctx, current, request)
	case isFlowType(request.Type):
		return s.flowProcessor.Update(ctx, current, request)
	default:
		return errors.New("Unsupported agent type: " + request.Type)
	}
}

func isFlowType(t string) bool {
	return t == model.AgentTypeSequential ||
		t == model.AgentTypeParallel ||
		t == model.AgentTypeLoop
}

func (s *agentService) reconstructCustomConfigurations(ctx context.Context, agent *model.Agent) error {
	if agent.Config == "" {
		return nil
	}

	config := stringutils.JSONToInterfaceMap(agent.Config)
	reconstructed := false

	if customToolIDs, ok := config["custom_tool_ids"].([]interface{}); ok && len(customToolIDs) > 0 {
		if _, hasTools := config["custom_tools"]; !hasTools {
			toolIDs := make([]uuid.UUID, 0)
			for _, id := range customToolIDs {
				if strID, ok := id.(string); ok {
					if parsedID, err := uuid.Parse(strID); err == nil {
						toolIDs = append(toolIDs, parsedID)
					}
				}
			}

			if len(toolIDs) > 0 {
				req := customToolModel.CustomToolListRequest{
					Page:     1,
					PageSize: 100,
					Search:   "",
				}
				tools, err := s.customToolService.List(ctx, req)
				if err != nil {
					log.Printf("Error getting custom tools for agent %s: %v", agent.ID, err)
				} else {
					filteredTools := make([]interface{}, 0)
					for _, tool := range tools.Items {
						for _, id := range toolIDs {
							if tool.ID == id {
								httpTool := s.customToolService.ConvertToHTTPTool(tool)
								filteredTools = append(filteredTools, httpTool)
								break
							}
						}
					}

					if len(filteredTools) > 0 {
						config["custom_tools"] = map[string]interface{}{
							"http_tools": filteredTools,
						}
						reconstructed = true
					}
				}
			}
		}
	}

	if serverIDs, ok := config["custom_mcp_server_ids"].([]interface{}); ok && len(serverIDs) > 0 {
		if _, hasServers := config["custom_mcp_servers"]; !hasServers {
			mcpServerIDs := make([]uuid.UUID, 0)
			for _, id := range serverIDs {
				if strID, ok := id.(string); ok {
					if parsedID, err := uuid.Parse(strID); err == nil {
						mcpServerIDs = append(mcpServerIDs, parsedID)
					}
				}
			}

			if len(mcpServerIDs) > 0 {
				servers, err := s.customMCPServerService.GetByAgentConfig(ctx, mcpServerIDs)
				if err != nil {
					log.Printf("Error getting custom MCP servers for agent %s: %v", agent.ID, err)
				} else {
					config["custom_mcp_servers"] = servers
					reconstructed = true
				}
			}
		}
	}

	if reconstructed {
		agent.Config = stringutils.InterfaceMapToJSON(config)
		if _, err := s.agentRepository.Update(ctx, agent, agent.ID); err != nil {
			log.Printf("Error updating agent %s: %v", agent.ID, err)
			return err
		}
	}

	return nil
}

func (s *agentService) sanitizeAgent(ctx context.Context, agent *model.Agent) error {
	configUpdated := false

	if agent.Name != "" {
		sanitizedName := ""
		for _, c := range agent.Name {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' {
				sanitizedName += string(c)
			} else {
				sanitizedName += "_"
			}
		}
		if sanitizedName != agent.Name {
			agent.Name = sanitizedName
			configUpdated = true
		}
	}

	if agent.Config != "" && (agent.Type == model.AgentTypeSequential || agent.Type == model.AgentTypeParallel || agent.Type == model.AgentTypeLoop) {
		config := stringutils.JSONToInterfaceMap(agent.Config)

		subAgents, hasSubAgents := config["sub_agents"].([]interface{})
		if !hasSubAgents || len(subAgents) == 0 {
			log.Printf("Agent %s of type %s has empty or missing sub_agents. Updating to prevent validation error.", agent.ID, agent.Type)

			agent.Type = model.AgentTypeLLM

			if agent.Model == "" {
				agent.Model = "gpt-4.1-nano"
			}

			llmConfig := map[string]interface{}{
				"api_key":            config["api_key"],
				"temperature":        config["temperature"],
				"max_tokens":         config["max_tokens"],
				"tools":              config["tools"],
				"custom_tools":       config["custom_tools"],
				"mcp_servers":        config["mcp_servers"],
				"custom_mcp_servers": config["custom_mcp_servers"],
			}

			if llmConfig["api_key"] == nil {
				llmConfig["api_key"] = ""
			}
			if llmConfig["temperature"] == nil {
				llmConfig["temperature"] = 0.7
			}
			if llmConfig["max_tokens"] == nil {
				llmConfig["max_tokens"] = 1000
			}
			if llmConfig["tools"] == nil {
				llmConfig["tools"] = []interface{}{}
			}
			if llmConfig["custom_tools"] == nil {
				llmConfig["custom_tools"] = []interface{}{}
			}
			if llmConfig["mcp_servers"] == nil {
				llmConfig["mcp_servers"] = []interface{}{}
			}
			if llmConfig["custom_mcp_servers"] == nil {
				llmConfig["custom_mcp_servers"] = []interface{}{}
			}

			for key, value := range config {
				if _, exists := llmConfig[key]; !exists && key != "sub_agents" {
					llmConfig[key] = value
				}
			}

			agent.Config = stringutils.InterfaceMapToJSON(llmConfig)
			configUpdated = true
		}
	}

	if configUpdated {
		if _, err := s.agentRepository.Update(ctx, agent, agent.ID); err != nil {
			return errorsPostgres.MapDBError(err, model.AgentErrors)
		}
	}

	return nil
}

func (s *agentService) GetByID(ctx context.Context, id uuid.UUID) (*model.Agent, error) {
	agent, err := s.agentRepository.GetByID(ctx, id)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.AgentErrors)
	}

	if err := s.reconstructCustomConfigurations(ctx, agent); err != nil {
		log.Printf("Error reconstructing configurations for agent %s: %v", agent.ID, err)
	}

	if err := s.sanitizeAgent(ctx, agent); err != nil {
		log.Printf("Error sanitizing agent %s: %v", agent.ID, err)
		return nil, err
	}

	return agent, nil
}

func (s *agentService) List(ctx context.Context, page int, pageSize int, filters []model.AgentListFilter, search string) (*model.AgentListResponse, error) {
	agents, err := s.agentRepository.List(ctx, page, pageSize, filters, search)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.AgentErrors)
	}

	totalItems, err := s.agentRepository.Count(ctx, filters, search)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.AgentErrors)
	}

	for i, agent := range agents {
		if err := s.reconstructCustomConfigurations(ctx, agent); err != nil {
			log.Printf("Error reconstructing configurations for agent %s: %v", agent.ID, err)
			continue
		}
		if err := s.sanitizeAgent(ctx, agent); err != nil {
			log.Printf("Error sanitizing agent %s: %v", agent.ID, err)
			continue
		}
		agents[i] = agent
	}

	items := make([]model.AgentResponse, len(agents))
	for i, agent := range agents {
		items[i] = *agent.ToResponse(s.aiProcessorURL)
	}

	totalPages := int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	skip := (page - 1) * pageSize
	limit := pageSize

	return &model.AgentListResponse{
		Items:      items,
		Page:       page,
		PageSize:   pageSize,
		Skip:       skip,
		Limit:      limit,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}, nil
}

func (s *agentService) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	agent, err := s.GetByID(ctx, id)
	if err != nil {
		return false, err
	}

	// Salvar referências ANTES de qualquer alteração
	evolutionBotID := agent.EvolutionBotID
	evolutionBotSync := agent.EvolutionBotSync

	// PRIMEIRO: Deletar bot externo (antes de deletar agent)
	if evolutionBotID != nil && evolutionBotSync {
		log.Printf("Deleting Evolution bot %s for agent %s", *evolutionBotID, agent.ID)

		if err := s.evolutionService.DeleteAgentBot(ctx, agent); err != nil {
			log.Printf("Failed to delete Evolution bot %s for agent %s: %v, attempting safe deletion",
				*evolutionBotID, agent.ID, err)

			// Tentar safe delete como fallback
			if safeErr := s.evolutionService.DeleteAgentBotSafe(ctx, agent); safeErr != nil {
				log.Printf("WARNING: Bot %s may be orphaned after agent %s deletion: %v",
					*evolutionBotID, agent.ID, safeErr)
			} else {
				log.Printf("Successfully deleted Evolution bot %s using safe mode", *evolutionBotID)
			}
		} else {
			log.Printf("Successfully deleted Evolution bot %s for agent %s", *evolutionBotID, agent.ID)
		}
	}

	// SEGUNDO: Deletar agent do core service
	deleted, err := s.agentRepository.Delete(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to delete agent: %w", err)
	}

	log.Printf("Successfully deleted agent %s", id)
	return deleted, nil
}

func (s *agentService) GetSharedAgent(ctx context.Context, id uuid.UUID, apiKey string) (*model.Agent, error) {
	agent, err := s.GetByID(ctx, id)

	if err != nil {
		return nil, err
	}

	config := stringutils.JSONToInterfaceMap(agent.Config)
	apiKeyConfig := config["api_key"]

	if apiKeyConfig == nil || apiKeyConfig != apiKey {
		return nil, errors.New("Invalid API key")
	}

	return agent, nil
}

func (s *agentService) GetShareAgent(ctx context.Context, id uuid.UUID) (string, error) {
	agent, err := s.agentRepository.GetByID(ctx, id)
	if err != nil {
		return "", errors.New("Agent not found")
	}

	config := stringutils.JSONToInterfaceMap(agent.Config)
	apiKeyConfig := config["api_key"]

	if config == nil || apiKeyConfig == nil {
		return "", errors.New("This agent does not have an API key")
	}

	return apiKeyConfig.(string), nil
}

func (s *agentService) ImportAgents(ctx context.Context, request model.AgentImportRequest) ([]*model.AgentResponse, error) {
	if request.FolderID != nil {
		_, err := s.folderService.GetByID(ctx, *request.FolderID)
		if err != nil {
			return nil, err
		}
	}

	agentsData, err := s.ImportAgentsFromJSON(ctx, request)
	if err != nil {
		return nil, err
	}

	// Convert to response format
	responses := make([]*model.AgentResponse, len(agentsData))
	for i, agent := range agentsData {
		responses[i] = agent.ToResponse(s.aiProcessorURL)
	}

	return responses, nil
}

func (s *agentService) ImportAgentsFromJSON(ctx context.Context, request model.AgentImportRequest) ([]*model.Agent, error) {
	folderID := request.FolderID
	agentsData := request.AgentData

	importedAgents := make([]*model.Agent, 0, len(agentsData))

	for _, data := range agentsData {
		agent := &model.Agent{
			FolderID: folderID,
		}

		name, ok := data["name"].(string)
		if !ok {
			return nil, errors.New("Invalid agent data: name is required and must be a string")
		}
		agent.Name = name

		agentType, ok := data["type"].(string)
		if !ok {
			return nil, errors.New("Invalid agent data: type is required and must be a string")
		}
		agent.Type = agentType

		if description, ok := data["description"].(string); ok {
			agent.Description = description
		}
		if model, ok := data["model"].(string); ok {
			agent.Model = model
		}
		if instruction, ok := data["instruction"].(string); ok {
			agent.Instruction = instruction
		}
		if cardURL, ok := data["card_url"].(string); ok {
			agent.CardURL = cardURL
		}

		if config, ok := data["config"].(map[string]interface{}); ok {
			agent.Config = stringutils.InterfaceMapToJSON(config)
		} else {
			agent.Config = "{}"
		}

		if apiKeyIDStr, ok := data["api_key_id"].(string); ok {
			apiKeyID, err := uuid.Parse(apiKeyIDStr)
			if err == nil {
				agent.ApiKeyID = &apiKeyID
			}
		}

		if err := s.validateCreate(ctx, agent); err != nil {
			return nil, err
		}

		if err := s.processAgentCreate(ctx, agent); err != nil {
			return nil, err
		}

		createdAgent, err := s.agentRepository.Create(ctx, *agent)
		if err != nil {
			return nil, err
		}

		importedAgents = append(importedAgents, createdAgent)
	}

	return importedAgents, nil
}

func (s *agentService) AssignFolder(ctx context.Context, id uuid.UUID, request *model.Agent) (*model.Agent, error) {
	agent, err := s.agentRepository.GetByID(ctx, id)
	if err != nil {
		return nil, errors.New("Agent not found")
	}

	if request.FolderID == nil {
		agent, err = s.agentRepository.RemoveFolder(ctx, id)
		if err != nil {
			return nil, errorsPostgres.MapDBError(err, model.AgentErrors)
		}

		return agent, nil
	}

	_, err = s.folderService.GetByID(ctx, *request.FolderID)
	if err != nil {
		return nil, errors.New("Folder not found")
	}

	agent, err = s.agentRepository.Update(ctx, request, id)
	if err != nil {
		return nil, err
	}

	return agent, nil
}

func (s *agentService) ListAgentsByFolderID(ctx context.Context, folderId uuid.UUID, page int, pageSize int) (*model.AgentListResponse, error) {
	_, err := s.folderService.GetByID(ctx, folderId)
	if err != nil {
		return nil, errors.New("Folder not found")
	}

	agents, err := s.agentRepository.ListAgentsByFolderID(ctx, folderId, page, pageSize)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.AgentErrors)
	}

	// Get total count for the folder
	totalItems, err := s.agentRepository.CountByFolderID(ctx, folderId)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.AgentErrors)
	}

	for i, agent := range agents {
		if err := s.reconstructCustomConfigurations(ctx, agent); err != nil {
			log.Printf("Error reconstructing configurations for agent %s: %v", agent.ID, err)
			continue
		}
		if err := s.sanitizeAgent(ctx, agent); err != nil {
			log.Printf("Error sanitizing agent %s: %v", agent.ID, err)
			continue
		}
		agents[i] = agent
	}

	items := make([]model.AgentResponse, len(agents))
	for i, agent := range agents {
		items[i] = *agent.ToResponse(s.aiProcessorURL)
	}

	totalPages := int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	skip := (page - 1) * pageSize
	limit := pageSize

	return &model.AgentListResponse{
		Items:      items,
		Page:       page,
		PageSize:   pageSize,
		Skip:       skip,
		Limit:      limit,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}, nil
}

func (s *agentService) ListReadAgents(ctx context.Context, request *model.AgentReadRequest) ([]*model.Agent, error) {
	if request.FolderID != "" {
		folderId, err := uuid.Parse(request.FolderID)
		if err != nil {
			return nil, errors.New("Invalid folder_id: " + err.Error())
		}

		agentsResponse, err := s.ListAgentsByFolderID(ctx, folderId, request.Page, request.PageSize)
		if err != nil {
			return nil, err
		}

		// Convert AgentResponse back to Agent entities for internal use
		agents := make([]*model.Agent, len(agentsResponse.Items))
		for i, agentResp := range agentsResponse.Items {
			// Create basic agent from response data
			agent := &model.Agent{
				ID:               agentResp.ID,
				Name:             agentResp.Name,
				Description:      agentResp.Description,
				Type:             agentResp.Type,
				Model:            agentResp.Model,
				ApiKeyID:         agentResp.ApiKeyID,
				Instruction:      agentResp.Instruction,
				CardURL:          agentResp.CardURL,
				FolderID:         agentResp.FolderID,
				Role:             agentResp.Role,
				Goal:             agentResp.Goal,
				EvolutionBotID:   agentResp.EvolutionBotID,
				EvolutionBotSync: agentResp.EvolutionBotSync,
				CreatedAt:        agentResp.CreatedAt,
				UpdatedAt:        agentResp.UpdatedAt,
			}

			// Convert config back to JSON string
			if agentResp.Config != nil {
				agent.Config = stringutils.InterfaceMapToJSON(agentResp.Config)
			}

			agents[i] = agent
		}

		return agents, nil
	}

	agentsResponse, err := s.List(ctx, request.Page, request.PageSize, nil, "")
	if err != nil {
		return nil, err
	}

	// Convert AgentResponse back to Agent entities for internal use
	agents := make([]*model.Agent, len(agentsResponse.Items))
	for i, agentResp := range agentsResponse.Items {
		// Create basic agent from response data
		agent := &model.Agent{
			ID:               agentResp.ID,
			Name:             agentResp.Name,
			Description:      agentResp.Description,
			Type:             agentResp.Type,
			Model:            agentResp.Model,
			ApiKeyID:         agentResp.ApiKeyID,
			Instruction:      agentResp.Instruction,
			CardURL:          agentResp.CardURL,
			FolderID:         agentResp.FolderID,
			Role:             agentResp.Role,
			Goal:             agentResp.Goal,
			EvolutionBotID:   agentResp.EvolutionBotID,
			EvolutionBotSync: agentResp.EvolutionBotSync,
			CreatedAt:        agentResp.CreatedAt,
			UpdatedAt:        agentResp.UpdatedAt,
		}

		// Convert config back to JSON string
		if agentResp.Config != nil {
			agent.Config = stringutils.InterfaceMapToJSON(agentResp.Config)
		}

		agents[i] = agent
	}

	return agents, nil
}

