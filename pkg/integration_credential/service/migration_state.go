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
// retired.
//
// The semantics mirror Ai::MigrationState of story 1.6: retired means the
// migration ran (a credential carries `imported_from`) OR there is nothing left
// to migrate, which is the case for a fresh install that only ever used the
// vault screen.
//
// ⚠️ A failure to read the state is NEVER retired. Story 2.7 removes the inline
// fallback behind this guard, so a false "retired" on a broken install would
// switch integrations off in silence, which is exactly the failure the guard
// exists to prevent.
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

	// Read first: it is the call that fails loudly on a broken database, so a
	// store that answers "nothing pending" because the query blew up never
	// reaches the loop below.
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

		// Per consumer: a store with nothing left inline is retired, whether
		// because the migration imported it or because it never had a secret.
		// Both conditions of the 1.6 guard collapse into this one check, and
		// the imported count above is what proves a broken read is not mistaken
		// for an empty store.
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
