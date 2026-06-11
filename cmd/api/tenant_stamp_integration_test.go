//go:build integration && enterprise

// Integration test for EVO-1624 (GO-3): proves the tenantstamp plugin
// stamps tenant_id on INSERT and is fail-closed when no tenant id is
// bound, mirroring PY-3 (tenant_stamp.py) and the Linear ACs.
//
// Reuses the provision/teardown/openSuperuser/openAppGorm helpers from
// rls_leak_integration_test.go (same package, same build tags).
//
// Run with:
//
//	EVO_TENANT_TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/evo_community?sslmode=disable \
//	go test -tags="integration enterprise" ./cmd/api/...
package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"evo-ai-core-service/pkg/evoextensions/runtimecontext"
	"evo-ai-core-service/pkg/evoextensions/tenantstamp"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// rlsDemo is a GORM model mapped onto the synthetic table provisioned
// by rls_leak_integration_test.go. Same shape as the real
// evo_core_* models: an id column and a tenant_id column the plugin
// can stamp.
type rlsDemo struct {
	ID       int       `gorm:"column:id;primaryKey"`
	TenantID uuid.UUID `gorm:"column:tenant_id;type:uuid;not null"`
	Payload  string    `gorm:"column:payload"`
}

func (rlsDemo) TableName() string { return testRLSTable }

func TestIntegration_TenantStamp_StampsAndFailsClosed(t *testing.T) {
	super := openSuperuser(t)
	defer super.Close()
	provision(t, super)
	defer teardown(super)

	// Membership row is not required for these tests — the plugin
	// runs against the test role pool directly, not through the gin
	// middleware. We just need the synthetic RLS table.

	db := openAppGorm(t)
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	// Register the plugin under test. wire_enterprise.go does this
	// in production; here we install it on the test-scoped *gorm.DB
	// so the callback is wired identically.
	if err := db.Use(tenantstamp.Plugin{}); err != nil {
		t.Fatalf("install plugin: %v", err)
	}

	tenantA := uuid.New()

	t.Run("Stamping_AutoFillsTenantIDFromContext", func(t *testing.T) {
		// SET LOCAL on a tx-scoped session so RLS allows the INSERT,
		// then create via GORM with the tenant id on context. The
		// plugin reads runtimecontext, stamps tenant_id, and the
		// row lands with the bound tenant.
		ctx := runtimecontext.WithID(context.Background(), tenantA.String())
		row := rlsDemo{ID: 100, Payload: "stamped"}

		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(
				`SELECT set_config('app.current_tenant_id', ?, true)`,
				tenantA.String(),
			).Error; err != nil {
				return fmt.Errorf("bind GUC: %w", err)
			}
			// WithContext again so the callback sees the tenant id.
			if err := tx.WithContext(ctx).Create(&row).Error; err != nil {
				return fmt.Errorf("create: %w", err)
			}
			// Read back inside the same tx (RLS still binds).
			var got rlsDemo
			if err := tx.WithContext(ctx).First(&got, "id = ?", 100).Error; err != nil {
				return fmt.Errorf("read back: %w", err)
			}
			if got.TenantID != tenantA {
				return fmt.Errorf("plugin did not stamp: want %s, got %s", tenantA, got.TenantID)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("stamp tx: %v", err)
		}
	})

	t.Run("FailClosed_NoContext_RLSRejects", func(t *testing.T) {
		// No tenant id on context → plugin leaves tenant_id at
		// uuid.Nil. The strict policy rejects the WITH CHECK because
		// '00000000-...' != '' (current_setting with missing_ok).
		// Expected message contains "row-level security".
		row := rlsDemo{ID: 200, Payload: "should-fail"}
		err := db.WithContext(context.Background()).Create(&row).Error
		if err == nil {
			t.Fatalf("expected RLS rejection, got nil error")
		}
		low := strings.ToLower(err.Error())
		if !strings.Contains(low, "row-level security") &&
			!strings.Contains(low, "row level security") {
			t.Fatalf("expected row-level security error, got: %v", err)
		}
	})
}
