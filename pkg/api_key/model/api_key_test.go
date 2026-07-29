package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToResponseNeverExposesKey(t *testing.T) {
	encrypted := "gAAAAABn-this-is-the-fernet-ciphertext-of-the-secret"
	apiKey := ApiKey{
		Name:     "Producao",
		Provider: "openai",
		Key:      encrypted,
		KeyHint:  "4f2a",
		IsActive: true,
	}

	payload, err := json.Marshal(apiKey.ToResponse())
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	body := string(payload)
	if strings.Contains(body, encrypted) {
		t.Errorf("response leaks the encrypted key: %s", body)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, present := decoded["key"]; present {
		t.Errorf("response still carries a \"key\" field: %s", body)
	}
	if decoded["key_hint"] != "4f2a" {
		t.Errorf("key_hint = %v, want 4f2a", decoded["key_hint"])
	}
}

func TestDeriveKeyHint(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{"regular key", "sk-proj-abcdef1234f2a", "4f2a"},
		{"exactly four", "ab12", "ab12"},
		{"shorter than four", "ab", "ab"},
		{"empty", "", ""},
		{"multibyte tail", "sk-chave-ção", "-ção"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveKeyHint(tc.key); got != tc.want {
				t.Errorf("DeriveKeyHint(%q) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

func TestOpenAICompatibleByProvider(t *testing.T) {
	compatible := []string{"openai", "azure", "custom", "custom_openai_compatible"}
	for _, provider := range compatible {
		if !IsOpenAICompatible(provider) {
			t.Errorf("provider %q should be OpenAI-compatible", provider)
		}
	}

	agentsOnly := []string{"anthropic", "gemini", "groq", "mistral", "cohere",
		"openrouter", "deepseek", "together_ai", "fireworks_ai", "perplexity",
		"bedrock", "vertex_ai"}
	for _, provider := range agentsOnly {
		if IsOpenAICompatible(provider) {
			t.Errorf("provider %q should not be OpenAI-compatible", provider)
		}
	}
}

func TestScopeIsCarriedToResponse(t *testing.T) {
	installation := ApiKey{Provider: "openai", Scope: ScopeInstallation}
	if got := installation.ToResponse().Scope; got != ScopeInstallation {
		t.Errorf("scope = %q, want %q", got, ScopeInstallation)
	}

	account := ApiKey{Provider: "openai", Scope: ScopeAccount}
	if got := account.ToResponse().Scope; got != ScopeAccount {
		t.Errorf("scope = %q, want %q", got, ScopeAccount)
	}
}

func TestNormalizeScopeDefaultsToAccount(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ScopeAccount},
		{"account", ScopeAccount},
		{"installation", ScopeInstallation},
		{"nonsense", ScopeAccount},
	}

	for _, tc := range cases {
		if got := NormalizeScope(tc.in); got != tc.want {
			t.Errorf("NormalizeScope(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestToResponseCarriesOpenAICompatible(t *testing.T) {
	openAIKey := ApiKey{Provider: "openai"}
	if !openAIKey.ToResponse().OpenAICompatible {
		t.Error("openai key should report openai_compatible = true")
	}

	anthropicKey := ApiKey{Provider: "anthropic"}
	if anthropicKey.ToResponse().OpenAICompatible {
		t.Error("anthropic key should report openai_compatible = false")
	}
}
