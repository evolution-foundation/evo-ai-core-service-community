package service

import (
	"context"
	"testing"

	"evo-ai-core-service/pkg/integration_credential/model"

	"github.com/google/uuid"
)

// stubReferenceReader stands in for the five stores.
type stubReferenceReader struct {
	rows  []model.CredentialReference
	err   error
	calls int
}

func (s *stubReferenceReader) ReferencesByCredential(_ context.Context) ([]model.CredentialReference, error) {
	s.calls++
	return s.rows, s.err
}

func TestReferencesGroupsEveryConsumerUnderItsCredential(t *testing.T) {
	first := uuid.New()
	second := uuid.New()

	reader := &stubReferenceReader{rows: []model.CredentialReference{
		{CredentialID: first, Label: "Agente Dify"},
		{CredentialID: first, Label: "Tool Busca [Authorization]"},
		{CredentialID: second, Label: "Bot do WhatsApp"},
	}}

	index, err := NewReferenceIndex(reader).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if got := index[first]; len(got) != 2 {
		t.Errorf("credential %s has %d references, want 2: %v", first, len(got), got)
	}
	if got := index[second]; len(got) != 1 {
		t.Errorf("credential %s has %d references, want 1: %v", second, len(got), got)
	}
}

// The list endpoint renders N credentials: querying per credential would be
// 5N round trips. The stores are read ONCE and joined in memory.
func TestReferencesReadsTheStoresOncePerRequest(t *testing.T) {
	reader := &stubReferenceReader{rows: []model.CredentialReference{
		{CredentialID: uuid.New(), Label: "um"},
		{CredentialID: uuid.New(), Label: "dois"},
		{CredentialID: uuid.New(), Label: "tres"},
	}}

	if _, err := NewReferenceIndex(reader).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if reader.calls != 1 {
		t.Errorf("the stores were read %d times, want 1", reader.calls)
	}
}

// A credential nobody uses reports an EMPTY list, not a missing field: the
// screen distinguishes "no consumers" from "the server does not know".
func TestReferencesAreEmptyRatherThanAbsentForAnUnusedCredential(t *testing.T) {
	index, err := NewReferenceIndex(&stubReferenceReader{}).Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if labels := index.For(uuid.New()); labels == nil {
		t.Error("an unused credential returned nil instead of an empty slice")
	} else if len(labels) != 0 {
		t.Errorf("an unused credential reported %d references", len(labels))
	}
}

// A read failure must not fail the listing: the credentials themselves are the
// point of the endpoint, and the reference list is decoration.
func TestReferencesDegradeToEmptyOnReadFailure(t *testing.T) {
	index, err := NewReferenceIndex(&stubReferenceReader{err: context.DeadlineExceeded}).Build(context.Background())

	if err == nil {
		t.Error("a read failure was swallowed instead of reported to the caller")
	}
	if index == nil {
		t.Fatal("Build returned a nil index on failure; the caller cannot render at all")
	}
	if labels := index.For(uuid.New()); len(labels) != 0 {
		t.Error("a failed read produced phantom references")
	}
}

func TestReferenceLabelsAreStableForRendering(t *testing.T) {
	id := uuid.New()
	reader := &stubReferenceReader{rows: []model.CredentialReference{
		{CredentialID: id, Label: "zeta"},
		{CredentialID: id, Label: "alfa"},
	}}

	index, _ := NewReferenceIndex(reader).Build(context.Background())

	labels := index.For(id)
	if len(labels) != 2 || labels[0] != "alfa" || labels[1] != "zeta" {
		t.Errorf("labels are not sorted for stable rendering: %v", labels)
	}
}
