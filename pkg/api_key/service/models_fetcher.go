package service

import (
	"context"
	"fmt"
	"log"
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
	// Two lists on purpose: `current` is what we want to offer, `chatCapable` is
	// the floor geminiModelsOrFallback falls back to.
	current := make([]ModelInfo, 0, len(resp.Models))
	chatCapable := make([]ModelInfo, 0, len(resp.Models))
	for _, m := range resp.Models {
		// Gemini returns names like "models/gemini-1.5-pro" — strip the prefix.
		id := strings.TrimPrefix(m.Name, "models/")
		if id == "" {
			continue
		}
		// Only include models that can actually be used for chat.
		if !supportsMethod(m.SupportedGenerationMethods, "generateContent") {
			continue
		}
		label := m.DisplayName
		if label == "" {
			label = id
		}
		info := ModelInfo{
			Value:    "gemini/" + id,
			Label:    label,
			Provider: "gemini",
		}
		chatCapable = append(chatCapable, info)
		if isCurrentGeminiChatModel(id, m.SupportedGenerationMethods) {
			current = append(current, info)
		}
	}
	return geminiModelsOrFallback(current, chatCapable), nil
}

// geminiModelsOrFallback keeps the stricter filter from becoming an outage.
// isCurrentGeminiChatModel leans on one metadata field Google does not document as
// a contract; if it ever stops arriving, every model is dropped and the endpoint
// answers "supported, nothing to offer" — which the picker reads as "use the
// hardcoded list", i.e. exactly the retired models this fix removes. Serving the
// unfiltered chat-capable set instead is noisier but never empty, and the warning
// is the signal that the filter needs revisiting.
func geminiModelsOrFallback(current, chatCapable []ModelInfo) []ModelInfo {
	if len(current) > 0 || len(chatCapable) == 0 {
		return current
	}
	log.Printf(
		"[CRM-424] gemini: none of the %d chat-capable models passed isCurrentGeminiChatModel; "+
			"serving them unfiltered — check supportedGenerationMethods upstream",
		len(chatCapable),
	)
	return chatCapable
}

func supportsMethod(methods []string, want string) bool {
	for _, m := range methods {
		if m == want {
			return true
		}
	}
	return false
}

// hasSegment reports whether any dash-separated segment of id equals one of want.
// Segment equality, not substring: "exp" also lives inside words like "expert".
func hasSegment(id string, want ...string) bool {
	for _, segment := range strings.Split(strings.ToLower(id), "-") {
		for _, w := range want {
			if segment == w {
				return true
			}
		}
	}
	return false
}

// isCurrentGeminiChatModel keeps only the general-purpose chat models that Gemini's
// live v1beta/models listing still offers. The listing also returns image, TTS,
// transcribe, omni, music, robotics and Gemma models plus dated previews, several of
// which get withdrawn without notice and then fail on every turn. Filtering is on
// metadata rather than a pinned allowlist, so a new stable model appears on its own.
// Three signals:
//
//  1. isChatCapableID — the same by-name modality exclusions the OpenAI-compatible
//     path applies, shared so a new modality is dropped for every provider at once.
//  2. createCachedContent — general-purpose chat models advertise context caching;
//     the specialised ones do not. This is what catches the families the name-based
//     rules above miss (omni, gemma, lyria).
//  3. no preview/experimental segment — withdrawn on Google's schedule, not ours.
//     Stable ids stay, including the `-latest` aliases and versioned snapshots.
func isCurrentGeminiChatModel(id string, methods []string) bool {
	if !isChatCapableID(id) {
		return false
	}
	if !supportsMethod(methods, "createCachedContent") {
		return false
	}
	return !hasSegment(id, "preview", "exp", "experimental")
}
