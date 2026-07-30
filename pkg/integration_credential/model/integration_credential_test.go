package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToResponseNeverExposesValue(t *testing.T) {
	encrypted := "gAAAAABn-this-is-the-fernet-ciphertext-of-the-integration-secret"
	credential := IntegrationCredential{
		Name:        "Dify producao",
		Provider:    "dify",
		Kind:        KindStatic,
		Value:       encrypted,
		ValueFormat: ValueFormatScalar,
		ValueHint:   "9c1d",
		Scope:       ScopeAccount,
		IsActive:    true,
	}

	payload, err := json.Marshal(credential.ToResponse())
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	body := string(payload)
	if strings.Contains(body, encrypted) {
		t.Errorf("response leaks the encrypted value: %s", body)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, present := decoded["value"]; present {
		t.Errorf("response still carries a \"value\" field: %s", body)
	}
	if decoded["value_hint"] != "9c1d" {
		t.Errorf("value_hint = %v, want 9c1d", decoded["value_hint"])
	}
}

// The entity is serialized by GORM, never by the API. A field that loses its
// `json:"-"` tag would start reaching the browser through any handler that
// marshals the entity by mistake, which is exactly how a secret escapes.
func TestEntityIsNeverSerializable(t *testing.T) {
	credential := IntegrationCredential{
		Name:      "Nexus",
		Provider:  "knowledge_nexus",
		Value:     "gAAAAAB-secret",
		ValueHint: "abcd",
	}

	payload, err := json.Marshal(credential)
	if err != nil {
		t.Fatalf("marshal entity: %v", err)
	}

	if got := string(payload); got != "{}" {
		t.Errorf("entity serializes to %s, want {} (every field needs json:\"-\")", got)
	}
}

func TestDeriveValueHint(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"regular secret", "app-dify-abcdef9c1d", "9c1d"},
		{"exactly four", "ab12", "ab12"},
		{"shorter than four", "ab", "ab"},
		{"empty", "", ""},
		{"multibyte tail", "chave-ção", "-ção"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveValueHint(tc.value); got != tc.want {
				t.Errorf("DeriveValueHint(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestNormalizeScopeKeepsUnknownOnNarrowest(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{ScopeInstallation, ScopeInstallation},
		{ScopeAccount, ScopeAccount},
		{"", ScopeAccount},
		{"agency", ScopeAccount},
		{"INSTALLATION", ScopeAccount},
	}

	for _, tc := range cases {
		if got := NormalizeScope(tc.in); got != tc.want {
			t.Errorf("NormalizeScope(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeKindDefaultsToStatic(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{KindStatic, KindStatic},
		{KindOAuth, KindOAuth},
		{"", KindStatic},
		{"garbage", KindStatic},
	}

	for _, tc := range cases {
		if got := NormalizeKind(tc.in); got != tc.want {
			t.Errorf("NormalizeKind(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeValueFormatDefaultsToScalar(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{ValueFormatScalar, ValueFormatScalar},
		{ValueFormatComposite, ValueFormatComposite},
		{"", ValueFormatScalar},
		{"garbage", ValueFormatScalar},
	}

	for _, tc := range cases {
		if got := NormalizeValueFormat(tc.in); got != tc.want {
			t.Errorf("NormalizeValueFormat(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A composite secret is a JSON envelope, so the hint must come from the
// sensitive component. Deriving it from the serialized envelope would expose
// syntax characters, and deriving it from the public component (the username)
// would mask the wrong half.
func TestDeriveCompositeHintUsesSensitiveComponent(t *testing.T) {
	envelope := `{"user":"admin","password":"s3nha-f9b2"}`

	got, err := DeriveCompositeHint(envelope)
	if err != nil {
		t.Fatalf("DeriveCompositeHint: %v", err)
	}
	if got != "f9b2" {
		t.Errorf("DeriveCompositeHint = %q, want f9b2 (last 4 of the password)", got)
	}

	if hintFromWholeEnvelope := DeriveValueHint(envelope); got == hintFromWholeEnvelope {
		t.Errorf("hint was derived from the whole envelope (%q)", hintFromWholeEnvelope)
	}
}

func TestDeriveCompositeHintRejectsEnvelopeWithoutSecret(t *testing.T) {
	cases := []struct {
		name     string
		envelope string
	}{
		{"no sensitive field", `{"user":"admin"}`},
		{"not an object", `"just-a-string"`},
		{"malformed", `{"user":`},
		{"empty secret", `{"user":"admin","password":""}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DeriveCompositeHint(tc.envelope); err == nil {
				t.Errorf("DeriveCompositeHint(%q) accepted an envelope with no usable secret", tc.envelope)
			}
		})
	}
}
