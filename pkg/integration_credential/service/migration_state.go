package service

import (
	"context"

	"evo-ai-core-service/pkg/integration_credential/model"
)

// Consumers and the consumer keys live in the model package, so the repository
// can use them without importing this one.
var Consumers = model.Consumers

const (
	ConsumerCustomTools      = model.ConsumerCustomTools
	ConsumerCustomMcpServers = model.ConsumerCustomMcpServers
	ConsumerKnowledgeNexus   = model.ConsumerKnowledgeNexus
	ConsumerAgentBots        = model.ConsumerAgentBots
	ConsumerExternalAgents   = model.ConsumerExternalAgents
)

// StoreCounts reads how much is left to migrate.
type StoreCounts interface {
	// ImportedCredentials counts vault rows carrying `imported_from`, which is
	// what says "the migration ran here".
	ImportedCredentials(ctx context.Context) (int64, error)
	// PendingInlineSecrets counts secrets a consumer still holds inline with no
	// vault reference replacing them.
	PendingInlineSecrets(ctx context.Context, consumer string) (int64, error)
}

// MigrationState answers, per consumer, whether the inline fallback can be
// retired: the migration ran, or there was never anything inline to migrate.
// // ⚠️ A failed read is NEVER retired. The fallback is removed behind this guard,
// so a false "retired" switches integrations off in silence.
type MigrationState struct {
	counts StoreCounts
}

func NewMigrationState(counts StoreCounts) *MigrationState {
	return &MigrationState{counts: counts}
}

func (s *MigrationState) Retired(ctx context.Context) (map[string]bool, error) {
	retired := make(map[string]bool, len(Consumers))
	for _, consumer := range Consumers {
		retired[consumer] = false
	}

	// Fails loudly on a broken database, so a store cannot answer "nothing
	// pending" because its query blew up.
	if _, err := s.counts.ImportedCredentials(ctx); err != nil {
		return retired, err
	}

	for _, consumer := range Consumers {
		pending, err := s.counts.PendingInlineSecrets(ctx, consumer)
		if err != nil {
			// Fail closed for every consumer: a partial answer here would let
			// story 2.7 remove a fallback that is still needed.
			return zeroed(), err
		}

		// Nothing left inline means retired, whether it was imported or never
		// existed. The count above is what tells a broken read from an empty store.
		retired[consumer] = pending == 0
	}

	return retired, nil
}

func zeroed() map[string]bool {
	retired := make(map[string]bool, len(Consumers))
	for _, consumer := range Consumers {
		retired[consumer] = false
	}
	return retired
}
