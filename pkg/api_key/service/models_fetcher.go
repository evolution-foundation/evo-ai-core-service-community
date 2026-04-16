package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"evo-ai-core-service/internal/httpclient"
)

// ModelInfo is the normalized shape the frontend ModelSelector consumes.
type ModelInfo struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
}

// ProviderSupportsDynamicModels reports whether the provider has an HTTP API
// we can query to list its models. Providers that require vendor SDKs
// (Bedrock, Vertex AI) or have no public listing endpoint (Perplexity) fall
// back to the frontend's hardcoded list.
func ProviderSupportsDynamicModels(provider string) bool {
	switch provider {
	case "openai", "anthropic", "gemini", "openrouter", "deepseek", "together_ai", "fireworks_ai":
		return true
	}
	return false
}

// FetchProviderModels calls the provider's models endpoint using the caller's
// key and returns a normalized list sorted by label. The returned slice is
// never nil — an empty slice with a nil error means the provider responded
// but had nothing to offer.
func FetchProviderModels(ctx context.Context, provider, apiKeyPlain string) ([]ModelInfo, error) {
	var (
		models []ModelInfo
		err    error
	)
	switch provider {
	case "openai":
		models, err = fetchOpenAICompatible(ctx, "https://api.openai.com/v1/models", apiKeyPlain, provider)
	case "deepseek":
		models, err = fetchOpenAICompatible(ctx, "https://api.deepseek.com/models", apiKeyPlain, provider)
	case "together_ai":
		models, err = fetchOpenAICompatible(ctx, "https://api.together.xyz/v1/models", apiKeyPlain, provider)
	case "fireworks_ai":
		models, err = fetchOpenAICompatible(ctx, "https://api.fireworks.ai/inference/v1/models", apiKeyPlain, provider)
	case "openrouter":
		models, err = fetchOpenAICompatible(ctx, "https://openrouter.ai/api/v1/models", apiKeyPlain, provider)
	case "anthropic":
		models, err = fetchAnthropic(ctx, apiKeyPlain)
	case "gemini":
		models, err = fetchGemini(ctx, apiKeyPlain)
	default:
		return nil, fmt.Errorf("dynamic model listing not supported for provider %q", provider)
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Label < models[j].Label })
	if models == nil {
		models = []ModelInfo{}
	}
	return models, nil
}

// openAIListResponse covers the common `{ data: [{ id: "..." }] }` shape used
// by OpenAI and every provider that mimics its API (DeepSeek, Together,
// Fireworks, OpenRouter — which adds an optional `name`).
type openAIListResponse struct {
	Data []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"data"`
}

func fetchOpenAICompatible(ctx context.Context, url, apiKey, provider string) ([]ModelInfo, error) {
	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Accept":        "application/json",
	}
	resp, err := httpclient.DoGetJSON[openAIListResponse](ctx, url, headers, 200)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(resp.Data))
	for _, m := range resp.Data {
		if m.ID == "" || !isChatCapableID(m.ID) {
			continue
		}
		label := m.Name
		if label == "" {
			label = m.ID
		}
		out = append(out, ModelInfo{
			Value:    provider + "/" + m.ID,
			Label:    label,
			Provider: provider,
		})
	}
	return out, nil
}

// isChatCapableID returns true when the model ID looks like a general-purpose
// chat/completion model suitable for driving an agent. The `/v1/models`
// endpoint on OpenAI-compatible APIs returns every model the account can
// touch — embeddings, transcription, TTS, image generation, moderation,
// old fine-tunes — and none of those belong in the agent model picker.
//
// Filter is intentionally permissive: it accepts known chat families and
// drops anything that clearly belongs to another modality. When a provider
// ships a new chat family we don't recognize yet, the user can still type it
// in via the "Custom Model" input.
func isChatCapableID(id string) bool {
	lower := strings.ToLower(id)

	// Drop fine-tunes and org-scoped custom models (colon-separated segments).
	if strings.Contains(id, ":ft-") || strings.Contains(lower, ":ft:") {
		return false
	}

	// Drop known non-chat modalities by substring match.
	nonChat := []string{
		"embedding", "embed-",
		"whisper", "tts", "audio", "transcribe", "realtime", "voice",
		"dall-e", "image", "imagen", "sora", "video",
		"moderation",
		"computer-use",
		"search-preview", "deep-research",
	}
	for _, kw := range nonChat {
		if strings.Contains(lower, kw) {
			return false
		}
	}

	// Drop OpenAI's legacy completion-only families and instruct variants.
	if strings.Contains(lower, "instruct") {
		return false
	}
	legacyPrefixes := []string{"babbage", "davinci", "curie", "ada-", "text-ada", "text-babbage", "text-curie", "text-davinci"}
	for _, p := range legacyPrefixes {
		if strings.HasPrefix(lower, p) {
			return false
		}
	}

	// Accept known chat families. If a provider's naming doesn't match any of
	// these, we still accept it so new families aren't silently hidden —
	// the non-chat keywords above already carry most of the filtering.
	return true
}

type anthropicListResponse struct {
	Data []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Type        string `json:"type"`
	} `json:"data"`
}

func fetchAnthropic(ctx context.Context, apiKey string) ([]ModelInfo, error) {
	headers := map[string]string{
		"x-api-key":         apiKey,
		"anthropic-version": "2023-06-01",
		"Accept":            "application/json",
	}
	resp, err := httpclient.DoGetJSON[anthropicListResponse](ctx, "https://api.anthropic.com/v1/models?limit=1000", headers, 200)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(resp.Data))
	for _, m := range resp.Data {
		if m.ID == "" {
			continue
		}
		label := m.DisplayName
		if label == "" {
			label = m.ID
		}
		out = append(out, ModelInfo{
			Value:    "anthropic/" + m.ID,
			Label:    label,
			Provider: "anthropic",
		})
	}
	return out, nil
}

type geminiListResponse struct {
	Models []struct {
		Name                       string   `json:"name"`
		DisplayName                string   `json:"displayName"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	} `json:"models"`
}

func fetchGemini(ctx context.Context, apiKey string) ([]ModelInfo, error) {
	url := "https://generativelanguage.googleapis.com/v1beta/models?key=" + apiKey + "&pageSize=1000"
	resp, err := httpclient.DoGetJSON[geminiListResponse](ctx, url, map[string]string{"Accept": "application/json"}, 200)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(resp.Models))
	for _, m := range resp.Models {
		// Gemini returns names like "models/gemini-1.5-pro" — strip the prefix.
		id := strings.TrimPrefix(m.Name, "models/")
		if id == "" {
			continue
		}
		// Only include models that can actually be used for chat.
		if !supportsGenerateContent(m.SupportedGenerationMethods) {
			continue
		}
		label := m.DisplayName
		if label == "" {
			label = id
		}
		out = append(out, ModelInfo{
			Value:    "gemini/" + id,
			Label:    label,
			Provider: "gemini",
		})
	}
	return out, nil
}

func supportsGenerateContent(methods []string) bool {
	for _, m := range methods {
		if m == "generateContent" {
			return true
		}
	}
	return false
}
