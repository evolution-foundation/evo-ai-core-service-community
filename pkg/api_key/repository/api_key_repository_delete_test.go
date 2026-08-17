package repository

import (
	"context"
	"strings"
	"testing"

	"evo-ai-core-service/pkg/api_key/model"

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

func TestDeleteRemovesTheRow(t *testing.T) {
	db := dryRunDB(t)
	id := uuid.New()

	stmt := db.WithContext(context.Background()).Where("id = ?", id).Delete(&model.ApiKey{}).Statement

	sql := stmt.SQL.String()
	if !strings.HasPrefix(sql, `DELETE FROM "evo_core_api_keys"`) {
		t.Fatalf("expected a hard DELETE, got %q", sql)
	}
	if strings.Contains(strings.ToLower(sql), "is_active") {
		t.Fatalf("delete must not be a soft toggle, got %q", sql)
	}
	if len(stmt.Vars) != 1 || stmt.Vars[0] != id {
		t.Fatalf("expected the id as the only bind, got %v", stmt.Vars)
	}
}

// The model has no DeletedAt: GORM would otherwise turn Delete into an UPDATE
// deleted_at, which is the soft delete this card removes.
func TestApiKeyModelHasNoSoftDeleteColumn(t *testing.T) {
	stmt := &gorm.Statement{DB: dryRunDB(t)}
	if err := stmt.Parse(&model.ApiKey{}); err != nil {
		t.Fatalf("parse model: %v", err)
	}
	if _, ok := stmt.Schema.FieldsByDBName["deleted_at"]; ok {
		t.Fatal("ApiKey must not carry deleted_at")
	}
}
