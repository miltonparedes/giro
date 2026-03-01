package model_test

import (
	"testing"
	"time"

	"github.com/miltonparedes/giro/internal/model"
)

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Pattern 1 — dash to dot, strip date/latest
		{"claude-haiku-4-5", "claude-haiku-4.5"},
		{"claude-sonnet-4-5", "claude-sonnet-4.5"},
		{"claude-opus-4-5", "claude-opus-4.5"},
		{"claude-haiku-4-5-20251001", "claude-haiku-4.5"},
		{"claude-haiku-4-5-latest", "claude-haiku-4.5"},
		{"claude-sonnet-4-6", "claude-sonnet-4.6"},

		// Pattern 2 — no minor, optional date
		{"claude-sonnet-4", "claude-sonnet-4"},
		{"claude-sonnet-4-20250514", "claude-sonnet-4"},
		{"claude-opus-4", "claude-opus-4"},

		// Pattern 3 — legacy format
		{"claude-3-7-sonnet-20250219", "claude-3.7-sonnet"},
		{"claude-3-7-sonnet", "claude-3.7-sonnet"},
		{"claude-3-5-haiku-20241022", "claude-3.5-haiku"},
		{"claude-3-5-haiku", "claude-3.5-haiku"},

		// Pattern 4 — dot with date suffix
		{"claude-haiku-4.5-20251001", "claude-haiku-4.5"},
		{"claude-3.7-sonnet-20250219", "claude-3.7-sonnet"},

		// Pattern 5 — inverted with required suffix
		{"claude-4.5-opus-high", "claude-opus-4.5"},
		{"claude-4.5-sonnet-low", "claude-sonnet-4.5"},

		// Already normalized — passthrough
		{"claude-sonnet-4", "claude-sonnet-4"},
		{"claude-haiku-4.5", "claude-haiku-4.5"},
		{"claude-3.7-sonnet", "claude-3.7-sonnet"},

		// Non-Claude — passthrough
		{"auto", "auto"},
		{"gpt-4", "gpt-4"},
		{"llama-3", "llama-3"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := model.NormalizeModelName(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeModelName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func newTestResolver(t *testing.T) *model.Resolver {
	t.Helper()
	cache := model.NewInfoCache(time.Hour)
	cache.Update([]model.Info{
		{ModelID: "claude-sonnet-4", MaxInputTokens: 200000},
		{ModelID: "claude-haiku-4.5", MaxInputTokens: 200000},
		{ModelID: "auto", MaxInputTokens: 200000},
	})

	hidden := map[string]string{
		"claude-3.7-sonnet": "CLAUDE_3_7_SONNET_20250219_V1_0",
	}
	aliases := map[string]string{
		"auto-kiro": "auto",
	}
	hiddenFromList := []string{"auto"}

	return model.NewResolver(cache, hidden, aliases, hiddenFromList)
}

func TestResolve_Alias(t *testing.T) {
	r := newTestResolver(t)
	res := r.Resolve("auto-kiro")

	if res.Source != "cache" {
		t.Errorf("Source = %q, want %q", res.Source, "cache")
	}
	if res.ResolvedModel != "auto" {
		t.Errorf("ResolvedModel = %q, want %q", res.ResolvedModel, "auto")
	}
}

func TestResolve_CacheHit(t *testing.T) {
	r := newTestResolver(t)
	res := r.Resolve("claude-sonnet-4")

	if res.Source != "cache" {
		t.Errorf("Source = %q, want %q", res.Source, "cache")
	}
	if res.ResolvedModel != "claude-sonnet-4" {
		t.Errorf("ResolvedModel = %q, want %q", res.ResolvedModel, "claude-sonnet-4")
	}
}

func TestResolve_CacheHitAfterNormalization(t *testing.T) {
	r := newTestResolver(t)
	res := r.Resolve("claude-haiku-4-5-20251001")

	if res.Source != "cache" {
		t.Errorf("Source = %q, want %q", res.Source, "cache")
	}
	if res.ResolvedModel != "claude-haiku-4.5" {
		t.Errorf("ResolvedModel = %q, want %q", res.ResolvedModel, "claude-haiku-4.5")
	}
}

func TestResolve_HiddenModel(t *testing.T) {
	r := newTestResolver(t)
	res := r.Resolve("claude-3.7-sonnet")

	if res.Source != "hidden" {
		t.Errorf("Source = %q, want %q", res.Source, "hidden")
	}
	if res.InternalID != "CLAUDE_3_7_SONNET_20250219_V1_0" {
		t.Errorf("InternalID = %q, want CLAUDE_3_7_SONNET_20250219_V1_0", res.InternalID)
	}
}

func TestResolve_HiddenModelViaNormalization(t *testing.T) {
	r := newTestResolver(t)
	res := r.Resolve("claude-3-7-sonnet-20250219")

	if res.Source != "hidden" {
		t.Errorf("Source = %q, want %q", res.Source, "hidden")
	}
	if res.ResolvedModel != "claude-3.7-sonnet" {
		t.Errorf("ResolvedModel = %q, want %q", res.ResolvedModel, "claude-3.7-sonnet")
	}
}

func TestResolve_Passthrough(t *testing.T) {
	r := newTestResolver(t)
	res := r.Resolve("gpt-4")

	if res.Source != "passthrough" {
		t.Errorf("Source = %q, want %q", res.Source, "passthrough")
	}
	if res.ResolvedModel != "gpt-4" {
		t.Errorf("ResolvedModel = %q, want %q", res.ResolvedModel, "gpt-4")
	}
}

func TestGetAvailableModels(t *testing.T) {
	r := newTestResolver(t)
	models := r.GetAvailableModels()

	// Expected: cache (claude-sonnet-4, claude-haiku-4.5, auto) + hidden (claude-3.7-sonnet)
	//           - hiddenFromList (auto) + aliases (auto-kiro)
	want := []string{
		"auto-kiro",
		"claude-3.7-sonnet",
		"claude-haiku-4.5",
		"claude-sonnet-4",
	}

	if len(models) != len(want) {
		t.Fatalf("GetAvailableModels() len = %d, want %d\ngot: %v", len(models), len(want), models)
	}
	for i, id := range models {
		if id != want[i] {
			t.Errorf("models[%d] = %q, want %q", i, id, want[i])
		}
	}
}

func TestGetAvailableModels_Sorted(t *testing.T) {
	r := newTestResolver(t)
	models := r.GetAvailableModels()

	for i := 1; i < len(models); i++ {
		if models[i] < models[i-1] {
			t.Errorf("not sorted: %q before %q", models[i-1], models[i])
		}
	}
}

func TestResolve_ThreadSafe(t *testing.T) {
	r := newTestResolver(t)

	done := make(chan struct{})
	for range 50 {
		go func() {
			r.Resolve("claude-sonnet-4")
			r.Resolve("auto-kiro")
			r.Resolve("claude-3-7-sonnet")
			r.Resolve("unknown")
			r.GetAvailableModels()
			done <- struct{}{}
		}()
	}
	for range 50 {
		<-done
	}
}
