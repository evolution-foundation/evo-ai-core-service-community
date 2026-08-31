package service

import (
	"os"
	"strings"
	"testing"
)

const backfillMigration = "../../../migrations/000023_backfill_retired_repair_model.up.sql"

// The migration writes the repair model as a SQL literal, which no compiler checks.
// If the constant moves on and the literal stays, the backfill quietly installs a
// second, older default into customer data.
func TestBackfillMigrationWritesTheRepairModel(t *testing.T) {
	sql, err := os.ReadFile(backfillMigration)
	if err != nil {
		t.Fatalf("reading the backfill migration: %v", err)
	}

	if !strings.Contains(string(sql), "SET model = '"+defaultRepairModel+"'") {
		t.Errorf("migration does not write %q; it must match the model the repair stamps", defaultRepairModel)
	}

	if !strings.Contains(string(sql), "WHERE model = 'gpt-4.1-nano'") {
		t.Error("migration must target only the bare id the old repair wrote")
	}

	// Without this the UPDATE matches no row under tenant_isolation and the
	// migration still succeeds, which is the failure mode the backfill exists to avoid.
	if !strings.Contains(string(sql), "set_config('row_security', 'off', true)") {
		t.Error("migration must disable row security, or it can silently update nothing")
	}
}
