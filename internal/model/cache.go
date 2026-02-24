// Package model provides model metadata caching and name resolution.
package model

import (
	"sync"
	"time"
)

// DefaultMaxInputTokens is the fallback token limit when a model's limit is unknown.
const DefaultMaxInputTokens = 200000

// Info holds metadata for a single model.
type Info struct {
	ModelID        string
	MaxInputTokens int
	IsHidden       bool
	InternalID     string // non-empty for hidden models
}

// InfoCache is a thread-safe cache for model metadata from the Kiro API.
type InfoCache struct {
	mu         sync.RWMutex
	models     map[string]Info
	lastUpdate time.Time
	cacheTTL   time.Duration
}

// NewInfoCache creates a new cache with the given TTL.
func NewInfoCache(ttl time.Duration) *InfoCache {
	return &InfoCache{
		models:   make(map[string]Info),
		cacheTTL: ttl,
	}
}

// Update replaces all cached models and resets the staleness clock.
func (c *InfoCache) Update(models []Info) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.models = make(map[string]Info, len(models))
	for _, m := range models {
		c.models[m.ModelID] = m
	}
	c.lastUpdate = time.Now()
}

// Get returns the Info for the given ID, if present.
func (c *InfoCache) Get(id string) (Info, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	m, ok := c.models[id]
	return m, ok
}

// IsValid returns true when the model exists in the cache.
func (c *InfoCache) IsValid(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	_, ok := c.models[id]
	return ok
}

// AddHiddenModel inserts a hidden model with DefaultMaxInputTokens.
func (c *InfoCache) AddHiddenModel(displayName, internalID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.models[displayName] = Info{
		ModelID:        displayName,
		MaxInputTokens: DefaultMaxInputTokens,
		IsHidden:       true,
		InternalID:     internalID,
	}
}

// GetMaxInputTokens returns the token limit for a model, or DefaultMaxInputTokens.
func (c *InfoCache) GetMaxInputTokens(id string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if m, ok := c.models[id]; ok && m.MaxInputTokens > 0 {
		return m.MaxInputTokens
	}
	return DefaultMaxInputTokens
}

// GetAllModelIDs returns every ModelID in the cache.
func (c *InfoCache) GetAllModelIDs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(c.models))
	for id := range c.models {
		ids = append(ids, id)
	}
	return ids
}

// IsStale reports whether the cache has exceeded its TTL.
func (c *InfoCache) IsStale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.lastUpdate.IsZero() {
		return true
	}
	return time.Since(c.lastUpdate) > c.cacheTTL
}
