package processor

import (
	"context"

	"evo-ai-core-service/internal/utils/stringutils"
	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/service/config"
	"evo-ai-core-service/pkg/agent/service/validator"
)

type TaskProcessor struct {
	validator      *validator.AgentValidator
	generateAPIKey func() string
}

func NewTaskProcessor(validator *validator.AgentValidator, generateAPIKey func() string) BaseProcessor {
	return &TaskProcessor{
		validator:      validator,
		generateAPIKey: generateAPIKey,
	}
}

func (p *TaskProcessor) Create(ctx context.Context, agent *model.Agent) error {
	agentConfig := stringutils.JSONToInterfaceMap(agent.Config)
	if agentConfig == nil {
		return invalidf("invalid configuration: must be an object with tasks")
	}

	if err := p.validator.ValidateTaskConfig(ctx, agentConfig); err != nil {
		return err
	}

	if err := p.validator.ValidateSubAgents(ctx, agentConfig["sub_agents"]); err != nil {
		return err
	}

	agentConfig["api_key"] = p.generateAPIKey()

	return config.UpdateConfig(agent, agentConfig)
}

func (p *TaskProcessor) Update(ctx context.Context, current, request *model.Agent) error {
	if request.Type == model.AgentTypeTask || request.Config != current.Config {
		updatedConfig := stringutils.JSONToInterfaceMap(request.Config)
		if updatedConfig == nil {
			updatedConfig = stringutils.JSONToInterfaceMap(current.Config)
		}

		if err := p.validator.ValidateTaskConfig(ctx, updatedConfig); err != nil {
			return err
		}

		if err := p.validator.ValidateSubAgents(ctx, updatedConfig["sub_agents"]); err != nil {
			return err
		}

		currentConfig := stringutils.JSONToInterfaceMap(current.Config)
		if apiKey, exists := currentConfig["api_key"]; exists && apiKey != "" {
			updatedConfig["api_key"] = apiKey
		} else {
			updatedConfig["api_key"] = p.generateAPIKey()
		}

		return config.UpdateConfig(request, updatedConfig)
	}

	return nil
}
