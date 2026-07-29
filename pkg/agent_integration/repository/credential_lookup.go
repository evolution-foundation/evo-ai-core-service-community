package repository

import (
	"context"

	"gorm.io/gorm"
)

// credentialLookup reads the integration credential vault
// (evo_core_integration_credentials, story 2.1) just enough to validate a
// reference: whether an ACTIVE credential with that id exists, and of which
// kind.
//
// It queries the table directly rather than importing the credentials package,
// so the two stores stay independent and only the id travels between them. The
// query is scoped and parameterized, unlike the raw-connection pattern used by
// some processor tools.
type credentialLookup struct {
	db *gorm.DB
}

func NewCredentialLookup(db *gorm.DB) *credentialLookup { //nolint:revive // constructor for an unexported adapter, consumed through the service interface
	return &credentialLookup{db: db}
}

func (l *credentialLookup) KindOfActive(ctx context.Context, id string) (string, bool) {
	var kind string

	err := l.db.WithContext(ctx).
		Table("evo_core_integration_credentials").
		Select("kind").
		Where("id = ? AND is_active = ?", id, true).
		Limit(1).
		Scan(&kind).Error

	if err != nil || kind == "" {
		return "", false
	}

	return kind, true
}
