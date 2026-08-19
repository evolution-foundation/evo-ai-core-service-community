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
type ReferenceIndex map[uuid.UUID][]string

// For returns the labels of a credential, always a slice and never nil: the
// screen distinguishes "no consumers" from "the server does not know", and a
// nil would serialize as `null` instead of `[]`.
func (i ReferenceIndex) For(id uuid.UUID) []string {
	if labels, ok := i[id]; ok {
		return labels
	}
	return []string{}
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
		index[row.CredentialID] = append(index[row.CredentialID], row.Label)
	}

	// Sorted so the same credential renders its consumers in the same order on
	// every load.
	for id := range index {
		sort.Strings(index[id])
	}

	return index, nil
}

// ConsumersOf names who holds ONE credential, narrowed in the database. It
// propagates a read failure instead of degrading: the caller is the delete
// guard, and an empty answer there means "nobody uses it, go ahead".
func (b *referenceIndexBuilder) ConsumersOf(ctx context.Context, id uuid.UUID) ([]string, error) {
	rows, err := b.reader.ReferencesForCredential(ctx, id)
	if err != nil {
		return nil, err
	}

	labels := make([]string, 0, len(rows))
	for _, row := range rows {
		labels = append(labels, row.Label)
	}
	sort.Strings(labels)

	return labels, nil
}
