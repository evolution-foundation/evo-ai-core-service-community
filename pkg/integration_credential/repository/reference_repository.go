package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"evo-ai-core-service/pkg/integration_credential/model"

	"gorm.io/gorm"
)

// referenceRepository reads every consumer that points at a vault credential.
// // It reads `agent_bots` even though the CRM owns that table: the database is
// shared, this query runs with no logged-in user (so HTTP to Rails hits the
// bearer problem), and an aggregation column would duplicate existing state.
// Migration state still belongs to the CRM — that answer depends on its rules,
// this one is settled fact.
// // It serves two callers with OPPOSITE postures on failure, hence two methods:
// the listing decorates and tolerates, the delete guard refuses and must not.
type referenceRepository struct {
	db *gorm.DB
}

func NewReferenceRepository(db *gorm.DB) *referenceRepository { //nolint:revive // adapter consumed through the service interface
	return &referenceRepository{db: db}
}

// ReferencesByCredential feeds the listing's `referenced_by`. A store that
// fails is DROPPED, not propagated: the column is decoration and the other
// stores still have useful answers.
func (r *referenceRepository) ReferencesByCredential(ctx context.Context) ([]model.CredentialReference, error) {
	references := make([]model.CredentialReference, 0)

	for _, query := range r.queries() {
		if !r.deployed(query) {
			continue
		}

		rows, err := r.scan(ctx, query.sql)
		if err != nil {
			continue
		}
		references = append(references, rows...)
	}

	return references, nil
}

// ReferencesForCredential feeds the delete guard, so it FAILS CLOSED: a store
// that errors aborts the answer instead of reporting "nobody uses it" and
// letting a hard delete leave a dangling jsonb reference. Only a store that is
// genuinely not deployed here is skipped, and that is decided by the catalog,
// never by a failed query.
func (r *referenceRepository) ReferencesForCredential(ctx context.Context, id uuid.UUID) ([]model.CredentialReference, error) {
	references := make([]model.CredentialReference, 0)

	for _, query := range r.queries() {
		if !r.deployed(query) {
			continue
		}

		rows, err := r.scan(ctx, filterByCredential(query.sql), id.String())
		if err != nil {
			return nil, fmt.Errorf("reading consumers in %s: %w", query.table, err)
		}
		references = append(references, rows...)
	}

	return references, nil
}

type referenceQuery struct {
	table string
	// columns the sql reads. A store may exist at an older version of the
	// shared database, so the catalog decides whether the query can run at all
	// — `HasTable` alone let a missing column reach the guard as a query error.
	columns []string
	sql     string
}

// filterByCredential narrows a store's query to one credential. Wrapping keeps
// a single definition per store: every query already yields credential_id as
// text, so the predicate applies uniformly.
func filterByCredential(sql string) string {
	return fmt.Sprintf(`SELECT refs.credential_id, refs.label FROM (%s) AS refs WHERE refs.credential_id = ?`, sql)
}

// Each query yields (credential_id, label). The label is a display string, and
// for the CRM-owned store it is deliberately NOT the bot name: `agent_bots` has
// no tenant column, so echoing names could disclose across tenants.
func (r *referenceRepository) queries() []referenceQuery {
	return []referenceQuery{
		{
			table:   "evo_core_agent_integrations",
			columns: []string{"config", "provider"},
			sql: `SELECT (config ->> 'credential_id') AS credential_id,
				CONCAT('Integração ', provider) AS label
				FROM evo_core_agent_integrations
				WHERE config ->> 'credential_id' IS NOT NULL`,
		},
		{
			table:   "evo_core_custom_tools",
			columns: []string{"credential_refs", "name"},
			sql: `SELECT refs.value AS credential_id,
				CONCAT('Ferramenta ', t.name, ' [', refs.key, ']') AS label
				FROM evo_core_custom_tools t,
				LATERAL jsonb_each_text(t.credential_refs) AS refs(key, value)
				WHERE t.credential_refs <> '{}'::jsonb`,
		},
		{
			table:   "evo_core_custom_mcp_servers",
			columns: []string{"credential_refs", "name"},
			sql: `SELECT refs.value AS credential_id,
				CONCAT('MCP ', m.name, ' [', refs.key, ']') AS label
				FROM evo_core_custom_mcp_servers m,
				LATERAL jsonb_each_text(m.credential_refs) AS refs(key, value)
				WHERE m.credential_refs <> '{}'::jsonb`,
		},
		{
			// Official MCP servers reference their env vars from the AGENT's
			// config, not from the catalog row: story 2.4 put the vault on the
			// agent end, and `evo_core_mcp_servers.environments` is a schema of
			// required keys, never a value.
			table:   "evo_core_agents",
			columns: []string{"config", "name"},
			sql: `SELECT refs.value AS credential_id,
				CONCAT('Agente ', a.name, ' [', refs.key, ']') AS label
				FROM evo_core_agents a,
				LATERAL jsonb_each_text((a.config -> 'credential_refs')::jsonb) AS refs(key, value)
				WHERE a.config -> 'credential_refs' IS NOT NULL`,
		},
		{
			table:   "agent_bots",
			columns: []string{"credential_id", "bot_provider"},
			sql: `SELECT credential_id::text AS credential_id,
				CONCAT('Bot de canal (', bot_provider, ')') AS label
				FROM agent_bots
				WHERE credential_id IS NOT NULL`,
		},
	}
}

// deployed answers whether the store exists here at a version this query can
// read. The core may run against a database whose CRM half is older, which is
// a legitimate absence — not a failure to report.
func (r *referenceRepository) deployed(query referenceQuery) bool {
	migrator := r.db.Migrator()
	if !migrator.HasTable(query.table) {
		return false
	}

	for _, column := range query.columns {
		if !migrator.HasColumn(query.table, column) {
			return false
		}
	}

	return true
}

func (r *referenceRepository) scan(ctx context.Context, sql string, args ...interface{}) ([]model.CredentialReference, error) {
	var rows []struct {
		CredentialID string
		Label        string
	}

	if err := r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	references := make([]model.CredentialReference, 0, len(rows))
	for _, row := range rows {
		id, err := parseUUID(row.CredentialID)
		if err != nil {
			continue
		}
		references = append(references, model.CredentialReference{CredentialID: id, Label: row.Label})
	}

	return references, nil
}

func parseUUID(value string) (uuid.UUID, error) {
	return uuid.Parse(value)
}
