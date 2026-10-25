package repository

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DryRun renders the statement without a connection, so this proves the SQL
// the repository emits without a live database.
func dryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		postgres.New(postgres.Config{DriverName: "pgx"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatalf("open dry-run db: %v", err)
	}
	return db
}

type capturedQuery struct {
	sql  string
	vars []interface{}
}

func captureQueries(t *testing.T, db *gorm.DB) *[]capturedQuery {
	t.Helper()
	var seen []capturedQuery
	record := func(tx *gorm.DB) {
		seen = append(seen, capturedQuery{sql: tx.Statement.SQL.String(), vars: tx.Statement.Vars})
	}
	if err := db.Callback().Query().After("gorm:query").Register("capture_query", record); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	return &seen
}

func TestListAndCountFilterByNameBeforePaginating(t *testing.T) {
	db := dryRunDB(t)
	seen := captureQueries(t, db)
	repo := &mcpServerRepository{db: db}

	if _, err := repo.List(context.Background(), 2, 10, "git"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := repo.Count(context.Background(), "git"); err != nil {
		t.Fatalf("count: %v", err)
	}

	if len(*seen) != 2 {
		t.Fatalf("expected the list and the count queries, got %v", *seen)
	}
	list, count := (*seen)[0], (*seen)[1]

	if !strings.Contains(list.sql, "WHERE name ILIKE $1 LIMIT $2 OFFSET $3") {
		t.Fatalf("list must filter before paginating, got %q", list.sql)
	}
	if len(list.vars) == 0 || list.vars[0] != "%git%" {
		t.Fatalf("list must bind the search as a substring, got %v", list.vars)
	}

	if !strings.HasPrefix(count.sql, "SELECT count(*)") || !strings.Contains(count.sql, "WHERE name ILIKE $1") {
		t.Fatalf("count must apply the same filter, got %q", count.sql)
	}
	if len(count.vars) != 1 || count.vars[0] != "%git%" {
		t.Fatalf("count must bind the search as a substring, got %v", count.vars)
	}
}

func TestListAndCountWithoutSearchHaveNoFilter(t *testing.T) {
	db := dryRunDB(t)
	seen := captureQueries(t, db)
	repo := &mcpServerRepository{db: db}

	if _, err := repo.List(context.Background(), 1, 20, ""); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := repo.Count(context.Background(), ""); err != nil {
		t.Fatalf("count: %v", err)
	}

	for _, q := range *seen {
		if strings.Contains(q.sql, "WHERE") {
			t.Fatalf("an empty search must not filter, got %q", q.sql)
		}
	}
}
