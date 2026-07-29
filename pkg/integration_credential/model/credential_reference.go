package model

import "github.com/google/uuid"

// CredentialReference is one consumer pointing at one vault credential.
//
// `Label` is a display string because that is the contract the screen reads
// (`referenced_by?: string[]`). It deliberately avoids consumer NAMES for the
// stores that have no tenant column: `agent_bots` is not on the tenantstamp
// allowlist, so echoing bot names could disclose across tenants in the
// enterprise build.
type CredentialReference struct {
	CredentialID uuid.UUID
	Label        string
}
