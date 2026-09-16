package service

import (
	"context"
	"sort"

	"evo-ai-core-service/pkg/integration_credential/model"

	"github.com/google/uuid"
)

// ReferenceReader reads the consumers that point at a vault credential, either
// in ONE pass for a whole page (per-credential would be 5N round trips for a
// page of N) or narrowed to a single credential, which is what the delete guard
// asks and where the full sweep would materialize every consumer in the
// database to answer about one row.
type ReferenceReader interface {
	ReferencesByCredential(ctx context.Context) ([]model.CredentialReference, error)
	ReferencesForCredential(ctx context.Context, id uuid.UUID) ([]model.CredentialReference, error)
}

// ReferenceIndex answers "who uses this credential" for the whole page.
type ReferenceIndex map[uuid.UUID][]model.CredentialConsumer

// For returns the consumers of a credential, always a slice and never nil: the
// screen distinguishes "no consumers" from "the server does not know", and a
// nil would serialize as `null` instead of `[]`.
func (i ReferenceIndex) For(id uuid.UUID) []model.CredentialConsumer {
	if consumers, ok := i[id]; ok {
		return consumers
	}
	return []model.CredentialConsumer{}
}

// LabelsFor returns the consumers of a credential as pt-BR display strings.
func (i ReferenceIndex) LabelsFor(id uuid.UUID) []string {
	return Labels(i.For(id))
}

// Labels renders consumers as pt-BR display strings.
func Labels(consumers []model.CredentialConsumer) []string {
	labels := make([]string, 0, len(consumers))
	for _, consumer := range consumers {
		labels = append(labels, consumer.Label())
	}
	return labels
}

type referenceIndexBuilder struct {
	reader ReferenceReader
}

func NewReferenceIndex(reader ReferenceReader) *referenceIndexBuilder { //nolint:revive // builder consumed through Build
	return &referenceIndexBuilder{reader: reader}
}

// Build groups the references by credential. A read failure returns an EMPTY
// index alongside the error, so a caller that degrades still renders something
// safe: the reference list is decoration, the credentials are the point.
func (b *referenceIndexBuilder) Build(ctx context.Context) (ReferenceIndex, error) {
	index := ReferenceIndex{}

	rows, err := b.reader.ReferencesByCredential(ctx)
	if err != nil {
		return index, err
	}

	for _, row := range rows {
		index[row.CredentialID] = append(index[row.CredentialID], row.Consumer)
	}

	// Sorted so the same credential renders its consumers in the same order on
	// every load.
	for id := range index {
		sortByLabel(index[id])
	}

	return index, nil
}

// ConsumersOf names who holds ONE credential, narrowed in the database. It
// propagates a read failure instead of degrading: the caller is the delete
// guard, and an empty answer there means "nobody uses it, go ahead".
func (b *referenceIndexBuilder) ConsumersOf(ctx context.Context, id uuid.UUID) ([]model.CredentialConsumer, error) {
	rows, err := b.reader.ReferencesForCredential(ctx, id)
	if err != nil {
		return nil, err
	}

	consumers := make([]model.CredentialConsumer, 0, len(rows))
	for _, row := range rows {
		consumers = append(consumers, row.Consumer)
	}
	sortByLabel(consumers)

	return consumers, nil
}

func sortByLabel(consumers []model.CredentialConsumer) {
	sort.SliceStable(consumers, func(a, b int) bool {
		return consumers[a].Label() < consumers[b].Label()
	})
}
