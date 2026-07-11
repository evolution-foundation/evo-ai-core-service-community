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
	"context"
	"errors"
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

// ErrScopeUnbound is the fail-closed sentinel raised when a schemaless
// tenant-scoped table (allowlist below) is written with no scope-bound
// connection in context — refusing rather than inserting onto the pool
// (empty GUC → the column DEFAULT would read NULL → NOT NULL violation,
// or worse a row bound to no tenant).
var ErrScopeUnbound = errors.New("tenantstamp: schemaless tenant write with no bound connection")

// schemalessTenantTables: tabelas onde o evo-core ESCREVE via struct mas cujo
// struct community NÃO declara a coluna tenant_id (a migration enterprise do gem
// adicionou-a NOT NULL + RLS no MESMO Postgres do CRM). LookUpField(tenant_id)
// retorna nil → o stamp normal (preencher o VALOR no struct) não tem onde escrever.
//
// SOLUÇÃO (simétrica ao tenantscope dos reads): em vez de carimbar o valor, ROTEAMOS
// o INSERT para a tx GUC-carrying per-request (db.Statement.ConnPool = conn), onde o
// Authorizer enterprise já fez set_config('app.current_tenant_id', tid, is_local). Aí
// o DEFAULT da coluna (migration do gem: tenant_id DEFAULT current_setting(...)) lê o
// tenant correto da tx. struct-create intacto → bot.ID volta via RETURNING. O write
// normalmente roda no pool global com GUC vazio (só os reads eram roteados); isto o
// roteia para tabelas do allowlist. NUNCA tocar o struct community (decisão: tenant_id
// é eixo enterprise). agent_bots é o único caso hoje (os demais structs já declaram
// tenant_id e seguem o caminho de stamp normal).
//
// CUIDADO: estritamente allowlist — não re-rotear writes de outras tabelas (mudaria
// a conexão/atomicidade delas sem motivo).
var schemalessTenantTables = map[string]struct{}{
	"agent_bots": {},
}

// tenantScopedWriteTables: as 8 tabelas evo_core_* cuja RLS foi apertada para FAIL-CLOSED
// (migration 20260630000001 — sem o branch `OR GUC IS NULL`). Elas DECLARAM tenant_id no
// struct, então o `stamp` já carimba o VALOR — MAS a policy WITH CHECK exige que o
// tenant_id BATA com o GUC app.current_tenant_id da CONEXÃO. O struct-write roda no pool
// global (GUC vazio) → a policy fail-closed rejeita com 42501 ("new row violates RLS").
//
// FIX (o "paired change" que a migration exigia): rotear TAMBÉM o INSERT dessas 8 para a
// conexão scope-bound (com GUC setado pelo Authorizer), igual ao agent_bots. Aí o valor
// carimbado bate com o GUC → WITH CHECK passa. O reroute roda ANTES do begin_transaction
// (evita o double-commit 500). O stamp-por-valor continua (belt+suspenders). Sem este
// reroute, criar agente/api-key/folder/tool/mcp dava HTTP 500 "Database error".
//
// Fonte da lista: as tabelas com policy USING sem `IS NULL` (pg_policy) = as 8 apertadas.
var tenantScopedWriteTables = map[string]struct{}{
	"evo_core_agents":             {},
	"evo_core_api_keys":           {},
	"evo_core_folders":            {},
	"evo_core_folder_shares":      {},
	"evo_core_custom_tools":       {},
	"evo_core_mcp_servers":        {},
	"evo_core_custom_mcp_servers": {},
	"evo_core_agent_integrations": {},
}

// callbackName must be unique across registered Create callbacks.
const callbackName = "evo:tenant_stamp"

// rerouteCallbackName is the SEPARATE reroute-only callback for the
// schemaless allowlist. It MUST run Before("gorm:begin_transaction")
// (see routeSchemalessTenantWrite for why) — distinct from the value
// stamper, which runs Before("gorm:create").
const rerouteCallbackName = "evo:tenant_reroute"

// rerouteUpdateCallbackName / rerouteDeleteCallbackName são os reroutes
// SIMÉTRICOS ao de Create, mas nas chains de Update e Delete. Sem eles, um
// UPDATE/DELETE das 8 tabelas fail-closed rodava no pool global com GUC vazio:
// a policy USING (tenant_id = current_setting('app.current_tenant_id')) não
// casava nenhuma linha → `rows:0` SILENCIOSO (nem erro nem persistência). Era a
// raiz de "editar agente não salva" (custom_mcp_server_ids, name, instruction,
// model — TUDO congelava na config do CREATE). O hook é o MESMO do Create:
// Before("gorm:begin_transaction") (em GORM v1.30.0 tanto a Update quanto a
// Delete chain têm gorm:begin_transaction como PRIMEIRO callback), para rotear a
// ConnPool ANTES da auto-tx do GORM abrir no pool bare e evitar o double-commit
// 500 documentado em routeScopedTenantWrite. NÃO usar Before("gorm:update"):
// dispara DEPOIS do begin_transaction.
const rerouteUpdateCallbackName = "evo:tenant_reroute_update"
const rerouteDeleteCallbackName = "evo:tenant_reroute_delete"

// Plugin implements gorm.Plugin.
type Plugin struct{}

// Name returns the plugin identity used by GORM's plugin registry.
func (Plugin) Name() string { return callbackName }

// Initialize registers the reroute + value-stamp callbacks:
//
//  1. evo:tenant_reroute — Create.Before("gorm:begin_transaction"):
//     reroutes ConnPool onto the scope-bound tx BEFORE GORM's default
//     transaction begins, so GORM's auto-tx becomes a swallowed no-op
//     instead of committing our request-scoped tx early (see
//     routeScopedTenantWrite).
//  2. evo:tenant_reroute_update / evo:tenant_reroute_delete —
//     Update/Delete.Before("gorm:begin_transaction"): the SAME reroute
//     on the Update and Delete chains. Sem eles, UPDATE/DELETE das 8
//     tabelas fail-closed rodava no pool com GUC vazio e batia 0 linhas
//     silenciosamente (a edição de agente nunca persistia). O reroute é
//     table-based e agnóstico à operação, então o mesmo callback serve
//     Create/Update/Delete.
//  3. evo:tenant_stamp — Create.Before("gorm:create"): stamps the
//     tenant_id VALUE on models that declare the column (the normal
//     path). Só no Create: o UPDATE não re-carimba tenant_id (a linha já
//     tem o valor; o reroute basta para a policy WITH CHECK/USING casar).
//
// They are split so the normal value-stamp path keeps running at its
// proven position and each reroute fires at the only point where it is
// correct (before begin_transaction).
func (Plugin) Initialize(db *gorm.DB) error {
	if err := db.Callback().Create().Before("gorm:begin_transaction").Register(rerouteCallbackName, rerouteSchemaless); err != nil {
		return err
	}
	if err := db.Callback().Update().Before("gorm:begin_transaction").Register(rerouteUpdateCallbackName, rerouteSchemaless); err != nil {
		return err
	}
	if err := db.Callback().Delete().Before("gorm:begin_transaction").Register(rerouteDeleteCallbackName, rerouteSchemaless); err != nil {
		return err
	}
	return db.Callback().Create().Before("gorm:create").Register(callbackName, stamp)
}

// rerouteSchemaless is the reroute-only callback (Before begin_transaction).
// It reroutes the INSERT onto the request-scoped, GUC-carrying connection for
// TWO classes of table:
//
//  1. the schemaless allowlist (agent_bots) — a struct WITHOUT a tenant_id
//     field whose table HAS the column; the DB DEFAULT reads the GUC; and
//  2. the tenantScopedWriteTables (the 8 evo_core_* tables) — structs that DO
//     declare tenant_id (so `stamp` value-stamps them at gorm:create) but whose
//     RLS was tightened to FAIL-CLOSED. A fail-closed WITH CHECK requires the
//     stamped tenant_id to MATCH the connection's GUC, so the INSERT must run on
//     the GUC-carrying conn — not the bare pool — or Postgres rejects it (42501).
//
// For every other model this is a no-op (the value stamper in `stamp` handles
// those at gorm:create, on the pool, with a fail-OPEN policy that tolerates
// GUC-less writes). See routeScopedTenantWrite for the reroute mechanics.
func rerouteSchemaless(db *gorm.DB) {
	if db.Statement == nil || db.Statement.Schema == nil {
		return
	}
	ctx := db.Statement.Context
	if ctx == nil {
		return
	}
	routeScopedTenantWrite(db, ctx)
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

	ctx := db.Statement.Context
	if ctx == nil {
		return
	}

	field := db.Statement.Schema.LookUpField(columnName)
	if field == nil {
		// O struct não declara tenant_id. O reroute do allowlist schemaless
		// (agent_bots) NÃO é feito aqui — ele roda no callback dedicado
		// rerouteSchemaless, Before("gorm:begin_transaction"), porque precisa
		// rotear a ConnPool ANTES da tx default do GORM começar (ver
		// routeSchemalessTenantWrite). Aqui (Before gorm:create) só fazemos o
		// stamp por VALOR; sem coluna, não há o que carimbar → no-op.
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
			elem := reflect.Indirect(rv.Index(i))
			if elem.Kind() == reflect.Map {
				stampMap(db, elem, parsed)
				continue
			}
			setIfZero(db, field, elem, parsed)
		}
	case reflect.Struct:
		setIfZero(db, field, rv, parsed)
	case reflect.Map:
		stampMap(db, rv, parsed)
	}
}

// routeScopedTenantWrite roteia o INSERT de uma tabela dos allowlists para a
// conexão scope-bound publicada pela camada enterprise (runtimecontext.ConnFromContext)
// — a tx onde o Authorizer fez set_config('app.current_tenant_id', tid, is_local).
// São DOIS allowlists com propósitos distintos, mas o MESMO reroute serve ambos:
//
//   - schemalessTenantTables (agent_bots): o struct NÃO tem tenant_id, então quem
//     resolve o tenant é o DEFAULT da coluna (migration do gem), que lê o GUC. Sem a
//     conn com GUC, o DEFAULT viria NULL → NOT NULL violation / row órfã.
//   - tenantScopedWriteTables (os 8 evo_core_*): o struct TEM tenant_id e o `stamp`
//     já carimba o VALOR — mas a RLS foi apertada p/ FAIL-CLOSED (WITH CHECK exige
//     tenant_id = GUC da conn). No pool o GUC é vazio → 42501. Roteando p/ a conn
//     scope-bound, o valor carimbado bate com o GUC → WITH CHECK passa. (Este é o
//     "paired change" que a migration 20260630000001 exigia p/ não dar HTTP 500.)
//
// Nessa tx o struct-create segue intacto (RETURNING id popula X.ID). É o simétrico de
// WRITE do tenantscope (que roteia os reads). FAIL-CLOSED: tabela do allowlist sem
// tenant no ctx OU sem conn scope-bound → ABORTA (não insere no pool com GUC vazio, o
// que gravaria a row sem tenant, violaria NOT NULL, ou bateria na RLS). Tabelas fora
// dos allowlists: no-op (seguem o value-stamp path no pool, com policy fail-open).
//
// POR QUE Before("gorm:begin_transaction") E NÃO Before("gorm:create"):
// o GORM tem SkipDefaultTransaction=false, então envolve cada Create numa tx
// própria: gorm:begin_transaction (db.Begin() no pool → abre uma tx nova "gormTx",
// seta gorm:started_transaction) ... gorm:commit_or_rollback_transaction
// (db.Commit() na ConnPool atual). Se o reroute rodasse DEPOIS do begin (em
// gorm:create), a ConnPool no commit já seria a NOSSA tx scope-bound → o GORM
// daria Commit() nela cedo demais, e o release(true)→tx.Commit() do request
// estouraria "transaction has already been committed" (HTTP 500), além de vazar
// a gormTx órfã. Roteando ANTES do begin, a ConnPool já é a *sql.Tx scope-bound
// quando db.Begin() roda: *sql.Tx não satisfaz TxBeginner/ConnPoolBeginner, então
// Begin() cai no default→ErrInvalidTransaction, que o BeginTransaction ENGOLE
// (tx.Error=nil) e NÃO seta gorm:started_transaction → commit_or_rollback vira
// no-op. Sem tx órfã, sem commit prematuro: o request commita uma vez só, no
// release. (Verificado em gorm@v1.30.0: finisher_api.go DB.Begin switch +
// callbacks/transaction.go.)
func routeScopedTenantWrite(db *gorm.DB, ctx context.Context) {
	_, schemaless := schemalessTenantTables[db.Statement.Table]
	_, failClosed := tenantScopedWriteTables[db.Statement.Table]
	if !schemaless && !failClosed {
		return // fora dos allowlists → segue o value-stamp path no pool
	}
	// tenant precisa estar bound (igual o tenantscope): sem tenant → fail-closed.
	if runtimecontext.IDFromContext(ctx) == "" {
		_ = db.AddError(ErrScopeUnbound)
		return
	}
	conn, ok := runtimecontext.ConnFromContext(ctx)
	if !ok {
		// tenant bound mas a conn scope-bound não está no ctx (rota que furou o
		// middleware enterprise). Recusar em vez de inserir no pool (GUC vazio →
		// DEFAULT NULL → NOT NULL violation, ou row órfã).
		_ = db.AddError(ErrScopeUnbound)
		return
	}
	// Roteia ESTE INSERT para a tx GUC-carrying. O DEFAULT da coluna lê o GUC dela.
	db.Statement.ConnPool = conn
}

// setIfZero writes parsed into the tenant_id field of elem only when
// the field is at its zero value. field.ValueOf returns (value, isZero);
// we drop the value and branch on isZero so callers that explicitly
// pre-populate tenant_id (seeders, backfill jobs) are not clobbered.
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

// stampMap handles the map[string]interface{} Create path. GORM allows
// `db.Model(&X{}).Create(map[string]interface{}{...})` for ad-hoc
// inserts; the struct-based stamper above never sees those rows because
// ReflectValue.Kind() is reflect.Map. We mirror setIfZero's "don't
// clobber" rule: only set the key when it's absent or empty.
func stampMap(db *gorm.DB, m reflect.Value, parsed uuid.UUID) {
	if !m.IsValid() || m.IsNil() {
		return
	}
	if m.Type().Key().Kind() != reflect.String {
		return
	}
	// Guard against panic when the map's value type isn't interface{} and
	// isn't directly assignable from uuid.UUID (eg. map[string]string).
	// Such Create patterns are unusual but legal; we'd rather no-op than
	// crash the request.
	elemType := m.Type().Elem()
	if elemType.Kind() != reflect.Interface && !reflect.TypeOf(parsed).AssignableTo(elemType) {
		return
	}
	key := reflect.ValueOf(columnName)
	if existing := m.MapIndex(key); existing.IsValid() {
		v := reflect.ValueOf(existing.Interface())
		if v.IsValid() && !v.IsZero() {
			return
		}
	}
	m.SetMapIndex(key, reflect.ValueOf(parsed))
}
