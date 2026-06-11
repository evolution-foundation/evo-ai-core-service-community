//go:build enterprise

// Package tenantstamp is the enterprise GORM plugin that stamps
// tenant_id on every INSERT into evo_core_* tables, mirroring the
// SQLAlchemy before_flush listener in PY-3 (evo-enterprise-licensing-
// python/src/evo_enterprise_licensing/tenant_stamp.py).
//
// The plugin lives under //go:build enterprise so the community
// release never imports it and the standalone build keeps its
// single-scope behaviour unchanged.
//
// Fail-closed: when runtimecontext.IDFromContext(ctx) returns "" the
// plugin does NOT set the column. The INSERT then carries tenant_id
// = uuid.Nil, which the gem-owned RLS policy
//
//	USING (tenant_id = current_setting('app.current_tenant_id', true)::uuid)
//
// rejects with "new row violates row-level security policy". The Go
// layer never invents a tenant id — Postgres is the source of truth
// for the binding contract.
package tenantstamp

import (
	"reflect"

	"evo-ai-core-service/pkg/evoextensions/runtimecontext"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// columnName is the column the gem's migrations add to each
// evo_core_* table. Keeping it as a constant (not a per-model tag
// lookup) lets the plugin stay model-agnostic.
const columnName = "tenant_id"

// callbackName must be unique across registered Create callbacks.
const callbackName = "evo:tenant_stamp"

// Plugin implements gorm.Plugin.
type Plugin struct{}

// Name returns the plugin identity used by GORM's plugin registry.
func (Plugin) Name() string { return callbackName }

// Initialize registers a Before("gorm:create") callback that stamps
// the tenant_id column on every INSERT when the bound model exposes
// that field.
func (Plugin) Initialize(db *gorm.DB) error {
	return db.Callback().Create().Before("gorm:create").Register(callbackName, stamp)
}

// stamp is the callback body. It is a no-op when:
//   - the statement has no parsed schema (raw SQL / Exec paths),
//   - the model does not declare a tenant_id column,
//   - the caller already set a non-zero tenant_id (seeders, backfill),
//   - no tenant id is bound to the request context (fail-closed).
func stamp(db *gorm.DB) {
	if db.Statement == nil || db.Statement.Schema == nil {
		return
	}
	field := db.Statement.Schema.LookUpField(columnName)
	if field == nil {
		return
	}

	ctx := db.Statement.Context
	if ctx == nil {
		return
	}
	tid := runtimecontext.IDFromContext(ctx)
	if tid == "" {
		// Fail-closed: leave tenant_id at uuid.Nil; the RLS policy
		// rejects the INSERT with "new row violates row-level
		// security policy". This is the documented AC for EVO-1624.
		return
	}
	parsed, err := uuid.Parse(tid)
	if err != nil {
		// A bad value in ctx is a programmer error upstream; refusing
		// to guess keeps the RLS rejection signal honest.
		return
	}

	rv := reflect.Indirect(db.Statement.ReflectValue)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			setIfZero(db, field, rv.Index(i), parsed)
		}
	case reflect.Struct:
		setIfZero(db, field, rv, parsed)
	}
}

// setIfZero writes parsed into the tenant_id field of elem only when
// the field is at its zero value. Respects callers that explicitly
// pre-populate tenant_id (seeders, backfill jobs).
func setIfZero(db *gorm.DB, field *schema.Field, elem reflect.Value, parsed uuid.UUID) {
	if !elem.IsValid() {
		return
	}
	_, isZero := field.ValueOf(db.Statement.Context, elem)
	if !isZero {
		return
	}
	_ = field.Set(db.Statement.Context, elem, parsed)
}
