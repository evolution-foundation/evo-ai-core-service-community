package service

import (
	"context"
	"testing"
)

// stubStoreCounts stands in for the database: how many secrets each store still
// holds inline, and how many imported credentials exist.
type stubStoreCounts struct {
	imported int64
	pending  map[string]int64
	err      error
}

func (s *stubStoreCounts) ImportedCredentials(_ context.Context) (int64, error) {
	return s.imported, s.err
}

func (s *stubStoreCounts) PendingInlineSecrets(_ context.Context, consumer string) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.pending[consumer], nil
}

func newState(counts *stubStoreCounts) *MigrationState {
	return NewMigrationState(counts)
}

// The semantics mirror Ai::MigrationState of story 1.6: a consumer is retired
// when the migration ran (a credential carries imported_from) OR when it has
// nothing left inline to migrate.
func TestRetiredWhenTheMigrationRan(t *testing.T) {
	state := newState(&stubStoreCounts{imported: 3, pending: map[string]int64{}})

	retired, err := state.Retired(context.Background())
	if err != nil {
		t.Fatalf("Retired: %v", err)
	}

	for _, consumer := range Consumers {
		if !retired[consumer] {
			t.Errorf("consumer %q is not retired even though the migration ran", consumer)
		}
	}
}

func TestRetiredWhenThereIsNothingToMigrate(t *testing.T) {
	// A fresh install: no imported credential, and no inline secret either.
	state := newState(&stubStoreCounts{imported: 0, pending: map[string]int64{}})

	retired, _ := state.Retired(context.Background())

	for _, consumer := range Consumers {
		if !retired[consumer] {
			t.Errorf("consumer %q should be retired on an install with nothing to migrate", consumer)
		}
	}
}

// The guard is per consumer: a store that still holds an inline secret keeps
// its fallback, even when another store already migrated.
func TestNotRetiredWhileAStoreStillHoldsInlineSecrets(t *testing.T) {
	state := newState(&stubStoreCounts{
		imported: 0,
		pending:  map[string]int64{ConsumerCustomTools: 2},
	})

	retired, _ := state.Retired(context.Background())

	if retired[ConsumerCustomTools] {
		t.Error("custom_tools is retired while it still holds inline secrets")
	}
	if !retired[ConsumerCustomMcpServers] {
		t.Error("an unrelated store was dragged down by another store's pending secrets")
	}
}

// Negative proof: a failure to read the state must NOT read as retired. Story
// 2.7 removes the inline fallback behind this guard, and a false "retired"
// would switch integrations off on a broken install.
func TestReadFailureIsNeverRetired(t *testing.T) {
	state := newState(&stubStoreCounts{err: context.DeadlineExceeded})

	retired, err := state.Retired(context.Background())

	if err == nil {
		t.Error("a read failure was swallowed instead of reported")
	}
	for consumer, value := range retired {
		if value {
			t.Errorf("consumer %q reads as retired despite a failed state read", consumer)
		}
	}
}

func TestConsumersCoverTheStoresTheFrontAsksAbout(t *testing.T) {
	// The screen keys off these exact names (vendor/crm, story 2.7).
	for _, expected := range []string{"custom_tools", "custom_mcp_servers", "knowledge_nexus", "agent_bots", "external_agents"} {
		found := false
		for _, consumer := range Consumers {
			if consumer == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("consumer %q is missing from the contract", expected)
		}
	}
}
