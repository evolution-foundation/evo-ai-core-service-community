package processor

import (
	"context"

	"evo-ai-core-service/internal/utils/stringutils"
	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/service/config"
	"evo-ai-core-service/pkg/agent/service/validator"
)

type FlowProcessor struct {
	validator      *validator.AgentValidator
	generateAPIKey func() string
}

func NewFlowProcessor(validator *validator.AgentValidator, generateAPIKey func() string) BaseProcessor {
	return &FlowProcessor{
		validator:      validator,
		generateAPIKey: generateAPIKey,
	}
}

func (p *FlowProcessor) Create(ctx context.Context, agent *model.Agent) error {
	agentConfig := stringutils.JSONToInterfaceMap(agent.Config)
	if agentConfig == nil {
		return invalidf("invalid configuration: must be an object with sub_agents for %s agent", agent.Type)
	}

	if err := p.validator.ValidateFlowConfig(ctx, agentConfig, agent.Type); err != nil {
		return err
	}

	agentConfig["api_key"] = p.generateAPIKey()

	return config.UpdateConfig(agent, agentConfig)
}

func (p *FlowProcessor) Update(ctx context.Context, current, request *model.Agent) error {
	if isFlowType(request.Type) || request.Config != current.Config {
		updatedConfig := stringutils.JSONToInterfaceMap(request.Config)
		if updatedConfig == nil {
			updatedConfig = stringutils.JSONToInterfaceMap(current.Config)
		}

		if err := p.validator.ValidateFlowConfig(ctx, updatedConfig, request.Type); err != nil {
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

func isFlowType(t string) bool {
	return t == "sequential" || t == "parallel" || t == "loop"
}
