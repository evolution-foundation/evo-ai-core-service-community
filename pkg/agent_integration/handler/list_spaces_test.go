package handler

import (
	"context"
	"testing"
)

// stubSpaceSource resolves a saved Nexus integration.
type stubSpaceSource struct {
	baseURL string
	apiKey  string
	err     error
	askedID string
}

func (s *stubSpaceSource) ResolveNexusTarget(_ context.Context, agentID string) (string, string, error) {
	s.askedID = agentID
	return s.baseURL, s.apiKey, s.err
}

// The security invariant of this endpoint, and the reason it is a resolver
// rather than an extra request field: a VAULT-RESOLVED key may only ever be
// sent to the base URL stored in the SAME integration row.
//
// Accepting a server-resolved credential together with a caller-supplied URL
// would turn `ai_agents:update` into a credential-exfiltration primitive:
// point the URL at your own host and harvest the stored Nexus key.
func TestSavedModeRejectsACallerSuppliedBaseURL(t *testing.T) {
	req := listKnowledgeNexusSpacesRequest{
		AgentID:      "11111111-1111-1111-1111-111111111111",
		NexusBaseURL: "https://atacante.example.com",
	}

	if err := req.validate(); err == nil {
		t.Fatal("a caller-supplied base URL was accepted alongside an agent reference")
	}
}

func TestSavedModeNeedsOnlyTheReference(t *testing.T) {
	req := listKnowledgeNexusSpacesRequest{AgentID: "11111111-1111-1111-1111-111111111111"}

	if err := req.validate(); err != nil {
		t.Fatalf("a bare agent reference was rejected: %v", err)
	}
	if !req.usesSavedCredential() {
		t.Error("the request should resolve through the saved integration")
	}
}

// The legacy mode stays fully working: while the user is typing a new base URL
// there is no saved row to resolve, and the caller already holds both values,
// so forwarding them adds no exposure.
func TestLegacyModeStillAcceptsBothFields(t *testing.T) {
	req := listKnowledgeNexusSpacesRequest{
		NexusBaseURL: "https://nexus.example.com",
		NexusAPIKey:  "evo_k_ab.cd",
	}

	if err := req.validate(); err != nil {
		t.Fatalf("the legacy two-field mode was rejected: %v", err)
	}
	if req.usesSavedCredential() {
		t.Error("a request with an explicit key must not resolve from the vault")
	}
}

func TestRequestWithoutAnyCredentialIsRejected(t *testing.T) {
	if err := (listKnowledgeNexusSpacesRequest{NexusBaseURL: "https://nexus.example.com"}).validate(); err == nil {
		t.Error("a request with no key and no reference was accepted")
	}
	if err := (listKnowledgeNexusSpacesRequest{}).validate(); err == nil {
		t.Error("an empty request was accepted")
	}
}

// A saved integration whose credential cannot be resolved must fail loudly
// rather than fall through to an unauthenticated upstream call.
func TestSavedModeFailsWhenTheTargetCannotBeResolved(t *testing.T) {
	source := &stubSpaceSource{err: context.DeadlineExceeded}

	_, _, err := source.ResolveNexusTarget(context.Background(), "agent-1")
	if err == nil {
		t.Error("an unresolvable target reported success")
	}
}
