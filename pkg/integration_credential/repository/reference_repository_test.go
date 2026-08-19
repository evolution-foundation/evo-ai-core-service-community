package repository

import (
	"strings"
	"testing"
)

// The guard asks about ONE credential, so the predicate has to reach the
// database: the unwrapped query returns every consumer of every credential.
func TestFilterByCredentialNarrowsEveryStoreQuery(t *testing.T) {
	repo := &referenceRepository{}

	for _, query := range repo.queries() {
		filtered := filterByCredential(query.sql)

		if !strings.HasSuffix(filtered, "WHERE refs.credential_id = ?") {
			t.Errorf("%s: query is not narrowed by credential: %s", query.table, filtered)
		}
		if !strings.Contains(filtered, query.sql) {
			t.Errorf("%s: the store query was rewritten instead of wrapped", query.table)
		}
	}
}

// A store read at an older version of the shared database is a legitimate
// absence, and `deployed` decides it from the catalog. Every query must
// declare the columns it reads, or a missing one reaches the guard as a query
// error instead.
func TestEveryStoreQueryDeclaresTheColumnsItReads(t *testing.T) {
	repo := &referenceRepository{}

	for _, query := range repo.queries() {
		if len(query.columns) == 0 {
			t.Errorf("%s: declares no columns", query.table)
			continue
		}
		for _, column := range query.columns {
			if !strings.Contains(query.sql, column) {
				t.Errorf("%s: declares column %q it does not read", query.table, column)
			}
		}
	}
}
