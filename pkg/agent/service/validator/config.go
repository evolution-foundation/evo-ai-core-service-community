package validator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	apiErrors "evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/pkg/agent/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func invalidf(format string, args ...interface{}) error {
	return apiErrors.New(apiErrors.ValidationError, fmt.Sprintf(format, args...), http.StatusBadRequest)
}

type AgentValidator struct {
	getAgent func(ctx context.Context, id uuid.UUID) (*model.Agent, error)
}

func NewAgentValidator(getAgent func(ctx context.Context, id uuid.UUID) (*model.Agent, error)) *AgentValidator {
	return &AgentValidator{getAgent: getAgent}
}

func (v *AgentValidator) ValidateSubAgents(ctx context.Context, subAgents interface{}) error {
	subAgentsList, ok := subAgents.([]interface{})
	if !ok {
		return invalidf("sub_agents must be a list")
	}

	for _, sa := range subAgentsList {
		saID, ok := sa.(string)
		if !ok {
			return invalidf("invalid sub-agent ID format")
		}

		subAgentID, err := uuid.Parse(saID)
		if err != nil {
			return invalidf("invalid sub-agent ID: %v", err)
		}

		_, err = v.getAgent(ctx, subAgentID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return invalidf("sub-agent not found: %s", saID)
		}
		if err != nil {
			return fmt.Errorf("failed to look up sub-agent %s: %w", saID, err)
		}
	}
	return nil
}

func (v *AgentValidator) ValidateFlowConfig(ctx context.Context, config map[string]interface{}, agentType string) error {
	subAgents, ok := config["sub_agents"].([]interface{})
	if !ok {
		return invalidf("invalid configuration: sub_agents must be a list for %s agents", agentType)
	}

	if len(subAgents) == 0 {
		return invalidf("invalid configuration: %s agents must have at least one sub-agent", agentType)
	}

	return v.ValidateSubAgents(ctx, subAgents)
}

func (v *AgentValidator) ValidateTaskConfig(ctx context.Context, config map[string]interface{}) error {
	tasks, ok := config["tasks"].([]interface{})
	if !ok {
		return invalidf("invalid configuration: tasks is required")
	}

	if len(tasks) == 0 {
		return invalidf("invalid configuration: tasks cannot be empty")
	}

	for _, task := range tasks {
		if err := v.validateTask(ctx, task); err != nil {
			return err
		}
	}

	return nil
}

func (v *AgentValidator) validateTask(ctx context.Context, task interface{}) error {
	taskMap, ok := task.(map[string]interface{})
	if !ok {
		return invalidf("invalid task configuration")
	}

	log.Println(taskMap)

	agentID, ok := taskMap["agent_id"].(string)
	if !ok {
		return invalidf("each task must have an agent_id")
	}

	taskAgentID, err := uuid.Parse(agentID)
	if err != nil {
		return invalidf("invalid agent_id format: %v", err)
	}

	_, err = v.getAgent(ctx, taskAgentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return invalidf("agent not found for task: %s", agentID)
	}
	if err != nil {
		return fmt.Errorf("failed to look up the agent of task %s: %w", agentID, err)
	}

	return nil
}
