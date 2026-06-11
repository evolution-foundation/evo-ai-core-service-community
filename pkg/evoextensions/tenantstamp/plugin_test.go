//go:build enterprise

package tenantstamp

import (
	"context"
	"testing"

	"evo-ai-core-service/pkg/evoextensions/runtimecontext"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// stamped is the test model that mirrors the canonical evo_core_*
// shape: an id and a tenant_id column the plugin should fill in.
type stamped struct {
	ID       uuid.UUID `gorm:"type:text;primary_key"`
	TenantID uuid.UUID `gorm:"column:tenant_id;type:text"`
	Name     string    `gorm:"type:text"`
}

// bare is a model with NO tenant_id column. The plugin must treat
// Create on this struct as a no-op.
type bare struct {
	ID   uuid.UUID `gorm:"type:text;primary_key"`
	Name string    `gorm:"type:text"`
}

func openSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open sqlite: %v", err)
	}
	if err := db.Use(Plugin{}); err != nil {
		t.Fatalf("plugin install: %v", err)
	}
	if err := db.AutoMigrate(&stamped{}, &bare{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestStamp_NoTenantBound_LeavesZero(t *testing.T) {
	// Fail-closed: with no tenant id on ctx, the plugin must NOT
	// invent one. The row inserts (sqlite has no RLS) but the column
	// stays at uuid.Nil — the contract Postgres relies on to reject.
	db := openSQLite(t)
	row := stamped{ID: uuid.New(), Name: "no-bind"}
	if err := db.WithContext(context.Background()).Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got stamped
	if err := db.First(&got, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.TenantID != uuid.Nil {
		t.Fatalf("want tenant_id zero (fail-closed), got %s", got.TenantID)
	}
}

func TestStamp_TenantBound_AutoFills(t *testing.T) {
	db := openSQLite(t)
	tenantID := uuid.New()
	ctx := runtimecontext.WithID(context.Background(), tenantID.String())

	row := stamped{ID: uuid.New(), Name: "bound"}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got stamped
	if err := db.First(&got, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.TenantID != tenantID {
		t.Fatalf("plugin did not stamp: want %s, got %s", tenantID, got.TenantID)
	}
}

func TestStamp_CallerSetTenantID_NotOverwritten(t *testing.T) {
	// Seeders / backfill jobs pre-populate tenant_id explicitly.
	// The plugin must respect that, mirroring PY-3's "skip if
	// already set" rule.
	db := openSQLite(t)
	ctxTenant := uuid.New()
	callerTenant := uuid.New()
	ctx := runtimecontext.WithID(context.Background(), ctxTenant.String())

	row := stamped{ID: uuid.New(), TenantID: callerTenant, Name: "explicit"}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got stamped
	if err := db.First(&got, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.TenantID != callerTenant {
		t.Fatalf("plugin clobbered caller-set value: want %s, got %s", callerTenant, got.TenantID)
	}
}

func TestStamp_ModelWithoutTenantIDField_NoOp(t *testing.T) {
	// LookUpField(columnName) returns nil → callback returns clean.
	// Verifies the plugin never errors on unrelated tables.
	db := openSQLite(t)
	tenantID := uuid.New()
	ctx := runtimecontext.WithID(context.Background(), tenantID.String())

	row := bare{ID: uuid.New(), Name: "no-tenant-col"}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("create on bare model errored: %v", err)
	}
}

func TestStamp_InvalidTenantIDInContext_LeavesZero(t *testing.T) {
	// A non-UUID string on the context is a programmer error
	// upstream. The plugin must refuse to guess — leaving the
	// column zero so the RLS rejection signal stays honest.
	db := openSQLite(t)
	ctx := runtimecontext.WithID(context.Background(), "not-a-uuid")

	row := stamped{ID: uuid.New(), Name: "garbage-ctx"}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got stamped
	if err := db.First(&got, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.TenantID != uuid.Nil {
		t.Fatalf("invalid ctx value leaked into tenant_id: %s", got.TenantID)
	}
}

func TestStamp_BatchInsert_StampsEachRow(t *testing.T) {
	// GORM emits a single INSERT with multiple VALUES tuples for
	// slice creates. The reflect-slice branch in stamp() must walk
	// every element.
	db := openSQLite(t)
	tenantID := uuid.New()
	ctx := runtimecontext.WithID(context.Background(), tenantID.String())

	rows := []stamped{
		{ID: uuid.New(), Name: "a"},
		{ID: uuid.New(), Name: "b"},
		{ID: uuid.New(), Name: "c"},
	}
	if err := db.WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatalf("batch create: %v", err)
	}
	for _, r := range rows {
		var got stamped
		if err := db.First(&got, "id = ?", r.ID).Error; err != nil {
			t.Fatalf("read back %s: %v", r.ID, err)
		}
		if got.TenantID != tenantID {
			t.Fatalf("batch row %s not stamped: got %s", r.ID, got.TenantID)
		}
	}
}
