package secretmerge

import "testing"

// The screens round-trip the object they received: they load fields from the
// GET and send the whole thing back on save. Since the response stopped
// carrying header values, a save arrives WITHOUT them, and a wholesale
// overwrite would erase the stored secret.
//
// This is the same defect that bit story 2.3 on agent integrations, in a second
// location: tools and MCP servers replace `headers` wholesale on update.
func TestKeepMissingRestoresOmittedEntries(t *testing.T) {
	stored := map[string]string{
		"Authorization": "Bearer sk-secreto",
		"X-Api-Key":     "chave-secreta",
		"Content-Type":  "application/json",
	}
	// What a screen sends back after a sanitized GET: the secret headers are
	// simply absent.
	incoming := map[string]string{"Content-Type": "application/xml"}

	merged := KeepMissing(incoming, stored)

	if merged["Authorization"] != "Bearer sk-secreto" {
		t.Errorf("Authorization was erased by a save that never carried it: %q", merged["Authorization"])
	}
	if merged["X-Api-Key"] != "chave-secreta" {
		t.Errorf("X-Api-Key was erased: %q", merged["X-Api-Key"])
	}
	if merged["Content-Type"] != "application/xml" {
		t.Errorf("a non-secret header was not updated: %q", merged["Content-Type"])
	}
}

func TestKeepMissingAcceptsADeliberateRotation(t *testing.T) {
	merged := KeepMissing(
		map[string]string{"Authorization": "Bearer novo"},
		map[string]string{"Authorization": "Bearer velho"},
	)

	if merged["Authorization"] != "Bearer novo" {
		t.Errorf("a deliberate rotation was ignored: %q", merged["Authorization"])
	}
}

// Sending an empty value is how a screen says "clear this": a present key wins,
// even when blank.
func TestKeepMissingHonoursExplicitBlank(t *testing.T) {
	merged := KeepMissing(
		map[string]string{"Authorization": ""},
		map[string]string{"Authorization": "Bearer velho"},
	)

	if merged["Authorization"] != "" {
		t.Errorf("an explicit clear was overridden: %q", merged["Authorization"])
	}
}

func TestKeepMissingHandlesEmptyStored(t *testing.T) {
	merged := KeepMissing(map[string]string{"Authorization": "Bearer novo"}, nil)

	if merged["Authorization"] != "Bearer novo" {
		t.Errorf("first save lost the header: %q", merged["Authorization"])
	}
}

func TestKeepMissingNeverMutatesTheInputs(t *testing.T) {
	stored := map[string]string{"Authorization": "Bearer velho"}
	incoming := map[string]string{"Content-Type": "application/json"}

	KeepMissing(incoming, stored)

	if _, present := incoming["Authorization"]; present {
		t.Error("the incoming map was mutated in place")
	}
	if len(stored) != 1 {
		t.Error("the stored map was mutated in place")
	}
}

// A header whose name is not obviously a credential is still redacted: the map
// is free-form, so `X-Tenant-Token` and friends carry secrets too. Only an
// allowlist of known-safe headers survives.
func TestRedactValuesKeepsOnlySafeHeaders(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer sk-secreto",
		"X-Api-Key":     "chave",
		"X-Tenant-Auth": "outro-segredo",
		"Content-Type":  "application/json",
		"Accept":        "application/json",
	}

	redacted := RedactValues(headers)

	for _, name := range []string{"Authorization", "X-Api-Key", "X-Tenant-Auth"} {
		if value, present := redacted[name]; present && value != "" {
			t.Errorf("%s still carries a value (%q)", name, value)
		}
	}
	for _, name := range []string{"Content-Type", "Accept"} {
		if redacted[name] == "" {
			t.Errorf("%s is not a secret and should have survived", name)
		}
	}
	// The NAMES survive: the screen has to render which headers exist.
	if len(redacted) != len(headers) {
		t.Errorf("redaction dropped header names: got %d, want %d", len(redacted), len(headers))
	}
}

func TestRedactValuesHandlesNil(t *testing.T) {
	if redacted := RedactValues(nil); redacted == nil {
		t.Error("RedactValues(nil) returned nil instead of an empty map")
	}
}
