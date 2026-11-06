//go:build integration

// A failed bot write on a request-bound transaction must not take the agent with
// it. Postgres aborts the whole transaction on a failed statement, so this runs
// on a real one: the repositories write on the transaction the way the enterprise
// build routes them, and the agent has to survive the commit.
//
// Run with:
//
//	EVO_TENANT_TEST_DATABASE_URL=postgres://evo_app:evo_app_pwd@localhost:5442/evo_community?sslmode=disable \
//	go test -tags=integration ./pkg/agent/service/...
package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/repository"
	"evo-ai-core-service/pkg/agent/service/processor"
	"evo-ai-core-service/pkg/evoextensions/runtimecontext"
	mcpmodel "evo-ai-core-service/pkg/mcp_server/model"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	botSavepointTestSchema = "bot_savepoint_test"
	refusedBotName         = "bot-write-refused"
)

func provisionBotSavepointSchema(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(envBackfillDatabaseURL)
	if dsn == "" {
		t.Skipf("%s not set — skipping integration", envBackfillDatabaseURL)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open pgx: %v", err)
	}

	stmts := []string{
		fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", botSavepointTestSchema),
		fmt.Sprintf("CREATE SCHEMA %s", botSavepointTestSchema),
		fmt.Sprintf(`CREATE TABLE %s.evo_core_agents (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			name varchar(255) NOT NULL, description text, type varchar(10) NOT NULL,
			model varchar(255), api_key_id uuid, instruction text, card_url varchar(1024),
			folder_id uuid, config json DEFAULT '{}', role text, goal text,
			evolution_bot_id uuid, evolution_bot_sync boolean DEFAULT false,
			created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL)`, botSavepointTestSchema),
		fmt.Sprintf(`CREATE TABLE %s.agent_bots (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			name varchar(255), description varchar(255), outgoing_url varchar(1024),
			bot_type integer DEFAULT 0, bot_config jsonb DEFAULT '{}', api_key varchar(1000),
			bot_provider varchar(255) NOT NULL DEFAULT 'webhook', message_signature text,
			text_segmentation_enabled boolean NOT NULL DEFAULT false,
			text_segmentation_limit integer DEFAULT 300, text_segmentation_min_size integer DEFAULT 50,
			delay_per_character decimal(8,2) DEFAULT 50.0, debounce_time integer NOT NULL DEFAULT 5,
			created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
			CONSTRAINT bot_write_refused CHECK (name <> '%s'))`, botSavepointTestSchema, refusedBotName),
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("provisioning %s: %v", botSavepointTestSchema, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", botSavepointTestSchema))
		_ = db.Close()
	})
	return db
}

// boundRequest opens the transaction a request would run on and a service whose
// repositories write on it.
func boundRequest(t *testing.T, db *sql.DB) (context.Context, *sql.Tx, *agentService) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.Exec("SET LOCAL search_path TO " + botSavepointTestSchema); err != nil {
		t.Fatalf("search_path: %v", err)
	}

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: tx}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm on the bound tx: %v", err)
	}

	ctx := runtimecontext.WithConn(runtimecontext.WithID(context.Background(), uuid.NewString()), tx)
	svc := &agentService{
		agentRepository:  repository.NewAgentRepository(gormDB),
		evolutionService: NewEvolutionService(repository.NewAgentBotRepository(gormDB)),
		configProcessor: processor.NewConfigProcessor(
			func() string { return "generated-key" },
			func(_ context.Context, _ uuid.UUID) (*mcpmodel.McpServer, error) { return nil, nil },
		),
		aiProcessorURL: "http://processor.test",
	}
	return ctx, tx, svc
}

func agentRow(t *testing.T, db *sql.DB, id uuid.UUID) (name string, botSync bool) {
	t.Helper()
	q := fmt.Sprintf("SELECT name, evolution_bot_sync FROM %s.evo_core_agents WHERE id = $1", botSavepointTestSchema)
	if err := db.QueryRow(q, id).Scan(&name, &botSync); err != nil {
		t.Fatalf("agent %s after commit: %v", id, err)
	}
	return name, botSync
}

func TestCreate_BotFailureOnBoundTxKeepsTheAgent(t *testing.T) {
	db := provisionBotSavepointSchema(t)
	ctx, tx, svc := boundRequest(t, db)

	agent, err := svc.Create(ctx, model.Agent{Name: refusedBotName, Type: model.AgentTypeLLM, Model: "openai/gpt-4o-mini"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if agent.EvolutionBotSync {
		t.Fatal("the refused bot was reported as synced")
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("the request transaction did not commit after a 201: %v", err)
	}

	if _, botSync := agentRow(t, db, agent.ID); botSync {
		t.Error("stored agent is marked as synced to a bot that was refused")
	}
}

func TestUpdate_BotFailureOnBoundTxKeepsTheEdit(t *testing.T) {
	db := provisionBotSavepointSchema(t)

	ctx, tx, svc := boundRequest(t, db)
	created, err := svc.Create(ctx, model.Agent{Name: "support", Type: model.AgentTypeLLM, Model: "openai/gpt-4o-mini"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !created.EvolutionBotSync {
		t.Fatal("setup: the bot of the first agent was not created")
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("setup commit: %v", err)
	}

	ctx, tx, svc = boundRequest(t, db)
	if _, err := svc.Update(ctx, &model.Agent{Name: refusedBotName, Type: model.AgentTypeLLM, Model: "openai/gpt-4o-mini"}, created.ID); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("the request transaction did not commit after a 200: %v", err)
	}

	if name, _ := agentRow(t, db, created.ID); name != refusedBotName {
		t.Errorf("the edit was lost with the bot failure: name = %q", name)
	}
}
