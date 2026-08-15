package postgres

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// Schema errors must carry the Postgres identifier detail: a bare "Undefined
// column" gives an operator nothing to act on, and the pg message only names
// schema objects (never row data), so surfacing it is safe.
func TestMapDBError_UndefinedColumnCarriesPgDetail(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "42703", Message: `column "base_url" of relation "evo_core_api_keys" does not exist`}

	err := MapDBError(pgErr, nil)

	var dbErr *Error
	if !errors.As(err, &dbErr) {
		t.Fatalf("expected *postgres.Error, got %T", err)
	}
	if dbErr.Code != ERR_UNDEFINED_COLUMN {
		t.Fatalf("code = %s, want %s", dbErr.Code, ERR_UNDEFINED_COLUMN)
	}
	if dbErr.HTTPCode != http.StatusInternalServerError {
		t.Fatalf("http = %d, want 500", dbErr.HTTPCode)
	}
	want := `Undefined column: column "base_url" of relation "evo_core_api_keys" does not exist`
	if dbErr.Message != want {
		t.Fatalf("message = %q, want %q", dbErr.Message, want)
	}
}

func TestMapDBError_UndefinedTableCarriesPgDetail(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "42P01", Message: `relation "evo_core_api_keys" does not exist`}

	err := MapDBError(pgErr, nil)

	var dbErr *Error
	if !errors.As(err, &dbErr) {
		t.Fatalf("expected *postgres.Error, got %T", err)
	}
	if dbErr.Code != ERR_UNDEFINED_TABLE {
		t.Fatalf("code = %s, want %s", dbErr.Code, ERR_UNDEFINED_TABLE)
	}
	if dbErr.Message != `Undefined table: relation "evo_core_api_keys" does not exist` {
		t.Fatalf("unexpected message %q", dbErr.Message)
	}
}

// Other SQLSTATE families keep the generic message: only schema errors are
// enriched, so a constraint violation does not start echoing constraint text.
func TestMapDBError_DuplicateKeyKeepsGenericMessage(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", Message: `duplicate key value violates unique constraint "x"`}

	var dbErr *Error
	if !errors.As(MapDBError(pgErr, nil), &dbErr) {
		t.Fatal("expected *postgres.Error")
	}
	if dbErr.Message != "Duplicate key violation" {
		t.Fatalf("message = %q, want generic", dbErr.Message)
	}
}
