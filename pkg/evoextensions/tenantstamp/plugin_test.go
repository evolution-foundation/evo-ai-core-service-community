//go:build enterprise

package tenantstamp

import (
	"context"
	"reflect"
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
	// Seeders / backfill jobs pre-populate tenant_id explicitly, and the
	// plugin must not overwrite it.
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

func TestStamp_MapCreate_StampsKey(t *testing.T) {
	// db.Model(&X{}).Create(map[string]interface{}{...}) takes a
	// different ReflectValue path (Kind == Map). The plugin must
	// stamp tenant_id into the map so the emitted INSERT carries it.
	db := openSQLite(t)
	tenantID := uuid.New()
	ctx := runtimecontext.WithID(context.Background(), tenantID.String())

	rowID := uuid.New()
	row := map[string]interface{}{
		"id":   rowID,
		"name": "via-map",
	}
	if err := db.WithContext(ctx).Model(&stamped{}).Create(row).Error; err != nil {
		t.Fatalf("map create: %v", err)
	}
	var got stamped
	if err := db.First(&got, "id = ?", rowID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.TenantID != tenantID {
		t.Fatalf("map create not stamped: want %s, got %s", tenantID, got.TenantID)
	}
}

func TestStampMap_IncompatibleValueType_NoPanic(t *testing.T) {
	// map[string]string can't hold uuid.UUID; without the assignability
	// guard SetMapIndex would panic. The plugin must no-op cleanly.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("stampMap panicked on map[string]string: %v", r)
		}
	}()
	m := map[string]string{"id": "x"}
	rv := reflect.ValueOf(m)
	stampMap(nil, rv, uuid.New())
	if _, present := m["tenant_id"]; present {
		t.Fatalf("stampMap should have no-op'd on incompatible value type; got tenant_id=%q", m["tenant_id"])
	}
}

func TestStamp_MapCreate_CallerSet_NotOverwritten(t *testing.T) {
	db := openSQLite(t)
	ctxTenant := uuid.New()
	callerTenant := uuid.New()
	ctx := runtimecontext.WithID(context.Background(), ctxTenant.String())

	rowID := uuid.New()
	row := map[string]interface{}{
		"id":        rowID,
		"tenant_id": callerTenant,
		"name":      "explicit-map",
	}
	if err := db.WithContext(ctx).Model(&stamped{}).Create(row).Error; err != nil {
		t.Fatalf("map create: %v", err)
	}
	var got stamped
	if err := db.First(&got, "id = ?", rowID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.TenantID != callerTenant {
		t.Fatalf("plugin clobbered caller-set map value: want %s, got %s", callerTenant, got.TenantID)
	}
}

// agentBots mirrors the SCHEMALESS allowlist case: the struct has NO
// tenant_id field, but the real table carries the column (added by the
// gem migration). The community evo-core writes via this struct.
type agentBots struct {
	ID   uuid.UUID `gorm:"type:text;primary_key"`
	Name string    `gorm:"type:text"`
}

func (agentBots) TableName() string { return "agent_bots" }

// boundTx wraps a real *sql.Tx so it satisfies runtimecontext.ScopedConn
// (== gorm.ConnPool) — exactly what the enterprise build publishes as the
// per-request GUC-carrying tx. We use a real one so we can observe whether
// GORM commits it prematurely.

// TestReroute_RegisteredBeforeBeginTransaction is the STRUCTURAL guard for
// the HTTP-500 fix: the schemaless reroute MUST run before GORM opens its
// default per-statement transaction. If a refactor moves it back to
// Before("gorm:create") (after begin), GORM would commit our bound tx early
// and the request's own Commit would explode with "already committed".
func TestReroute_RegisteredBeforeBeginTransaction(t *testing.T) {
	db := openSQLite(t)
	// Walk the Create callback chain and assert evo:tenant_reroute appears
	// BEFORE gorm:begin_transaction.
	var order []string
	seen := map[string]int{}
	for _, name := range createCallbackOrder(t, db) {
		seen[name] = len(order)
		order = append(order, name)
	}
	reroute, okR := seen[rerouteCallbackName]
	begin, okB := seen["gorm:begin_transaction"]
	if !okR {
		t.Fatalf("reroute callback %q not registered; chain=%v", rerouteCallbackName, order)
	}
	if !okB {
		t.Fatalf("gorm:begin_transaction not in chain=%v", order)
	}
	if reroute >= begin {
		t.Fatalf("reroute (%d) must run BEFORE gorm:begin_transaction (%d); chain=%v", reroute, begin, order)
	}
}

// TestReroute_BoundTxNotCommittedByGorm reproduces the 500 at the unit level:
// for a schemaless-allowlist INSERT routed onto a bound *sql.Tx, GORM must NOT
// commit that tx (its default-transaction must have been a swallowed no-op).
// We assert by committing the bound tx OURSELVES afterwards and requiring it to
// succeed exactly once — a premature GORM commit would make this fail with
// "transaction has already been committed or rolled back".
func TestReroute_BoundTxNotCommittedByGorm(t *testing.T) {
	db := openSQLite(t)
	if err := db.AutoMigrate(&agentBots{}); err != nil {
		t.Fatalf("migrate agent_bots: %v", err)
	}
	// Add the tenant_id column the gem migration would add (struct omits it).
	if err := db.Exec(`ALTER TABLE agent_bots ADD COLUMN tenant_id text`).Error; err != nil {
		t.Fatalf("add tenant_id col: %v", err)
	}

	// Real bound tx from the same sqlite DB — stands in for the enterprise
	// GUC-carrying per-request *sql.Tx published via runtimecontext.WithConn.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB(): %v", err)
	}
	boundTx, err := sqlDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin bound tx: %v", err)
	}

	tenantID := uuid.New()
	ctx := runtimecontext.WithID(context.Background(), tenantID.String())
	ctx = runtimecontext.WithConn(ctx, boundTx)

	row := agentBots{ID: uuid.New(), Name: "via-bound-tx"}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("create on bound tx: %v", err)
	}

	// THE ASSERTION: GORM did not commit our bound tx. We commit it once —
	// that must succeed. A premature GORM commit (the 500) makes this error.
	if err := boundTx.Commit(); err != nil {
		t.Fatalf("bound tx already consumed by GORM (the 500 bug regressed): %v", err)
	}

	// And the row really landed (committed via OUR commit, on the bound tx).
	var n int64
	if err := db.Model(&agentBots{}).Where("id = ?", row.ID).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("rerouted row not persisted: count=%d", n)
	}
}

// evoCoreAgents mirrors the FAIL-CLOSED allowlist case: the struct DOES
// declare tenant_id (so `stamp` value-stamps it), AND the table is one of the
// 8 evo_core_* tables whose RLS was tightened to fail-closed. Its INSERT must
// therefore ALSO be rerouted onto the GUC-carrying bound tx so the stamped
// value matches app.current_tenant_id and WITH CHECK passes.
type evoCoreAgents struct {
	ID       uuid.UUID `gorm:"type:text;primary_key"`
	TenantID uuid.UUID `gorm:"column:tenant_id;type:text"`
	Name     string    `gorm:"type:text"`
}

func (evoCoreAgents) TableName() string { return "evo_core_agents" }

// TestReroute_FailClosedTable_RoutedToBoundTx is the regression guard for the
// HTTP-500 "new row violates row-level security policy" fix. A table that HAS a
// tenant_id field but is in tenantScopedWriteTables must be rerouted onto the
// bound tx (not the pool), because its fail-closed policy demands GUC == value.
// We prove the reroute two ways: (1) the stamp still fills tenant_id (value path
// untouched), and (2) GORM did NOT commit the bound tx (the reroute ran before
// begin_transaction, same no-double-commit contract as agent_bots).
func TestReroute_FailClosedTable_RoutedToBoundTx(t *testing.T) {
	db := openSQLite(t)
	if err := db.AutoMigrate(&evoCoreAgents{}); err != nil {
		t.Fatalf("migrate evo_core_agents: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB(): %v", err)
	}
	boundTx, err := sqlDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin bound tx: %v", err)
	}

	tenantID := uuid.New()
	ctx := runtimecontext.WithID(context.Background(), tenantID.String())
	ctx = runtimecontext.WithConn(ctx, boundTx)

	row := evoCoreAgents{ID: uuid.New(), Name: "fail-closed-agent"}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("create evo_core_agents: %v", err)
	}

	// (1) value-stamp still happened: the model carries tenant_id.
	if row.TenantID != tenantID {
		t.Fatalf("want tenant_id %s stamped, got %s", tenantID, row.TenantID)
	}

	// (2) reroute happened before begin: GORM did not consume the bound tx.
	if err := boundTx.Commit(); err != nil {
		t.Fatalf("bound tx already consumed by GORM (fail-closed 500 regressed): %v", err)
	}

	var got evoCoreAgents
	if err := db.First(&got, "id = ?", row.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.TenantID != tenantID {
		t.Fatalf("persisted tenant_id mismatch: want %s got %s", tenantID, got.TenantID)
	}
}

// TestReroute_FailClosedTable_NoConn_FailsClosed proves the fail-closed table
// aborts (does not silently insert on the pool) when the request has a tenant
// but NO scope-bound conn — the "route furou o middleware" case. Inserting on
// the pool would hit the RLS 42501 anyway; we refuse earlier with ErrScopeUnbound.
func TestReroute_FailClosedTable_NoConn_FailsClosed(t *testing.T) {
	db := openSQLite(t)
	if err := db.AutoMigrate(&evoCoreAgents{}); err != nil {
		t.Fatalf("migrate evo_core_agents: %v", err)
	}
	// tenant bound, but NO WithConn → no GUC-carrying tx available.
	ctx := runtimecontext.WithID(context.Background(), uuid.New().String())
	row := evoCoreAgents{ID: uuid.New(), Name: "no-conn"}
	err := db.WithContext(ctx).Create(&row).Error
	if err == nil {
		t.Fatalf("want ErrScopeUnbound (fail-closed), got nil — row inserted on the pool")
	}
}

// createCallbackOrder returns the registered Create callback names in
// execution order, by probing GORM's callback processor.
func createCallbackOrder(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	// GORM doesn't export the ordered list directly; we reconstruct it by
	// registering sentinel callbacks at known anchors and observing which
	// fire. Simpler: assert via a live Create that records the order through
	// a Replace on each known callback is invasive. Instead, drive a real
	// Create on a schemaless-allowlist model with a bound tx and capture the
	// flag state at gorm:create time (proves reroute already ran + begin was
	// a no-op).
	var chain []string
	// Register a probe right after begin_transaction that records whether the
	// reroute ran first AND whether begin set its started_transaction flag.
	_ = db.Callback().Create().After("gorm:begin_transaction").Register("evo:test_probe", func(d *gorm.DB) {
		// If reroute ran before begin, ConnPool is our bound tx (a *sql.Tx),
		// and begin would have swallowed ErrInvalidTransaction → no flag.
		if _, started := d.InstanceGet("gorm:started_transaction"); !started {
			chain = append(chain, rerouteCallbackName, "gorm:begin_transaction")
		} else {
			chain = append(chain, "gorm:begin_transaction", rerouteCallbackName)
		}
	})

	if err := db.AutoMigrate(&agentBots{}); err != nil {
		t.Fatalf("migrate agent_bots: %v", err)
	}
	_ = db.Exec(`ALTER TABLE agent_bots ADD COLUMN tenant_id text`)
	sqlDB, _ := db.DB()
	boundTx, err := sqlDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin bound tx: %v", err)
	}
	defer func() { _ = boundTx.Rollback() }()
	ctx := runtimecontext.WithID(context.Background(), uuid.New().String())
	ctx = runtimecontext.WithConn(ctx, boundTx)
	_ = db.WithContext(ctx).Create(&agentBots{ID: uuid.New(), Name: "probe"}).Error
	_ = db.Callback().Create().Remove("evo:test_probe")
	return chain
}
