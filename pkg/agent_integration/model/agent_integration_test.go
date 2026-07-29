package model

import (
	"encoding/json"
	"testing"

	"gorm.io/datatypes"
)

func configWith(t *testing.T, fields map[string]interface{}) datatypes.JSON {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return datatypes.JSON(raw)
}

// The platform secret of an external agent lives in this config, and until
// story 2.3 it was echoed verbatim to anyone holding ai_agents:read: the
// sanitizer only knew OAuth field names.
func TestSanitizeConfigRemovesPlatformSecrets(t *testing.T) {
	integration := AgentIntegration{
		Provider: "dify",
		Config: configWith(t, map[string]interface{}{
			"apiUrl":        "https://dify.example.com",
			"apiKey":        "app-dify-abcdef9c1d",
			"basicAuthUser": "admin",
			"basicAuthPass": "s3nha-f9b2",
			"nexus_api_key": "evo_k_abc.def",
			"botType":       "chatBot",
		}),
	}

	payload, err := json.Marshal(integration.ToResponse())
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	body := string(payload)

	for _, secret := range []string{"app-dify-abcdef9c1d", "s3nha-f9b2", "evo_k_abc.def"} {
		if contains(body, secret) {
			t.Errorf("response leaks %q: %s", secret, body)
		}
	}

	// What is NOT a secret has to survive: apiUrl and botType are the address
	// and the mode, and dropping them would break the screen.
	for _, kept := range []string{"https://dify.example.com", "chatBot"} {
		if !contains(body, kept) {
			t.Errorf("response dropped the non-secret %q: %s", kept, body)
		}
	}
	if contains(body, "admin") {
		t.Errorf("basicAuthUser is half of the credential and must not be echoed: %s", body)
	}
}

func TestSanitizeConfigKeepsExistingOAuthCoverage(t *testing.T) {
	integration := AgentIntegration{
		Provider: "github",
		Config: configWith(t, map[string]interface{}{
			"access_token":  "gho_secret",
			"refresh_token": "ghr_secret",
			"client_secret": "cs_secret",
			"connected":     true,
		}),
	}

	payload, _ := json.Marshal(integration.ToResponse())
	body := string(payload)

	for _, secret := range []string{"gho_secret", "ghr_secret", "cs_secret"} {
		if contains(body, secret) {
			t.Errorf("response leaks %q: %s", secret, body)
		}
	}
	if !contains(body, "connected") {
		t.Errorf("connected flag was dropped: %s", body)
	}
}

// The screens round-trip the config object they received: they load a field
// from the GET and send the whole object back on save. Sanitizing a field out
// of the response without merging on write means the next save stores a config
// with no secret, silently erasing it.
func TestMergePreservedSecretsRestoresOmittedFields(t *testing.T) {
	stored := map[string]interface{}{
		"apiUrl":        "https://dify.example.com",
		"apiKey":        "app-dify-abcdef9c1d",
		"basicAuthPass": "s3nha-f9b2",
	}
	// What a screen sends back after the sanitized GET: the secret is simply
	// absent, not blank.
	incoming := map[string]interface{}{
		"apiUrl":  "https://dify.example.com/v2",
		"botType": "chatBot",
	}

	merged := MergePreservedSecrets(incoming, stored)

	if merged["apiKey"] != "app-dify-abcdef9c1d" {
		t.Errorf("apiKey was erased by a save that never carried it: %v", merged["apiKey"])
	}
	if merged["basicAuthPass"] != "s3nha-f9b2" {
		t.Errorf("basicAuthPass was erased: %v", merged["basicAuthPass"])
	}
	if merged["apiUrl"] != "https://dify.example.com/v2" {
		t.Errorf("non-secret field was not updated: %v", merged["apiUrl"])
	}
	if merged["botType"] != "chatBot" {
		t.Errorf("new field was not written: %v", merged["botType"])
	}
}

func TestMergePreservedSecretsAcceptsAnExplicitNewSecret(t *testing.T) {
	stored := map[string]interface{}{"apiKey": "app-dify-velha"}
	incoming := map[string]interface{}{"apiKey": "app-dify-nova"}

	merged := MergePreservedSecrets(incoming, stored)

	if merged["apiKey"] != "app-dify-nova" {
		t.Errorf("a deliberate rotation was ignored: %v", merged["apiKey"])
	}
}

// Sending an empty string is how a screen says "clear this": it is a present
// key, so it must win over the stored value instead of being treated as absent.
func TestMergePreservedSecretsHonoursExplicitBlank(t *testing.T) {
	stored := map[string]interface{}{"apiKey": "app-dify-velha"}
	incoming := map[string]interface{}{"apiKey": ""}

	merged := MergePreservedSecrets(incoming, stored)

	if merged["apiKey"] != "" {
		t.Errorf("an explicit clear was overridden by the stored value: %v", merged["apiKey"])
	}
}

func TestMergePreservedSecretsHandlesEmptyStored(t *testing.T) {
	merged := MergePreservedSecrets(map[string]interface{}{"apiKey": "nova"}, nil)

	if merged["apiKey"] != "nova" {
		t.Errorf("first save lost the secret: %v", merged["apiKey"])
	}
}

func TestCredentialIDIsReadFromConfig(t *testing.T) {
	id := "8f14e45f-ceea-467a-9f37-3d1c2b8d1234"

	got, present := CredentialIDFrom(map[string]interface{}{"credential_id": id})
	if !present || got != id {
		t.Errorf("CredentialIDFrom = (%q, %v), want (%q, true)", got, present, id)
	}

	if _, present := CredentialIDFrom(map[string]interface{}{"apiKey": "x"}); present {
		t.Error("CredentialIDFrom reported a reference where there is none")
	}
	if _, present := CredentialIDFrom(map[string]interface{}{"credential_id": ""}); present {
		t.Error("a blank credential_id must not count as a reference")
	}
	if _, present := CredentialIDFrom(nil); present {
		t.Error("a nil config must not carry a reference")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && stringIndex(haystack, needle) >= 0
}

func stringIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
