package service

import "testing"

// Method sets exactly as Gemini's live v1beta/models listing returns them per model
// family. `createCachedContent` is the discriminator: only general-purpose chat
// models advertise it.
var (
	chatMethods    = []string{"generateContent", "countTokens", "createCachedContent", "batchGenerateContent"}
	imageMethods   = []string{"generateContent", "countTokens", "batchGenerateContent"}
	specialMethods = []string{"generateContent", "countTokens"}
)

func TestIsCurrentGeminiChatModel(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		methods []string
		keep    bool
	}{
		// Stable chat models, including the aliases that never rot.
		{"stable chat model", "gemini-2.5-flash", chatMethods, true},
		{"stable lite variant", "gemini-2.5-flash-lite", chatMethods, true},
		{"future stable model is picked up without a code change", "gemini-3.7-flash", chatMethods, true},
		{"rolling alias", "gemini-flash-latest", chatMethods, true},

		// Specialised models the listing also returns. Image is caught by the shared
		// name rules; omni and gemma only by the missing cache support.
		{"image model", "gemini-2.5-flash-image", imageMethods, false},
		{"transcribe model", "gemini-3.5-transcribe", specialMethods, false},
		{"omni model", "gemini-omni-1.1-flash", specialMethods, false},
		{"gemma family", "gemma-4-31b-it", specialMethods, false},
		{"computer use", "gemini-2.5-computer-use-preview-10-2025", specialMethods, false},
		{"deep research", "deep-research-pro-preview-12-2025", specialMethods, false},

		// Preview/experimental variants are withdrawn on Google's schedule and start
		// failing mid-flight, so they go even when they do support caching.
		{"dated preview with cache support", "gemini-3-flash-preview", chatMethods, false},
		{"preview with a trailing qualifier", "gemini-3.1-pro-preview-customtools", chatMethods, false},
		{"experimental snapshot", "gemini-2.0-pro-exp-02-05", chatMethods, false},
		{"bare exp alias", "gemini-exp-1206", chatMethods, false},

		// Regression guard: the rule matches whole dash-separated segments. A
		// substring check on "exp" would also drop any stable id containing a word
		// like "expert", and a released model would silently vanish from the picker.
		{"exp inside a word is not an experimental marker", "gemini-3-expert-flash", chatMethods, true},

		// Same guard for a model that merely mentions caching-less siblings: without
		// generateContent it never reaches this function, so nothing to assert there.
		{"stable model whose name contains preview-like text", "gemini-3-previewer", chatMethods, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isCurrentGeminiChatModel(c.id, c.methods); got != c.keep {
				t.Errorf("isCurrentGeminiChatModel(%q) = %v, want %v", c.id, got, c.keep)
			}
		})
	}
}

// The filter reads a metadata field Google does not document as a contract. If it
// ever stops arriving, dropping every model would hand the picker an empty list —
// and the picker answers an empty list by falling back to its hardcoded one, which
// is where the retired models live. Degrade to the unfiltered set instead.
func TestGeminiModelsOrFallback(t *testing.T) {
	chatCapable := []ModelInfo{
		{Value: "gemini/gemini-2.5-flash", Label: "Gemini 2.5 Flash", Provider: "gemini"},
		{Value: "gemini/gemini-3-flash-preview", Label: "Gemini 3 Flash Preview", Provider: "gemini"},
	}
	current := chatCapable[:1]

	if got := geminiModelsOrFallback(current, chatCapable); len(got) != 1 {
		t.Errorf("filtered list must win when it has anything: got %d models, want 1", len(got))
	}
	if got := geminiModelsOrFallback(nil, chatCapable); len(got) != len(chatCapable) {
		t.Errorf("an empty filter result must fall back, not starve the picker: got %d models, want %d",
			len(got), len(chatCapable))
	}
	if got := geminiModelsOrFallback(nil, nil); len(got) != 0 {
		t.Errorf("nothing in, nothing out: got %d models, want 0", len(got))
	}
}
