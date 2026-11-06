package service

import (
	"context"
	"errors"
	"testing"

	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/repository"

	"github.com/google/uuid"
)

type updateResultBotRepo struct {
	repository.AgentBotRepository
	err error
}

func (r updateResultBotRepo) Update(_ context.Context, bot *model.AgentBot, _ uuid.UUID) (*model.AgentBot, error) {
	if r.err != nil {
		return nil, r.err
	}
	return bot, nil
}

func TestUpdateAgentBot_SyncFlagFollowsTheWrite(t *testing.T) {
	cases := []struct {
		name     string
		repoErr  error
		wantSync bool
	}{
		{name: "failed update leaves the agent unsynced", repoErr: errors.New("bot refused"), wantSync: false},
		{name: "successful update enables sync", wantSync: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			botID := uuid.New()
			agent := &model.Agent{ID: uuid.New(), Name: "support", EvolutionBotID: &botID}
			svc := NewEvolutionService(updateResultBotRepo{err: tc.repoErr})

			_, err := svc.UpdateAgentBot(context.Background(), agent, "http://processor.test")

			if (err != nil) != (tc.repoErr != nil) {
				t.Fatalf("UpdateAgentBot error = %v, want error %v", err, tc.repoErr != nil)
			}
			if agent.EvolutionBotSync != tc.wantSync {
				t.Errorf("EvolutionBotSync = %v, want %v", agent.EvolutionBotSync, tc.wantSync)
			}
		})
	}
}
