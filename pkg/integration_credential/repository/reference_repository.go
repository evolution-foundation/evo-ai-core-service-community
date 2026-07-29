package repository

import (
	"context"

	"github.com/google/uuid"

	"evo-ai-core-service/pkg/integration_credential/model"

	"gorm.io/gorm"
)

// referenceRepository reads every consumer that points at a vault credential.
//
// ⚠️ DECISÃO DE FRONTEIRA, e ela reverte uma escrita anterior deste mesmo
// pacote. O comentário do `migrationStateRepository` dizia que `agent_bots`
// vive no schema do CRM e que este serviço não o lê. Aqui ele passa a ler.
//
// O motivo: `evo_community` é UM banco só, compartilhado pelos dois serviços, e
// as alternativas são piores. Go chamando o Rails por HTTP reintroduz o problema
// de bearer que moldou a 1.2 (esta consulta roda sem usuário logado), e o Rails
// escrevendo uma coluna de agregação duplicaria estado que já existe. O que a
// story 2.5 decidiu continua valendo para ESTADO DE MIGRAÇÃO, onde a resposta
// depende de regra de negócio do CRM; aqui é leitura de fato consumada.
//
// Cada store é lido numa consulta agrupada, nunca por credencial: a tela lista
// N credenciais, e perguntar por credencial seriam 5N idas ao banco.
type referenceRepository struct {
	db *gorm.DB
}

func NewReferenceRepository(db *gorm.DB) *referenceRepository { //nolint:revive // adapter consumed through the service interface
	return &referenceRepository{db: db}
}

func (r *referenceRepository) ReferencesByCredential(ctx context.Context) ([]model.CredentialReference, error) {
	references := make([]model.CredentialReference, 0)

	for _, query := range r.queries() {
		rows, err := r.scan(ctx, query)
		if err != nil {
			// A missing table means the store is not deployed here, not a
			// failure: the core may run against a database whose CRM half is
			// older, and the other stores still have useful answers.
			continue
		}
		references = append(references, rows...)
	}

	return references, nil
}

type referenceQuery struct {
	table string
	sql   string
}

// Each query yields (credential_id, label). The label is a display string, and
// for the CRM-owned store it is deliberately NOT the bot name: `agent_bots` has
// no tenant column, so echoing names could disclose across tenants.
func (r *referenceRepository) queries() []referenceQuery {
	return []referenceQuery{
		{
			table: "evo_core_agent_integrations",
			sql: `SELECT (config ->> 'credential_id') AS credential_id,
				CONCAT('Integração ', provider) AS label
				FROM evo_core_agent_integrations
				WHERE config ->> 'credential_id' IS NOT NULL`,
		},
		{
			table: "evo_core_custom_tools",
			sql: `SELECT refs.value AS credential_id,
				CONCAT('Ferramenta ', t.name, ' [', refs.key, ']') AS label
				FROM evo_core_custom_tools t,
				LATERAL jsonb_each_text(t.credential_refs) AS refs(key, value)
				WHERE t.credential_refs <> '{}'::jsonb`,
		},
		{
			table: "evo_core_custom_mcp_servers",
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
			table: "evo_core_agents",
			sql: `SELECT refs.value AS credential_id,
				CONCAT('Agente ', a.name, ' [', refs.key, ']') AS label
				FROM evo_core_agents a,
				LATERAL jsonb_each_text((a.config -> 'credential_refs')::jsonb) AS refs(key, value)
				WHERE a.config -> 'credential_refs' IS NOT NULL`,
		},
		{
			table: "agent_bots",
			sql: `SELECT credential_id::text AS credential_id,
				CONCAT('Bot de canal (', bot_provider, ')') AS label
				FROM agent_bots
				WHERE credential_id IS NOT NULL`,
		},
	}
}

func (r *referenceRepository) scan(ctx context.Context, query referenceQuery) ([]model.CredentialReference, error) {
	if !r.db.Migrator().HasTable(query.table) {
		return nil, gorm.ErrRecordNotFound
	}

	var rows []struct {
		CredentialID string
		Label        string
	}

	if err := r.db.WithContext(ctx).Raw(query.sql).Scan(&rows).Error; err != nil {
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
