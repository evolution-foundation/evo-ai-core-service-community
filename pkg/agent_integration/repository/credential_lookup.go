package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/fernet/fernet-go"
	"gorm.io/gorm"
)

// credentialLookup reads the vault just enough to validate a reference: whether
// an ACTIVE credential with that id exists, and of which kind.
// // It queries the table directly instead of importing the credentials package,
// so the two stores stay independent and only the id travels between them.
type credentialLookup struct {
	db            *gorm.DB
	encryptionKey string
}

func NewCredentialLookup(db *gorm.DB, encryptionKey string) *credentialLookup { //nolint:revive // constructor for an unexported adapter, consumed through the service interface
	return &credentialLookup{db: db, encryptionKey: encryptionKey}
}

// PlaintextOfActive decrypts an ACTIVE static credential with the shared Fernet
// key. An oauth row is refused outright: its value column is NULL by database
// CHECK, because the vault points at the store that owns the token.
func (l *credentialLookup) PlaintextOfActive(ctx context.Context, id string) (string, error) {
	var row struct {
		Kind  string
		Value string
	}

	err := l.db.WithContext(ctx).
		Table("evo_core_integration_credentials").
		Select("kind, value").
		Where("id = ? AND is_active = ?", id, true).
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return "", err
	}
	if row.Kind == "" {
		return "", errors.New("no active credential with that id")
	}
	if row.Kind == "oauth" {
		return "", errors.New("an oauth credential holds no value")
	}

	key, err := fernet.DecodeKey(l.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("invalid encryption key: %w", err)
	}

	plain := fernet.VerifyAndDecrypt([]byte(row.Value), 0, []*fernet.Key{key})
	if plain == nil {
		return "", errors.New("the stored credential could not be decrypted")
	}

	return string(plain), nil
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
