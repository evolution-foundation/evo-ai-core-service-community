package service

import (
	"context"
	"errors"
	"testing"

	"evo-ai-core-service/pkg/integration_credential/model"

	"github.com/google/uuid"
)

var errNotReadable = errors.New("store not readable")

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

func (s *stubReferenceReader) ReferencesForCredential(_ context.Context, id uuid.UUID) ([]model.CredentialReference, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}

	matching := make([]model.CredentialReference, 0, len(s.rows))
	for _, row := range s.rows {
		if row.CredentialID == id {
			matching = append(matching, row)
		}
	}
	return matching, nil
}

func TestReferencesGroupsEveryConsumerUnderItsCredential(t *testing.T) {
	first := uuid.New()
	second := uuid.New()

	reader := &stubReferenceReader{rows: []model.CredentialReference{
		{CredentialID: first, Consumer: model.CredentialConsumer{Kind: model.ConsumerKindAgent, Name: "Dify", Key: "api_key"}},
		{CredentialID: first, Consumer: model.CredentialConsumer{Kind: model.ConsumerKindTool, Name: "Busca", Key: "Authorization"}},
		{CredentialID: second, Consumer: model.CredentialConsumer{Kind: model.ConsumerKindChannelBot, Name: "whatsapp"}},
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
		{CredentialID: uuid.New(), Consumer: model.CredentialConsumer{Kind: model.ConsumerKindIntegration, Name: "um"}},
		{CredentialID: uuid.New(), Consumer: model.CredentialConsumer{Kind: model.ConsumerKindIntegration, Name: "dois"}},
		{CredentialID: uuid.New(), Consumer: model.CredentialConsumer{Kind: model.ConsumerKindIntegration, Name: "tres"}},
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

	id := uuid.New()
	if consumers := index.For(id); consumers == nil {
		t.Error("an unused credential returned nil consumers instead of an empty slice")
	} else if len(consumers) != 0 {
		t.Errorf("an unused credential reported %d consumers", len(consumers))
	}
	if labels := index.LabelsFor(id); labels == nil {
		t.Error("an unused credential returned nil labels instead of an empty slice")
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
		{CredentialID: id, Consumer: model.CredentialConsumer{Kind: model.ConsumerKindTool, Name: "zeta", Key: "k"}},
		{CredentialID: id, Consumer: model.CredentialConsumer{Kind: model.ConsumerKindAgent, Name: "alfa", Key: "k"}},
	}}

	index, _ := NewReferenceIndex(reader).Build(context.Background())

	labels := index.LabelsFor(id)
	if len(labels) != 2 || labels[0] != "Agente alfa [k]" || labels[1] != "Ferramenta zeta [k]" {
		t.Errorf("labels are not sorted for stable rendering: %v", labels)
	}
	consumers := index.For(id)
	if len(consumers) != 2 || consumers[0].Name != "alfa" || consumers[1].Name != "zeta" {
		t.Errorf("consumers are not in the order of their labels: %v", consumers)
	}
}

// The delete guard asks about ONE credential, so the narrowing happens in the
// store: building the whole index to read one entry would sweep every consumer
// in the database to answer about a single row.
func TestConsumersOfNarrowsToTheCredentialAsked(t *testing.T) {
	wanted := uuid.New()
	other := uuid.New()

	reader := &stubReferenceReader{rows: []model.CredentialReference{
		{CredentialID: wanted, Consumer: model.CredentialConsumer{Kind: model.ConsumerKindMCP, Name: "Zendesk", Key: "token"}},
		{CredentialID: wanted, Consumer: model.CredentialConsumer{Kind: model.ConsumerKindAgent, Name: "Cobrança", Key: "api_key"}},
		{CredentialID: other, Consumer: model.CredentialConsumer{Kind: model.ConsumerKindChannelBot, Name: "whatsapp"}},
	}}

	consumers, err := NewReferenceIndex(reader).ConsumersOf(context.Background(), wanted)
	if err != nil {
		t.Fatalf("ConsumersOf: %v", err)
	}

	want := []model.CredentialConsumer{
		{Kind: model.ConsumerKindAgent, Name: "Cobrança", Key: "api_key"},
		{Kind: model.ConsumerKindMCP, Name: "Zendesk", Key: "token"},
	}
	if len(consumers) != len(want) {
		t.Fatalf("consumers = %v, want %v", consumers, want)
	}
	for i := range want {
		if consumers[i] != want[i] {
			t.Errorf("consumers[%d] = %v, want %v", i, consumers[i], want[i])
		}
	}
}

// The listing degrades to an empty index on a read failure; the guard must not.
func TestConsumersOfPropagatesAReadFailure(t *testing.T) {
	reader := &stubReferenceReader{err: errNotReadable}

	if _, err := NewReferenceIndex(reader).ConsumersOf(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected the read failure to propagate, got nil")
	}
}

func TestLabelsKeepTheStringsOlderClientsDisplay(t *testing.T) {
	cases := []struct {
		consumer model.CredentialConsumer
		want     string
	}{
		{model.CredentialConsumer{Kind: model.ConsumerKindIntegration, Name: "github"}, "Integração github"},
		{model.CredentialConsumer{Kind: model.ConsumerKindTool, Name: "Busca", Key: "Authorization"}, "Ferramenta Busca [Authorization]"},
		{model.CredentialConsumer{Kind: model.ConsumerKindMCP, Name: "Zendesk", Key: "token"}, "MCP Zendesk [token]"},
		{model.CredentialConsumer{Kind: model.ConsumerKindAgent, Name: "Cobrança", Key: "api_key"}, "Agente Cobrança [api_key]"},
		{model.CredentialConsumer{Kind: model.ConsumerKindChannelBot, Name: "whatsapp"}, "Bot de canal (whatsapp)"},
	}

	for _, tc := range cases {
		if got := tc.consumer.Label(); got != tc.want {
			t.Errorf("%s label = %q, want %q", tc.consumer.Kind, got, tc.want)
		}
	}
}
