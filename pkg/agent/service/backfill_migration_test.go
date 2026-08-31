package service

import (
	"os"
	"strings"
	"testing"
)

const backfillMigration = "../../../migrations/000023_backfill_retired_repair_model.up.sql"

func readBackfillMigration(t *testing.T) string {
	t.Helper()
	sql, err := os.ReadFile(backfillMigration)
	if err != nil {
		t.Fatalf("reading the backfill migration: %v", err)
	}
	return string(sql)
}

// The migration writes the repair model as a SQL literal, which no compiler checks.
// If the constant moves on and the literal stays, the backfill quietly installs a
// second, older default into customer data.
func TestBackfillMigrationWritesTheRepairModel(t *testing.T) {
	sql := readBackfillMigration(t)

	if !strings.Contains(sql, "SET model = '"+defaultRepairModel+"'") {
		t.Errorf("migration does not write %q; it must match the model the repair stamps", defaultRepairModel)
	}

	if !strings.Contains(sql, "WHERE model = 'gpt-4.1-nano'") {
		t.Error("migration must target only the bare id the old repair wrote")
	}
}

// The role that runs the migrations owns evo_core_agents but crosses no tenant
// policy, and forced row level security filters even the owner. Without lifting
// FORCE the UPDATE matches nothing and the migration still reports success — the
// failure mode the backfill exists to avoid. Lifting it without putting it back
// would leave the table open to its owner, which is worse.
func TestBackfillMigrationLiftsAndRestoresForcedRowSecurity(t *testing.T) {
	sql := readBackfillMigration(t)

	if !strings.Contains(sql, "NO FORCE ROW LEVEL SECURITY") {
		t.Error("migration must lift forced row security, or it can silently update nothing")
	}

	if !strings.Contains(sql, "'ALTER TABLE evo_core_agents FORCE ROW LEVEL SECURITY'") {
		t.Error("migration must restore forced row security before it ends")
	}
}

// golang-migrate runs on lib/pq, which drops notices: a RAISE NOTICE reaches no log
// the deployment collects, so the affected count would be recorded nowhere.
func TestBackfillMigrationLogsTheCountWhereTheRunnerKeepsIt(t *testing.T) {
	sql := readBackfillMigration(t)

	if !strings.Contains(sql, "RAISE LOG") {
		t.Error("migration must RAISE LOG the affected count, which reaches the server log")
	}

	if strings.Contains(sql, "RAISE NOTICE") {
		t.Error("RAISE NOTICE is discarded by the migration runner; use RAISE LOG")
	}
}
