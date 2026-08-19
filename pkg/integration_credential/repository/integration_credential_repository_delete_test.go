package repository

import (
	"context"
	"strings"
	"testing"

	"evo-ai-core-service/pkg/integration_credential/model"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DryRun renders the statement without a connection, so this proves the SQL
// the repository emits (a DELETE, not the UPDATE is_active it used to be)
// without a live database or a new dependency.
func dryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		postgres.New(postgres.Config{DriverName: "pgx"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatalf("open dry-run db: %v", err)
	}
	return db
}

// Captures every statement the repository sends through GORM, whichever
// operation it is: an UPDATE is_active would be recorded just the same, so
// the assertion below fails against the old soft delete.
func captureStatements(t *testing.T, db *gorm.DB) *[]string {
	t.Helper()
	var seen []string
	record := func(tx *gorm.DB) { seen = append(seen, tx.Statement.SQL.String()) }
	if err := db.Callback().Delete().After("gorm:delete").Register("capture_delete", record); err != nil {
		t.Fatalf("register delete callback: %v", err)
	}
	if err := db.Callback().Update().After("gorm:update").Register("capture_update", record); err != nil {
		t.Fatalf("register update callback: %v", err)
	}
	return &seen
}

func TestDeleteRemovesTheRow(t *testing.T) {
	db := dryRunDB(t)
	seen := captureStatements(t, db)
	repo := &integrationCredentialRepository{db: db}
	id := uuid.New()

	if _, err := repo.Delete(context.Background(), id); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if len(*seen) != 1 {
		t.Fatalf("expected exactly one statement, got %v", *seen)
	}
	sql := (*seen)[0]
	if !strings.HasPrefix(sql, `DELETE FROM "evo_core_integration_credentials" WHERE id = `) {
		t.Fatalf("expected a hard DELETE by id, got %q", sql)
	}
	if strings.Contains(strings.ToLower(sql), "is_active") {
		t.Fatalf("delete must not be a soft toggle, got %q", sql)
	}
}

// GORM turns Delete into an UPDATE deleted_at when the model has that field.
func TestIntegrationCredentialModelHasNoSoftDeleteColumn(t *testing.T) {
	stmt := &gorm.Statement{DB: dryRunDB(t)}
	if err := stmt.Parse(&model.IntegrationCredential{}); err != nil {
		t.Fatalf("parse model: %v", err)
	}
	if _, ok := stmt.Schema.FieldsByDBName["deleted_at"]; ok {
		t.Fatal("IntegrationCredential must not carry deleted_at")
	}
}
