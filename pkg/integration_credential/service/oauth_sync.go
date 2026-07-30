package service

import (
	"context"
	"time"

	"evo-ai-core-service/pkg/integration_credential/model"
)

// ConnectionReader reads the live OAuth connections from the store that OWNS
// them (evo_core_agent_integrations, joined with the agents table for the
// label). It reads metadata only: no token crosses this boundary.
type ConnectionReader interface {
	LiveConnections(ctx context.Context) ([]model.OAuthConnection, error)
}

// ReferenceStore persists the vault's side: the reference rows, never a secret.
type ReferenceStore interface {
	ListOAuthRows(ctx context.Context) ([]model.IntegrationCredential, error)
	UpsertOAuthRow(ctx context.Context, row model.IntegrationCredential) error
	DeactivateOAuthRow(ctx context.Context, ownerRef string) error
}

// OAuthSync reconciles the vault's oauth rows with the connections in the owner
// store. It runs on listing, not on the OAuth callback: the callback lives in
// the processor, and reconciling on read means a connection made anywhere shows
// up next listing with no coupling.
// // It never copies a token. The rows it writes carry `owner_store` and
// `owner_ref` and nothing else from the connection.
type OAuthSync struct {
	reader ConnectionReader
	store  ReferenceStore
}

func NewOAuthSync(reader ConnectionReader, store ReferenceStore) *OAuthSync {
	return &OAuthSync{reader: reader, store: store}
}

// Run makes the vault's oauth rows match the live connections. It is idempotent
// by the natural key (owner_store, owner_ref): re-running with nothing changed
// writes nothing, which matters because it runs on every page load.
func (s *OAuthSync) Run(ctx context.Context) error {
	connections, err := s.reader.LiveConnections(ctx)
	if err != nil {
		return err
	}

	existing, err := s.store.ListOAuthRows(ctx)
	if err != nil {
		return err
	}

	existingByRef := make(map[string]model.IntegrationCredential, len(existing))
	for _, row := range existing {
		if row.OwnerRef != nil {
			existingByRef[*row.OwnerRef] = row
		}
	}

	live := make(map[string]struct{}, len(connections))
	for _, connection := range connections {
		if !model.IsConnectionProvider(connection.Provider) {
			continue
		}

		live[connection.IntegrationID] = struct{}{}

		row, present := existingByRef[connection.IntegrationID]
		if present && row.IsActive {
			// Nothing to write: the mirrored state is read at display time, so
			// an unchanged connection produces no updated_at churn.
			continue
		}

		if err := s.store.UpsertOAuthRow(ctx, connection.ToVaultRow()); err != nil {
			return err
		}
	}

	// A connection that vanished from the owner store is DEACTIVATED, not
	// deleted: the row is reversible if the connection comes back, and it never
	// held a secret to lose.
	for ownerRef, row := range existingByRef {
		if _, stillLive := live[ownerRef]; stillLive || !row.IsActive {
			continue
		}
		if err := s.store.DeactivateOAuthRow(ctx, ownerRef); err != nil {
			return err
		}
	}

	return nil
}

// Decorate adds the state read LIVE from the owner store to each reference row.
// Nothing here is persisted: a stored copy would go stale on the first token
// rotation and show "connected" for a dead integration.
func (s *OAuthSync) Decorate(ctx context.Context, rows []model.IntegrationCredential, now time.Time) ([]*model.IntegrationCredentialResponse, error) {
	connections, err := s.reader.LiveConnections(ctx)
	if err != nil {
		return nil, err
	}

	byRef := make(map[string]model.OAuthConnection, len(connections))
	for _, connection := range connections {
		byRef[connection.IntegrationID] = connection
	}

	decorated := make([]*model.IntegrationCredentialResponse, 0, len(rows))
	for _, row := range rows {
		if row.OwnerRef == nil {
			decorated = append(decorated, row.ToResponse())
			continue
		}

		connection, found := byRef[*row.OwnerRef]
		if !found {
			// The owner is gone. Saying "connected" here would be the exact lie
			// this story exists to prevent.
			response := row.ToResponse()
			response.ConnectionStatus = model.ConnectionStatusExpired
			decorated = append(decorated, response)
			continue
		}

		decorated = append(decorated, connection.ToResponse(row, now))
	}

	return decorated, nil
}
