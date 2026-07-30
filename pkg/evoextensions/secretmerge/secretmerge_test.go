package secretmerge

import "testing"

// The round-trip a screen actually performs: RedactValues sends every NAME with
// a blanked value, the screen edits an unrelated field and posts the object
// back. Those blanks must not erase the stored secrets, and tools and MCP
// servers replace `headers` wholesale on update.
// // Absence is a different case — it means deletion, covered separately below.
func TestKeepMissingRestoresRedactedEntries(t *testing.T) {
	stored := map[string]string{
		"Authorization": "Bearer sk-secreto",
		"X-Api-Key":     "chave-secreta",
		"Content-Type":  "application/json",
	}
	incoming := RedactValues(stored)
	incoming["Content-Type"] = "application/xml"

	merged := KeepMissing(incoming, stored)

	if merged["Authorization"] != "Bearer sk-secreto" {
		t.Errorf("Authorization was erased by the redacted echo: %q", merged["Authorization"])
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

// A blank under a REDACTED name is our own redaction echoing back, never user
// intent: RedactValues sends `Authorization: ""` on every GET and the screens
// round-trip what they received, so honouring the blank erased the stored
// secret on every save (the 1.6 modal data-loss path, again).
func TestKeepMissingTreatsRedactedEchoAsKeep(t *testing.T) {
	merged := KeepMissing(
		map[string]string{"Authorization": ""},
		map[string]string{"Authorization": "Bearer velho"},
	)

	if merged["Authorization"] != "Bearer velho" {
		t.Errorf("the redacted echo erased the stored secret: %q", merged["Authorization"])
	}
}

// The composition property that makes the round trip safe by construction:
// saving back exactly what the GET returned changes nothing.
func TestRedactThenMergeIsLossless(t *testing.T) {
	stored := map[string]string{
		"Authorization": "Bearer secreto",
		"X-Tenant-Auth": "tk-1",
		"Content-Type":  "application/json",
	}

	merged := KeepMissing(RedactValues(stored), stored)

	for key, want := range stored {
		if merged[key] != want {
			t.Errorf("%s = %q after a redacted round trip, want %q", key, merged[key], want)
		}
	}
}

// Safe-listed names are never redacted, so a blank there IS user intent and
// still clears.
func TestKeepMissingHonoursBlankOnSafeNames(t *testing.T) {
	merged := KeepMissing(
		map[string]string{"Content-Type": ""},
		map[string]string{"Content-Type": "application/json"},
	)

	if merged["Content-Type"] != "" {
		t.Errorf("a genuine clear on a safe header was overridden: %q", merged["Content-Type"])
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

// Removing a header must actually remove it.
// // The redaction blanks VALUES but always sends the NAMES, so an absent key is
// unambiguous user intent: the row was deleted in the editor. Restoring every
// absent key made deletion impossible — the header came back on reload, for
// auth and non-auth names alike, with no UI affordance able to shed it.
func TestKeepMissingLetsAHeaderBeDeleted(t *testing.T) {
	stored := map[string]string{
		"Authorization": "Bearer secreto",
		"Content-Type":  "application/json",
	}
	// The editor kept Content-Type and removed the Authorization row entirely.
	incoming := map[string]string{"Content-Type": "application/json"}

	merged := KeepMissing(incoming, stored)

	if _, present := merged["Authorization"]; present {
		t.Errorf("a deleted header was restored: %v", merged)
	}
	if merged["Content-Type"] != "application/json" {
		t.Errorf("the kept header was lost: %v", merged)
	}
}

// The redacted echo still has to be preserved: a blank under a redacted NAME is
// our own redaction coming back, not a request to clear.
func TestKeepMissingStillPreservesTheRedactedEchoWhileAllowingDeletion(t *testing.T) {
	stored := map[string]string{"Authorization": "Bearer secreto", "X-Api-Key": "chave"}
	// The screen returns what it received: names present, values blanked. It
	// deleted X-Api-Key.
	incoming := map[string]string{"Authorization": ""}

	merged := KeepMissing(incoming, stored)

	if merged["Authorization"] != "Bearer secreto" {
		t.Errorf("the redacted echo erased the stored secret: %q", merged["Authorization"])
	}
	if _, present := merged["X-Api-Key"]; present {
		t.Error("the deleted header was restored")
	}
}
