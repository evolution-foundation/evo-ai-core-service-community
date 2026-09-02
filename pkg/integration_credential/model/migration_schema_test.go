package model

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const migrationPath = "../../../migrations/000019_create_integration_credentials_table.up.sql"

func readMigration(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	return string(content)
}

// No database in this suite, so uniqueness is proven against the DDL. The index
// must be (scope, name): the sibling core tables unique on name alone, which
// collides across tenants in the enterprise build.
func TestUniqueIsScopedNotNameAlone(t *testing.T) {
	ddl := readMigration(t)

	scopedUnique := regexp.MustCompile(`(?is)UNIQUE\s*\(\s*scope\s*,\s*name\s*\)`)
	if !scopedUnique.MatchString(ddl) {
		t.Error("migration does not declare UNIQUE (scope, name)")
	}

	// Negative proof: a UNIQUE on name alone, in a constraint or an index,
	// would silently reintroduce the cross-tenant collision.
	nameOnlyUnique := regexp.MustCompile(`(?is)UNIQUE\s*(INDEX\s+\S+\s+ON\s+\S+\s*)?\(\s*name\s*\)`)
	if nameOnlyUnique.MatchString(ddl) {
		t.Error("migration declares a UNIQUE on name alone; it collides across tenants in enterprise")
	}
}

// The coherence CHECK is what makes "the vault never holds an oauth token" a
// database guarantee rather than a handler convention.
func TestKindContentCheckIsDeclared(t *testing.T) {
	ddl := strings.ToLower(readMigration(t))

	for _, fragment := range []string{
		"kind = 'static' and value is not null",
		"kind = 'oauth' and value is null",
		"owner_store is not null and owner_ref is not null",
	} {
		if !strings.Contains(ddl, fragment) {
			t.Errorf("coherence CHECK is missing the %q branch", fragment)
		}
	}
}

func TestEnumChecksAreDeclared(t *testing.T) {
	ddl := strings.ToLower(readMigration(t))

	cases := []struct {
		column string
		values string
	}{
		{"kind", "('static', 'oauth')"},
		{"value_format", "('scalar', 'composite')"},
		{"scope", "('installation', 'account')"},
	}

	for _, tc := range cases {
		if !strings.Contains(ddl, tc.column+" in "+tc.values) {
			t.Errorf("missing CHECK for %s with values %s", tc.column, tc.values)
		}
	}
}

// imported_from ships in the create migration even though nothing writes it
// until Story 2.6. Story 1.5 needed a whole migration (000018) just to add the
// equivalent column to evo_core_api_keys; this avoids the repeat.
func TestImportedFromShipsInTheCreateMigration(t *testing.T) {
	if !strings.Contains(strings.ToLower(readMigration(t)), "imported_from varchar(128)") {
		t.Error("imported_from is missing, so Story 2.6 would need an ALTER TABLE")
	}
}

// The model and the DDL must agree on width: a struct declaring more than the
// column holds turns into a runtime "value too long" instead of a build error.
func TestModelWidthsMatchTheMigration(t *testing.T) {
	ddl := strings.ToLower(readMigration(t))

	cases := []struct {
		name   string
		column string
	}{
		{"provider", "provider varchar(100)"},
		{"kind", "kind varchar(16)"},
		{"value_format", "value_format varchar(16)"},
		{"value_hint", "value_hint varchar(8)"},
		{"scope", "scope varchar(32)"},
		{"owner_store", "owner_store varchar(64)"},
		{"owner_ref", "owner_ref varchar(128)"},
	}

	for _, tc := range cases {
		if !strings.Contains(ddl, tc.column) {
			t.Errorf("migration does not declare %q as the model expects", tc.column)
		}
	}
}
