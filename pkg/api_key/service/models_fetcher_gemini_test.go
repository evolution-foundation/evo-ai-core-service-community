package service

import "testing"

// TestIsCurrentGeminiChatModel documenta o ESTUDO da situação atual (CRM-424) contra o
// RETORNO REAL da API v1beta/models (verificado ao vivo: 53 modelos, 39 com
// generateContent, 11 de chat geral). O filtro é por metadata (createCachedContent +
// não-preview/exp), não por lista fixa — modelo novo aparece sozinho, problemático some
// sozinho. As method-sets abaixo são as que a API realmente devolve por tipo.
func TestIsCurrentGeminiChatModel(t *testing.T) {
	// Chat geral: generateContent + countTokens + createCachedContent + batch.
	chat := []string{"generateContent", "countTokens", "createCachedContent", "batchGenerateContent"}
	// Imagem: generateContent + countTokens + batch, SEM cache.
	image := []string{"generateContent", "countTokens", "batchGenerateContent"}
	// Transcribe/omni/gemma/tts: generateContent + countTokens, SEM cache nem batch.
	special := []string{"generateContent", "countTokens"}

	type tc struct {
		id      string
		methods []string
		keep    bool
	}
	cases := []tc{
		// vigentes de chat — DEVEM aparecer (incl. modelos novos e aliases -latest)
		{"gemini-2.5-flash", chat, true},
		{"gemini-2.5-pro", chat, true},
		{"gemini-2.5-flash-lite", chat, true},
		{"gemini-3.1-flash-lite", chat, true},
		{"gemini-3.5-flash", chat, true},
		{"gemini-3.5-flash-lite", chat, true},
		{"gemini-3.6-flash", chat, true},
		{"gemini-3.7-flash", chat, true},
		{"gemini-flash-latest", chat, true},
		{"gemini-flash-lite-latest", chat, true},
		{"gemini-pro-latest", chat, true},

		// imagem — sem createCachedContent → fora (o marcador de nome NÃO pegava "-image")
		{"gemini-2.5-flash-image", image, false},
		{"gemini-3-pro-image", image, false},
		{"gemini-3.1-flash-image", image, false},
		{"gemini-3.1-flash-lite-image", image, false},

		// especializados sem cache — fora
		{"gemini-3.5-transcribe", special, false},
		{"gemini-omni-1.1-flash", special, false},
		{"gemma-4-31b-it", special, false},
		{"gemma-4-26b-a4b-it", special, false},

		// preview/experimental — fora mesmo TENDO cache (retirados, viram erro)
		{"gemini-3-flash-preview", chat, false},
		{"gemini-3.1-pro-preview", chat, false},
		{"gemini-3.1-pro-preview-customtools", chat, false},
		{"gemini-3.1-flash-lite-preview", chat, false},
		{"gemini-robotics-er-2-preview", chat, false},
		{"gemini-2.5-flash-preview-tts", special, false},
		{"gemini-2.5-pro-preview-tts", image, false},
		{"gemini-2.5-computer-use-preview-10-2025", special, false},
		{"deep-research-pro-preview-12-2025", special, false},
		{"lyria-3-pro-preview", special, false},
		{"nano-banana-pro-preview", image, false},
		{"antigravity-preview-05-2026", special, false},
	}

	for _, c := range cases {
		got := isCurrentGeminiChatModel(c.id, c.methods)
		if got != c.keep {
			t.Errorf("isCurrentGeminiChatModel(%q) = %v, quer %v", c.id, got, c.keep)
		}
	}
}
