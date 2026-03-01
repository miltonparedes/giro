package model_test

import (
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/miltonparedes/giro/internal/model"
)

func seedCache(t *testing.T) *model.InfoCache {
	t.Helper()
	c := model.NewInfoCache(time.Hour)
	c.Update([]model.Info{
		{ModelID: "claude-sonnet-4", MaxInputTokens: 200000},
		{ModelID: "claude-haiku-4.5", MaxInputTokens: 180000},
	})
	return c
}

func TestCache_UpdateAndGet(t *testing.T) {
	c := seedCache(t)

	m, ok := c.Get("claude-sonnet-4")
	if !ok {
		t.Fatal("expected claude-sonnet-4 in cache")
	}
	if m.MaxInputTokens != 200000 {
		t.Errorf("MaxInputTokens = %d, want 200000", m.MaxInputTokens)
	}

	_, ok = c.Get("nonexistent")
	if ok {
		t.Error("expected nonexistent to be missing")
	}
}

func TestCache_UpdateReplacesAll(t *testing.T) {
	c := seedCache(t)

	c.Update([]model.Info{
		{ModelID: "new-model", MaxInputTokens: 100000},
	})

	if c.IsValid("claude-sonnet-4") {
		t.Error("old model should have been replaced")
	}
	if !c.IsValid("new-model") {
		t.Error("new model should be present")
	}
}

func TestCache_IsValid(t *testing.T) {
	c := seedCache(t)

	if !c.IsValid("claude-sonnet-4") {
		t.Error("expected claude-sonnet-4 to be valid")
	}
	if c.IsValid("unknown") {
		t.Error("expected unknown to be invalid")
	}
}

func TestCache_IsStale(t *testing.T) {
	c := model.NewInfoCache(50 * time.Millisecond)

	if !c.IsStale() {
		t.Error("never-updated cache should be stale")
	}

	c.Update([]model.Info{{ModelID: "a"}})
	if c.IsStale() {
		t.Error("freshly updated cache should not be stale")
	}

	time.Sleep(60 * time.Millisecond)
	if !c.IsStale() {
		t.Error("cache should be stale after TTL")
	}
}

func TestCache_AddHiddenModel(t *testing.T) {
	c := model.NewInfoCache(time.Hour)
	c.AddHiddenModel("claude-3.7-sonnet", "CLAUDE_3_7_SONNET_20250219_V1_0")

	m, ok := c.Get("claude-3.7-sonnet")
	if !ok {
		t.Fatal("hidden model not found")
	}
	if !m.IsHidden {
		t.Error("expected IsHidden=true")
	}
	if m.InternalID != "CLAUDE_3_7_SONNET_20250219_V1_0" {
		t.Errorf("InternalID = %q, want CLAUDE_3_7_SONNET_20250219_V1_0", m.InternalID)
	}
	if m.MaxInputTokens != model.DefaultMaxInputTokens {
		t.Errorf("MaxInputTokens = %d, want %d", m.MaxInputTokens, model.DefaultMaxInputTokens)
	}
}

func TestCache_GetMaxInputTokens(t *testing.T) {
	c := seedCache(t)

	if got := c.GetMaxInputTokens("claude-haiku-4.5"); got != 180000 {
		t.Errorf("GetMaxInputTokens(claude-haiku-4.5) = %d, want 180000", got)
	}
	if got := c.GetMaxInputTokens("unknown"); got != model.DefaultMaxInputTokens {
		t.Errorf("GetMaxInputTokens(unknown) = %d, want %d", got, model.DefaultMaxInputTokens)
	}
}

func TestCache_GetMaxInputTokens_ZeroFallback(t *testing.T) {
	c := model.NewInfoCache(time.Hour)
	c.Update([]model.Info{
		{ModelID: "zero-model", MaxInputTokens: 0},
	})
	if got := c.GetMaxInputTokens("zero-model"); got != model.DefaultMaxInputTokens {
		t.Errorf("GetMaxInputTokens(zero-model) = %d, want default %d", got, model.DefaultMaxInputTokens)
	}
}

func TestCache_GetAllModelIDs(t *testing.T) {
	c := seedCache(t)
	ids := c.GetAllModelIDs()
	sort.Strings(ids)

	want := []string{"claude-haiku-4.5", "claude-sonnet-4"}
	if len(ids) != len(want) {
		t.Fatalf("GetAllModelIDs len = %d, want %d", len(ids), len(want))
	}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, id, want[i])
		}
	}
}

func TestCache_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	c := model.NewInfoCache(time.Hour)
	c.Update([]model.Info{
		{ModelID: "m1", MaxInputTokens: 100},
	})

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			c.Get("m1")
			c.IsValid("m1")
			c.GetMaxInputTokens("m1")
			c.GetAllModelIDs()
			c.IsStale()
		}()
		go func() {
			defer wg.Done()
			c.Update([]model.Info{{ModelID: "m1", MaxInputTokens: 200}})
		}()
		go func() {
			defer wg.Done()
			c.AddHiddenModel("h1", "INTERNAL")
		}()
	}
	wg.Wait()
}
