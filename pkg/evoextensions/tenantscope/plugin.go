//go:build enterprise

// Package tenantscope routes READS (Query / Row / Raw) of tenant-scoped
// evo_core_* tables onto the scope-bound connection. It is the read-side
// symmetric of tenantstamp, which stamps writes.
//
// It resolves the scope only through the neutral runtimecontext bridge
// (IDFromContext / ConnFromContext), never by importing the enterprise
// module — that decoupling is the reason the bridge exists.
//
// Fail-closed: the statement is routed onto the bound connection; the
// session setting is never applied to a pooled connection, and a
// tenant-scoped read with no bound connection aborts rather than falling
// through to the pool.
package tenantscope

import (
	"evo-ai-core-service/pkg/evoextensions/runtimecontext"

	"errors"

	"gorm.io/gorm"
)

// columnName is the tenant discriminator the gem migrations add to each
// tenant-scoped evo_core_* table. A model exposing this field is the
// signal that the read must be scope-bound.
const columnName = "tenant_id"

const (
	queryCB = "evo:tenant_scope:query"
	rowCB   = "evo:tenant_scope:row"
	rawCB   = "evo:tenant_scope:raw"
)

// ErrScopeUnbound is the fail-closed sentinel raised when a tenant-scoped
// table is read with no scope-bound connection in context. Declared here
// (community) so the adapter does not import any enterprise error.
var ErrScopeUnbound = errors.New("tenantscope: tenant-scoped read with no bound connection")

// Plugin implements gorm.Plugin.
type Plugin struct{}

// Name returns the plugin identity used by GORM's plugin registry.
func (Plugin) Name() string { return "evo:tenant_scope" }

// Initialize registers Before callbacks on the read paths (Query, Row,
// Raw). Registration happens once at boot (db.Use), mirroring tenantstamp.
func (Plugin) Initialize(db *gorm.DB) error {
	if err := db.Callback().Query().Before("gorm:query").Register(queryCB, bindRead); err != nil {
		return err
	}
	if err := db.Callback().Row().Before("gorm:row").Register(rowCB, bindRead); err != nil {
		return err
	}
	if err := db.Callback().Raw().Before("gorm:raw").Register(rawCB, bindRead); err != nil {
		return err
	}
	return nil
}

// bindRead routes a tenant-scoped statement onto the scope-bound conn.
//
// No-op (leave on pool) when the statement has no parsed schema
// (raw/ad-hoc) or the model has no tenant_id column (untenanted table).
// Fail-closed (abort) when the table IS tenant-scoped but no scope-bound
// connection is present.
func bindRead(db *gorm.DB) {
	if db.Statement == nil || db.Statement.Schema == nil {
		return // raw/Exec path, no model → not our concern
	}
	if db.Statement.Schema.LookUpField(columnName) == nil {
		return // untenanted table → pool is fine
	}

	ctx := db.Statement.Context
	if ctx == nil {
		_ = db.AddError(ErrScopeUnbound)
		return
	}

	// A tenant-scoped read MUST run on the scope-bound conn. If no tenant
	// is bound at all, this is an unscoped read of a tenant table →
	// fail-closed (the permissive RLS branch would otherwise leak).
	if runtimecontext.IDFromContext(ctx) == "" {
		_ = db.AddError(ErrScopeUnbound)
		return
	}

	conn, ok := runtimecontext.ConnFromContext(ctx)
	if !ok {
		// Tenant id is bound but the scope-bound conn isn't reachable
		// (programming error on a tenant-scoped route, or a path that
		// bypassed the enterprise middleware). Refuse rather than read on
		// the pool with an empty GUC.
		_ = db.AddError(ErrScopeUnbound)
		return
	}

	// Route THIS statement onto the bound conn. runtimecontext.ScopedConn
	// is exactly gorm.ConnPool, so the read runs on the connection where
	// the enterprise build set app.current_tenant_id (tx-scoped) → RLS
	// filters to the caller's tenant. We never touch the global pool, so
	// no GUC leaks to other requests.
	db.Statement.ConnPool = conn
}
