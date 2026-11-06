package service

import (
	"context"
	"fmt"

	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/evoextensions/runtimecontext"
)

const botSavepoint = "agent_bot_write"

// A bot write is allowed to fail without failing the agent write. On the pool that
// holds by itself; on a request-bound transaction a failed statement aborts the
// whole transaction and the agent row goes with it, so the bot write gets a
// savepoint of its own there.
//
// botErr is the bot failure the caller may tolerate. txErr means the bound
// transaction can no longer be used and the request has to fail.
func writeAgentBot(ctx context.Context, write func() (*model.AgentBot, error)) (bot *model.AgentBot, botErr, txErr error) {
	conn, bound := runtimecontext.ConnFromContext(ctx)
	if !bound {
		bot, botErr = write()
		return bot, botErr, nil
	}

	if _, err := conn.ExecContext(ctx, "SAVEPOINT "+botSavepoint); err != nil {
		return nil, nil, fmt.Errorf("opening the bot savepoint: %w", err)
	}

	bot, botErr = write()
	if botErr != nil {
		if _, err := conn.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+botSavepoint); err != nil {
			return nil, nil, fmt.Errorf("rolling back the bot savepoint after %v: %w", botErr, err)
		}
		return nil, botErr, nil
	}

	if _, err := conn.ExecContext(ctx, "RELEASE SAVEPOINT "+botSavepoint); err != nil {
		return nil, nil, fmt.Errorf("releasing the bot savepoint: %w", err)
	}
	return bot, nil, nil
}
