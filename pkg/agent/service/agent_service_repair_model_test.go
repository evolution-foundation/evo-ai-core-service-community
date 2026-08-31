package service

import (
	"context"
	"testing"

	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/repository"

	"github.com/google/uuid"
)

type repairFakeAgentRepo struct {
	repository.AgentRepository
	persistedModel string
	updateCalled   bool
}

func (f *repairFakeAgentRepo) Update(_ context.Context, agent *model.Agent, _ uuid.UUID) (*model.Agent, error) {
	f.updateCalled = true
	f.persistedModel = agent.Model
	return agent, nil
}

func TestSanitizeAgent_RepairStampsCurrentModel(t *testing.T) {
	repo := &repairFakeAgentRepo{}
	svc := &agentService{agentRepository: repo}

	agent := &model.Agent{
		ID:     uuid.New(),
		Type:   model.AgentTypeSequential,
		Config: `{"sub_agents":[]}`,
	}

	if err := svc.sanitizeAgent(context.Background(), agent); err != nil {
		t.Fatalf("sanitizeAgent returned error: %v", err)
	}

	if agent.Type != model.AgentTypeLLM {
		t.Fatalf("repair did not run: type is %q, want %q", agent.Type, model.AgentTypeLLM)
	}

	// Spelled out rather than read from defaultRepairModel: a test that asserts the
	// constant against itself passes whatever the constant rots into.
	const want = "openai/gpt-5.6-luna"
	if agent.Model != want {
		t.Errorf("repair model = %q, want %q", agent.Model, want)
	}

	if !repo.updateCalled {
		t.Error("repair did not persist — the stamped model reaches the database, and the test must cover that")
	}
	if repo.persistedModel != want {
		t.Errorf("persisted model = %q, want %q", repo.persistedModel, want)
	}
}

func TestSanitizeAgent_RepairKeepsModelTheAgentAlreadyHas(t *testing.T) {
	repo := &repairFakeAgentRepo{}
	svc := &agentService{agentRepository: repo}

	agent := &model.Agent{
		ID:     uuid.New(),
		Type:   model.AgentTypeSequential,
		Model:  "perplexity/sonar-pro",
		Config: `{"sub_agents":[]}`,
	}

	if err := svc.sanitizeAgent(context.Background(), agent); err != nil {
		t.Fatalf("sanitizeAgent returned error: %v", err)
	}

	if agent.Model != "perplexity/sonar-pro" {
		t.Errorf("repair overwrote a model the agent already had: got %q", agent.Model)
	}
}
