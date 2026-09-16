//go:build integration

// Integration test that RUNS the five consumer queries against Postgres, at a
// volume where a wrong join, a leaked row or a dropped store shows up.
// It builds replicas of the stores in a throwaway schema, seeds them with
// consumers of every kind plus rows the queries must ignore, and checks both
// readers against what was seeded and against the pt-BR strings the queries
// used to render in SQL.
//
// Run with:
//
//	EVO_TENANT_TEST_DATABASE_URL=postgres://evo_app:evo_app_pwd@localhost:5442/evo_community?sslmode=disable \
//	go test -tags=integration -run TestReferenceQueries ./pkg/integration_credential/repository/...
package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"evo-ai-core-service/pkg/integration_credential/model"
	"evo-ai-core-service/pkg/integration_credential/repository"
	"evo-ai-core-service/pkg/integration_credential/service"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	envReferenceDatabaseURL = "EVO_TENANT_TEST_DATABASE_URL"
	referenceTestSchema     = "credential_holders_it"
)

// The column types match the shared database: the agent config is `json`, not
// `jsonb`, which is why its query casts before expanding.
var referenceStoreDDL = []string{
	`CREATE TABLE evo_core_agent_integrations (
		id serial PRIMARY KEY, provider varchar NOT NULL, config jsonb)`,
	`CREATE TABLE evo_core_custom_tools (
		id serial PRIMARY KEY, name varchar(255) NOT NULL, credential_refs jsonb NOT NULL DEFAULT '{}'::jsonb)`,
	`CREATE TABLE evo_core_custom_mcp_servers (
		id serial PRIMARY KEY, name varchar NOT NULL, credential_refs jsonb NOT NULL DEFAULT '{}'::jsonb)`,
	`CREATE TABLE evo_core_agents (
		id serial PRIMARY KEY, name varchar(255) NOT NULL, config json DEFAULT '{}'::json)`,
	`CREATE TABLE agent_bots (
		id serial PRIMARY KEY, bot_provider varchar NOT NULL DEFAULT 'webhook', credential_id uuid)`,
}

// The queries as they rendered labels in SQL before the consumers were
// structured, run the way the repository ran them: a catalog check per store,
// then one query per store. It proves the listing and the 409 still say the
// same thing, and gives the timings a baseline that does the same work.
var labelsRenderedInSQL = []struct {
	table   string
	columns []string
	sql     string
}{
	{"evo_core_agent_integrations", []string{"config", "provider"}, `SELECT (config ->> 'credential_id') AS credential_id,
		CONCAT('Integração ', provider) AS label
		FROM evo_core_agent_integrations WHERE config ->> 'credential_id' IS NOT NULL`},
	{"evo_core_custom_tools", []string{"credential_refs", "name"}, `SELECT refs.value AS credential_id,
		CONCAT('Ferramenta ', t.name, ' [', refs.key, ']') AS label
		FROM evo_core_custom_tools t, LATERAL jsonb_each_text(t.credential_refs) AS refs(key, value)
		WHERE t.credential_refs <> '{}'::jsonb`},
	{"evo_core_custom_mcp_servers", []string{"credential_refs", "name"}, `SELECT refs.value AS credential_id,
		CONCAT('MCP ', m.name, ' [', refs.key, ']') AS label
		FROM evo_core_custom_mcp_servers m, LATERAL jsonb_each_text(m.credential_refs) AS refs(key, value)
		WHERE m.credential_refs <> '{}'::jsonb`},
	{"evo_core_agents", []string{"config", "name"}, `SELECT refs.value AS credential_id,
		CONCAT('Agente ', a.name, ' [', refs.key, ']') AS label
		FROM evo_core_agents a, LATERAL jsonb_each_text((a.config -> 'credential_refs')::jsonb) AS refs(key, value)
		WHERE a.config -> 'credential_refs' IS NOT NULL`},
	{"agent_bots", []string{"credential_id", "bot_provider"}, `SELECT credential_id::text AS credential_id,
		CONCAT('Bot de canal (', bot_provider, ')') AS label
		FROM agent_bots WHERE credential_id IS NOT NULL`},
}

// renderedLabels reads the labels as the SQL rendered them, for every
// credential or, given an id, for that one.
func renderedLabels(t *testing.T, db *gorm.DB, id *uuid.UUID) map[uuid.UUID][]string {
	t.Helper()
	rendered := map[uuid.UUID][]string{}

	for _, store := range labelsRenderedInSQL {
		deployed := db.Migrator().HasTable(store.table)
		for _, column := range store.columns {
			deployed = deployed && db.Migrator().HasColumn(store.table, column)
		}
		if !deployed {
			continue
		}

		var rows []struct {
			CredentialID string
			Label        string
		}
		query, args := store.sql, []interface{}{}
		if id != nil {
			query = `SELECT refs.credential_id, refs.label FROM (` + store.sql + `) AS refs WHERE refs.credential_id = ?`
			args = append(args, id.String())
		}
		if err := db.Raw(query, args...).Scan(&rows).Error; err != nil {
			t.Fatalf("labels rendered in SQL (%s): %v", store.table, err)
		}
		for _, row := range rows {
			if parsed, err := uuid.Parse(row.CredentialID); err == nil {
				rendered[parsed] = append(rendered[parsed], row.Label)
			}
		}
	}

	return rendered
}

func openReferenceReplica(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv(envReferenceDatabaseURL)
	if dsn == "" {
		t.Skipf("%s not set — skipping integration", envReferenceDatabaseURL)
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open %s: %v", envReferenceDatabaseURL, err)
	}
	dropSchema := func() {
		if err := admin.Exec("DROP SCHEMA IF EXISTS " + referenceTestSchema + " CASCADE").Error; err != nil {
			t.Errorf("drop schema: %v", err)
		}
	}
	dropSchema()
	if err := admin.Exec("CREATE SCHEMA " + referenceTestSchema).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		dropSchema()
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	// Unqualified table names in the repository resolve to the replica only.
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s: %v", envReferenceDatabaseURL, err)
	}
	query := u.Query()
	query.Set("search_path", referenceTestSchema)
	u.RawQuery = query.Encode()

	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open replica: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil || schema != referenceTestSchema {
		t.Fatalf("replica connection resolves to schema %q (%v), want %q", schema, err, referenceTestSchema)
	}

	for _, ddl := range referenceStoreDDL {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create store: %v", err)
		}
	}

	return db
}

// seededHolders is what the stores were seeded with, per credential.
type seededHolders map[uuid.UUID][]model.CredentialConsumer

func (s seededHolders) add(id uuid.UUID, consumer model.CredentialConsumer) {
	s[id] = append(s[id], consumer)
}

// Names a label format could trip on: brackets, parentheses, commas, accents
// and characters outside the BMP.
var awkwardNames = []string{
	"Cobrança %d",
	"Busca [v%d], interna",
	"Zendesk (sandbox) %d",
	"Atendimento 🚀 %d",
	"  espaços  %d",
	"日本語 %d",
}

var awkwardKeys = []string{"Authorization", "X-Api-Key", "token", "api_key", "chave [v2]", "Ç, ñ"}

var providers = []string{"github", "hubspot", "whatsapp", "notion", "google_drive"}

// seedReferenceStores fills every store and returns the holders each credential
// must be reported with. A third of the references land on one hot credential,
// so a single delete guard answers for hundreds of consumers.
func seedReferenceStores(t *testing.T, db *gorm.DB, credentials []uuid.UUID) seededHolders {
	t.Helper()
	rng := rand.New(rand.NewSource(20260916))
	seeded := seededHolders{}
	hot := credentials[0]

	pick := func() uuid.UUID {
		if rng.Intn(3) == 0 {
			return hot
		}
		return credentials[rng.Intn(len(credentials))]
	}
	name := func(i int) string { return fmt.Sprintf(awkwardNames[i%len(awkwardNames)], i) }
	exec := func(sql string, args ...interface{}) {
		t.Helper()
		if err := db.Exec(sql, args...).Error; err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	// refs builds a credential_refs object with distinct keys, recording each.
	refs := func(kind, consumerName string) string {
		object := map[string]string{}
		for _, i := range rng.Perm(len(awkwardKeys))[:1+rng.Intn(4)] {
			id := pick()
			object[awkwardKeys[i]] = id.String()
			seeded.add(id, model.CredentialConsumer{Kind: kind, Name: consumerName, Key: awkwardKeys[i]})
		}
		encoded, err := json.Marshal(object)
		if err != nil {
			t.Fatalf("encode refs: %v", err)
		}
		return string(encoded)
	}

	for i := 0; i < 400; i++ {
		provider := providers[i%len(providers)]
		switch i % 5 {
		case 0, 1, 2:
			id := pick()
			exec(`INSERT INTO evo_core_agent_integrations (provider, config) VALUES (?, ?::jsonb)`,
				provider, fmt.Sprintf(`{"credential_id": %q, "scope": "x"}`, id))
			seeded.add(id, model.CredentialConsumer{Kind: model.ConsumerKindIntegration, Name: provider})
		case 3:
			exec(`INSERT INTO evo_core_agent_integrations (provider, config) VALUES (?, '{"scope": "x"}'::jsonb)`, provider)
		case 4:
			exec(`INSERT INTO evo_core_agent_integrations (provider, config) VALUES (?, NULL)`, provider)
		}
	}
	// A value that is not a credential id is read by the query and dropped.
	exec(`INSERT INTO evo_core_agent_integrations (provider, config) VALUES ('github', '{"credential_id": "not-a-uuid"}'::jsonb)`)

	for i := 0; i < 400; i++ {
		toolName := name(i)
		if i%7 == 0 {
			exec(`INSERT INTO evo_core_custom_tools (name) VALUES (?)`, toolName)
			continue
		}
		exec(`INSERT INTO evo_core_custom_tools (name, credential_refs) VALUES (?, ?::jsonb)`,
			toolName, refs(model.ConsumerKindTool, toolName))
	}
	exec(`INSERT INTO evo_core_custom_tools (name, credential_refs) VALUES ('Quebrada', '{"token": "not-a-uuid"}'::jsonb)`)

	for i := 0; i < 250; i++ {
		serverName := name(i + 1000)
		if i%6 == 0 {
			exec(`INSERT INTO evo_core_custom_mcp_servers (name) VALUES (?)`, serverName)
			continue
		}
		exec(`INSERT INTO evo_core_custom_mcp_servers (name, credential_refs) VALUES (?, ?::jsonb)`,
			serverName, refs(model.ConsumerKindMCP, serverName))
	}

	for i := 0; i < 500; i++ {
		agentName := name(i + 2000)
		switch i % 5 {
		case 0:
			exec(`INSERT INTO evo_core_agents (name, config) VALUES (?, '{}'::json)`, agentName)
		case 1:
			exec(`INSERT INTO evo_core_agents (name, config) VALUES (?, NULL)`, agentName)
		case 2:
			exec(`INSERT INTO evo_core_agents (name, config) VALUES (?, '{"credential_refs": {}}'::json)`, agentName)
		default:
			exec(`INSERT INTO evo_core_agents (name, config) VALUES (?, ?::json)`, agentName,
				fmt.Sprintf(`{"model": "x", "credential_refs": %s}`, refs(model.ConsumerKindAgent, agentName)))
		}
	}

	for i := 0; i < 300; i++ {
		provider := providers[i%len(providers)]
		if i%4 == 0 {
			exec(`INSERT INTO agent_bots (bot_provider) VALUES (?)`, provider)
			continue
		}
		id := pick()
		exec(`INSERT INTO agent_bots (bot_provider, credential_id) VALUES (?, ?)`, provider, id.String())
		seeded.add(id, model.CredentialConsumer{Kind: model.ConsumerKindChannelBot, Name: provider})
	}

	return seeded
}

// sortedAsServed orders consumers the way the service serves them.
func sortedAsServed(consumers []model.CredentialConsumer) []model.CredentialConsumer {
	sorted := append([]model.CredentialConsumer{}, consumers...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Label() < sorted[b].Label() })
	return sorted
}

func withoutKinds(consumers []model.CredentialConsumer, kinds ...string) []model.CredentialConsumer {
	kept := []model.CredentialConsumer{}
	for _, consumer := range consumers {
		dropped := false
		for _, kind := range kinds {
			dropped = dropped || consumer.Kind == kind
		}
		if !dropped {
			kept = append(kept, consumer)
		}
	}
	return kept
}

func sortedStrings(values []string) []string {
	sorted := append([]string{}, values...)
	sort.Strings(sorted)
	return sorted
}

func TestReferenceQueriesReportEveryHolderAtVolume(t *testing.T) {
	db := openReferenceReplica(t)
	ctx := context.Background()

	credentials := make([]uuid.UUID, 250)
	for i := range credentials {
		credentials[i] = uuid.New()
	}
	seeded := seedReferenceStores(t, db, credentials)
	// Credentials nobody references, including one never seeded anywhere.
	unused := []uuid.UUID{uuid.New(), uuid.New()}

	total := 0
	for _, consumers := range seeded {
		total += len(consumers)
	}
	hot := credentials[0]
	t.Logf("seeded %d holders across %d credentials; the hot credential has %d", total, len(seeded), len(seeded[hot]))
	if len(seeded[hot]) < 200 {
		t.Fatalf("the hot credential has %d holders, the seed is too small to mean anything", len(seeded[hot]))
	}

	reader := repository.NewReferenceRepository(db)
	references := service.NewReferenceIndex(reader)

	// Best of a few runs, so a cold connection does not stand in for the query.
	var index service.ReferenceIndex
	var sweep time.Duration
	for run := 0; run < 5; run++ {
		start := time.Now()
		built, err := references.Build(ctx)
		if elapsed := time.Since(start); run == 0 || elapsed < sweep {
			sweep = elapsed
		}
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		index = built
	}

	t.Run("the listing reports exactly the seeded holders of every credential", func(t *testing.T) {
		if len(index) != len(seeded) {
			t.Errorf("index has %d credentials, seeded %d — a row was dropped or invented", len(index), len(seeded))
		}
		for id, consumers := range seeded {
			if got, want := index.For(id), sortedAsServed(consumers); !reflect.DeepEqual(got, want) {
				t.Errorf("credential %s: listing holders differ\n got %d: %v\nwant %d: %v", id, len(got), got, len(want), want)
			}
		}
		for _, id := range unused {
			if got := index.For(id); got == nil || len(got) != 0 {
				t.Errorf("unused credential %s: holders = %#v, want an empty non-nil slice", id, got)
			}
		}
	})

	var guardTotal time.Duration
	t.Run("the delete guard reports exactly the seeded holders of every credential", func(t *testing.T) {
		for _, id := range append(append([]uuid.UUID{}, credentials...), unused...) {
			start := time.Now()
			got, err := references.ConsumersOf(ctx, id)
			guardTotal += time.Since(start)
			if err != nil {
				t.Fatalf("ConsumersOf(%s): %v", id, err)
			}
			if want := sortedAsServed(seeded[id]); !reflect.DeepEqual(got, want) {
				t.Errorf("credential %s: guard holders differ\n got %d: %v\nwant %d: %v", id, len(got), got, len(want), want)
			}
		}
	})

	t.Run("the pt-BR strings match what the queries rendered in SQL", func(t *testing.T) {
		var rendered map[uuid.UUID][]string
		var renderedSweep time.Duration
		for run := 0; run < 5; run++ {
			start := time.Now()
			rendered = renderedLabels(t, db, nil)
			if elapsed := time.Since(start); run == 0 || elapsed < renderedSweep {
				renderedSweep = elapsed
			}
		}
		t.Logf("listing sweep: structured %s, rendered in SQL %s", sweep, renderedSweep)

		if len(rendered) != len(index) {
			t.Errorf("SQL rendered labels for %d credentials, the listing has %d", len(rendered), len(index))
		}
		for id, labels := range rendered {
			if got, want := sortedStrings(index.LabelsFor(id)), sortedStrings(labels); !reflect.DeepEqual(got, want) {
				t.Errorf("credential %s: listing labels differ\n got %v\nwant %v", id, got, want)
			}
		}

		var renderedTotal time.Duration
		for _, id := range append(append([]uuid.UUID{}, credentials...), unused...) {
			start := time.Now()
			labels := renderedLabels(t, db, &id)[id]
			renderedTotal += time.Since(start)

			consumers, err := references.ConsumersOf(ctx, id)
			if err != nil {
				t.Fatalf("ConsumersOf(%s): %v", id, err)
			}
			if got, want := sortedStrings(service.Labels(consumers)), sortedStrings(labels); !reflect.DeepEqual(got, want) {
				t.Errorf("credential %s: 409 labels differ\n got %v\nwant %v", id, got, want)
			}
		}
		lookups := time.Duration(len(credentials) + len(unused))
		t.Logf("delete guard per credential: structured %s, rendered in SQL %s", guardTotal/lookups, renderedTotal/lookups)
	})

	t.Run("only tools, MCP servers and agents carry a key on the wire", func(t *testing.T) {
		encoded, err := json.Marshal(index.For(hot))
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var holders []map[string]interface{}
		if err := json.Unmarshal(encoded, &holders); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		seen := map[string]bool{}
		for _, holder := range holders {
			kind, _ := holder["kind"].(string)
			seen[kind] = true
			name, _ := holder["name"].(string)
			if strings.TrimSpace(name) == "" {
				t.Errorf("holder %v has no name", holder)
			}
			key, hasKey := holder["key"].(string)
			switch kind {
			case model.ConsumerKindIntegration, model.ConsumerKindChannelBot:
				if hasKey {
					t.Errorf("%s holder carries a key: %v", kind, holder)
				}
			case model.ConsumerKindTool, model.ConsumerKindMCP, model.ConsumerKindAgent:
				if !hasKey || key == "" {
					t.Errorf("%s holder has no key: %v", kind, holder)
				}
			default:
				t.Errorf("holder with a kind outside the vocabulary: %v", holder)
			}
		}
		if len(seen) != 5 {
			t.Errorf("the hot credential covers kinds %v, want all five", seen)
		}
	})

	t.Run("a store missing here is skipped by both readers", func(t *testing.T) {
		if err := db.Exec(`DROP TABLE evo_core_custom_mcp_servers`).Error; err != nil {
			t.Fatalf("drop store: %v", err)
		}
		if err := db.Exec(`ALTER TABLE agent_bots DROP COLUMN bot_provider`).Error; err != nil {
			t.Fatalf("drop column: %v", err)
		}

		index, err := references.Build(ctx)
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		for id, consumers := range seeded {
			want := sortedAsServed(withoutKinds(consumers, model.ConsumerKindMCP, model.ConsumerKindChannelBot))
			if got := index.For(id); !reflect.DeepEqual(got, want) {
				t.Errorf("credential %s: listing without the stores differs\n got %v\nwant %v", id, got, want)
			}

			got, err := references.ConsumersOf(ctx, id)
			if err != nil {
				t.Fatalf("guard failed on a store that is not deployed: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("credential %s: guard without the stores differs\n got %v\nwant %v", id, got, want)
			}
		}
	})
}
