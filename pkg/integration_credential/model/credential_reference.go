package model

import "github.com/google/uuid"

// CredentialReference is one consumer pointing at one vault credential.
// // `Label` avoids consumer NAMES for stores with no tenant column: `agent_bots`
// is not on the tenantstamp allowlist, so bot names could disclose across
// tenants in the enterprise build.
type CredentialReference struct {
	CredentialID uuid.UUID
	Label        string
}
