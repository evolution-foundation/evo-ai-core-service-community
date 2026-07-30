package service

import (
	"context"
	"sort"

	"evo-ai-core-service/pkg/integration_credential/model"

	"github.com/google/uuid"
)

// ReferenceReader reads, in ONE pass, every consumer that points at a vault
// credential. Per-credential would be 5N round trips for a page of N.
type ReferenceReader interface {
	ReferencesByCredential(ctx context.Context) ([]model.CredentialReference, error)
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
