package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/repository"

	"github.com/google/uuid"
)

type EvolutionService interface {
	CreateAgentBot(ctx context.Context, agent *model.Agent, aiProcessorURL string) (*model.AgentBot, error)
	UpdateAgentBot(ctx context.Context, agent *model.Agent, aiProcessorURL string) (*model.AgentBot, error)
	DeleteAgentBot(ctx context.Context, agent *model.Agent) error
	DeleteAgentBotSafe(ctx context.Context, agent *model.Agent) error
	SyncAgentBot(ctx context.Context, agent *model.Agent, aiProcessorURL string) (*model.AgentBot, error)
	CleanupEvolutionBot(ctx context.Context, botID uuid.UUID)
}

type evolutionService struct {
	agentBotRepo repository.AgentBotRepository
}

func NewEvolutionService(agentBotRepo repository.AgentBotRepository) EvolutionService {
	return &evolutionService{
		agentBotRepo: agentBotRepo,
	}
}

func (s *evolutionService) CreateAgentBot(ctx context.Context, agent *model.Agent, aiProcessorURL string) (*model.AgentBot, error) {
	if agent.EvolutionBotSync {
		log.Printf("Agent %s already has Evolution bot sync enabled with bot ID %v", agent.ID, agent.EvolutionBotID)
		return nil, nil
	}

	log.Printf("Creating Evolution bot for agent %s", agent.ID)

	apiKey, err := s.getAgentAPIKey(agent)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent API key: %w", err)
	}

	advancedConfig := s.getAdvancedBotConfig(agent)
	bot := s.buildAgentBot(agent, aiProcessorURL, apiKey, advancedConfig)

	createdBot, err := s.agentBotRepo.Create(ctx, bot)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent bot: %w", err)
	}

	log.Printf("Successfully created Evolution bot %s for agent %s", createdBot.ID, agent.ID)
	return createdBot, nil
}

func (s *evolutionService) UpdateAgentBot(ctx context.Context, agent *model.Agent, aiProcessorURL string) (*model.AgentBot, error) {
	if agent.EvolutionBotID != nil && !agent.EvolutionBotSync {
		log.Printf("Agent %s has existing Evolution bot ID %s but sync disabled, updating bot and enabling sync", agent.ID, *agent.EvolutionBotID)
	} else if agent.EvolutionBotID == nil {
		log.Printf("Agent %s does not have Evolution bot, attempting to create new bot", agent.ID)
		return s.CreateAgentBot(ctx, agent, aiProcessorURL)
	}

	apiKey, err := s.getAgentAPIKey(agent)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent API key: %w", err)
	}

	advancedConfig := s.getAdvancedBotConfig(agent)
	bot := s.buildAgentBot(agent, aiProcessorURL, apiKey, advancedConfig)
	bot.ID = *agent.EvolutionBotID

	updatedBot, err := s.agentBotRepo.Update(ctx, &bot, *agent.EvolutionBotID)
	if err != nil {
		return nil, fmt.Errorf("failed to update agent bot: %w", err)
	}
	agent.EvolutionBotSync = true

	log.Printf("Successfully updated Evolution bot %s for agent %s", updatedBot.ID, agent.ID)
	return updatedBot, nil
}

func (s *evolutionService) DeleteAgentBot(ctx context.Context, agent *model.Agent) error {
	if !agent.EvolutionBotSync || agent.EvolutionBotID == nil {
		log.Printf("Agent %s does not have Evolution bot sync enabled", agent.ID)
		return nil
	}

	err := s.agentBotRepo.Delete(ctx, *agent.EvolutionBotID)
	if err != nil {
		log.Printf("Failed to delete Evolution bot %s for agent %s: %v", *agent.EvolutionBotID, agent.ID, err)
		return fmt.Errorf("failed to delete Evolution bot %s: %w", *agent.EvolutionBotID, err)
	}

	log.Printf("Successfully deleted Evolution bot %s for agent %s", *agent.EvolutionBotID, agent.ID)
	return nil
}

func (s *evolutionService) DeleteAgentBotSafe(ctx context.Context, agent *model.Agent) error {
	if !agent.EvolutionBotSync || agent.EvolutionBotID == nil {
		log.Printf("Agent %s does not have Evolution bot sync enabled", agent.ID)
		return nil
	}

	err := s.agentBotRepo.Delete(ctx, *agent.EvolutionBotID)
	if err != nil {
		log.Printf("Safe delete - Failed to delete Evolution bot %s for agent %s: %v", *agent.EvolutionBotID, agent.ID, err)
		return nil
	}

	log.Printf("Successfully deleted Evolution bot %s for agent %s", *agent.EvolutionBotID, agent.ID)
	return nil
}

func (s *evolutionService) SyncAgentBot(ctx context.Context, agent *model.Agent, aiProcessorURL string) (*model.AgentBot, error) {
	if agent.EvolutionBotSync && agent.EvolutionBotID != nil {
		return s.UpdateAgentBot(ctx, agent, aiProcessorURL)
	}
	return s.CreateAgentBot(ctx, agent, aiProcessorURL)
}

func (s *evolutionService) CleanupEvolutionBot(ctx context.Context, botID uuid.UUID) {
	err := s.agentBotRepo.Delete(ctx, botID)
	if err != nil {
		log.Printf("Failed to cleanup Evolution bot %s: %v", botID, err)
	}
}

func (s *evolutionService) getAgentAPIKey(agent *model.Agent) (string, error) {
	if agent.Config != "" {
		config := make(map[string]interface{})
		if err := json.Unmarshal([]byte(agent.Config), &config); err == nil {
			if apiKey, ok := config["api_key"].(string); ok && apiKey != "" {
				return apiKey, nil
			}
		}
	}
	return "evo-ai-bot-" + agent.ID.String(), nil
}

type advancedBotConfig struct {
	MessageWaitTime         int                      `json:"message_wait_time"`
	MessageSignature        string                   `json:"message_signature"`
	EnableTextSegmentation  bool                     `json:"enable_text_segmentation"`
	MaxCharactersPerSegment int                      `json:"max_characters_per_segment"`
	MinSegmentSize          int                      `json:"min_segment_size"`
	CharacterDelayMS        float64                  `json:"character_delay_ms"`
	SendAsReply             bool                     `json:"send_as_reply"`
	InactivityActions       []map[string]interface{} `json:"inactivity_actions,omitempty"`
	TransferRules           []map[string]interface{} `json:"transfer_rules,omitempty"`
	PipelineRules           []map[string]interface{} `json:"pipeline_rules,omitempty"`
	ContactEditConfig       map[string]interface{}   `json:"contact_edit_config,omitempty"`
}

func (s *evolutionService) buildAgentBot(agent *model.Agent, aiProcessorURL, apiKey string, config *advancedBotConfig) model.AgentBot {
	outgoingURL := fmt.Sprintf("%s/api/v1/a2a/%s", aiProcessorURL, agent.ID)

	bot := model.AgentBot{
		Name:                    agent.Name,
		Description:             agent.Description,
		OutgoingURL:             outgoingURL,
		APIKey:                  apiKey,
		BotType:                 0, // webhook
		BotProvider:             "evo_ai",
		MessageSignature:        config.MessageSignature,
		TextSegmentationEnabled: config.EnableTextSegmentation,
		TextSegmentationLimit:   config.MaxCharactersPerSegment,
		TextSegmentationMinSize: config.MinSegmentSize,
		DelayPerCharacter:       config.CharacterDelayMS,
		DebounceTime:            config.MessageWaitTime,
	}

	// Build bot_config JSON
	botConfig := make(map[string]interface{})
	if config.SendAsReply {
		botConfig["send_as_reply"] = true
	}
	if len(config.InactivityActions) > 0 {
		botConfig["inactivity_actions"] = config.InactivityActions
	}
	if len(config.TransferRules) > 0 {
		botConfig["transfer_rules"] = config.TransferRules
	}
	if len(config.PipelineRules) > 0 {
		botConfig["pipeline_rules"] = config.PipelineRules
	}
	if len(config.ContactEditConfig) > 0 {
		botConfig["contact_edit_config"] = config.ContactEditConfig
	}
	bot.SetBotConfigMap(botConfig)

	return bot
}

func (s *evolutionService) getAdvancedBotConfig(agent *model.Agent) *advancedBotConfig {
	config := &advancedBotConfig{
		MessageWaitTime:         5,
		MessageSignature:        "",
		EnableTextSegmentation:  false,
		MaxCharactersPerSegment: 300,
		MinSegmentSize:          50,
		CharacterDelayMS:        0.05,
	}

	if agent.Config == "" {
		return config
	}

	var agentConfig map[string]interface{}
	if err := json.Unmarshal([]byte(agent.Config), &agentConfig); err != nil {
		log.Printf("Failed to parse agent config for advanced bot settings: %v", err)
		return config
	}

	if messageWaitTime, ok := agentConfig["message_wait_time"].(float64); ok {
		config.MessageWaitTime = int(messageWaitTime)
	}
	if messageSignature, ok := agentConfig["message_signature"].(string); ok {
		config.MessageSignature = messageSignature
	}
	if enableTextSegmentation, ok := agentConfig["enable_text_segmentation"].(bool); ok {
		config.EnableTextSegmentation = enableTextSegmentation
	}
	if maxCharactersPerSegment, ok := agentConfig["max_characters_per_segment"].(float64); ok {
		config.MaxCharactersPerSegment = int(maxCharactersPerSegment)
	}
	if minSegmentSize, ok := agentConfig["min_segment_size"].(float64); ok {
		config.MinSegmentSize = int(minSegmentSize)
	}
	if characterDelayMs, ok := agentConfig["character_delay_ms"].(float64); ok {
		config.CharacterDelayMS = characterDelayMs
	}
	if sendAsReply, ok := agentConfig["send_as_reply"].(bool); ok {
		config.SendAsReply = sendAsReply
	}
	if inactivityActions, ok := agentConfig["inactivity_actions"].([]interface{}); ok {
		config.InactivityActions = make([]map[string]interface{}, 0, len(inactivityActions))
		for _, action := range inactivityActions {
			if actionMap, ok := action.(map[string]interface{}); ok {
				config.InactivityActions = append(config.InactivityActions, actionMap)
			}
		}
	}
	if transferRules, ok := agentConfig["transfer_rules"].([]interface{}); ok {
		config.TransferRules = make([]map[string]interface{}, 0, len(transferRules))
		for _, rule := range transferRules {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				config.TransferRules = append(config.TransferRules, ruleMap)
			}
		}
	}
	if pipelineRules, ok := agentConfig["pipeline_rules"].([]interface{}); ok {
		config.PipelineRules = make([]map[string]interface{}, 0, len(pipelineRules))
		for _, rule := range pipelineRules {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				config.PipelineRules = append(config.PipelineRules, ruleMap)
			}
		}
	}
	if contactEditConfig, ok := agentConfig["contact_edit_config"].(map[string]interface{}); ok {
		config.ContactEditConfig = contactEditConfig
	}

	return config
}
