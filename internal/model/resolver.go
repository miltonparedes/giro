package model

import (
	"regexp"
	"sort"
	"strings"
)

// Compiled regex patterns for model name normalization, applied in order.
var (
	// Pattern 1: claude-haiku-4-5 → claude-haiku-4.5 (dash-to-dot, strip date/latest)
	patStandard = regexp.MustCompile(
		`^(claude-(?:haiku|sonnet|opus)-\d+)-(\d{1,2})(?:-(?:\d{8}|latest|\d+))?$`,
	)
	// Pattern 2: claude-sonnet-4-20250514 → claude-sonnet-4 (no minor, strip date)
	patNoMinor = regexp.MustCompile(
		`^(claude-(?:haiku|sonnet|opus)-\d+)(?:-\d{8})?$`,
	)
	// Pattern 3: claude-3-7-sonnet-20250219 → claude-3.7-sonnet (legacy format)
	patLegacy = regexp.MustCompile(
		`^(claude)-(\d+)-(\d+)-(haiku|sonnet|opus)(?:-(?:\d{8}|latest|\d+))?$`,
	)
	// Pattern 4: claude-haiku-4.5-20251001 → claude-haiku-4.5 (dot with date)
	patDotDate = regexp.MustCompile(
		`^(claude-(?:\d+\.\d+-)?(?:haiku|sonnet|opus)(?:-\d+\.\d+)?)-\d{8}$`,
	)
	// Pattern 5: claude-4.5-opus-high → claude-opus-4.5 (inverted with suffix)
	patInverted = regexp.MustCompile(
		`^claude-(\d+)\.(\d+)-(haiku|sonnet|opus)-(.+)$`,
	)
)

// NormalizeModelName converts various client model name formats to canonical Kiro names.
func NormalizeModelName(name string) string {
	if name == "" {
		return name
	}

	lower := strings.ToLower(name)

	if m := patStandard.FindStringSubmatch(lower); m != nil {
		return m[1] + "." + m[2]
	}
	if m := patNoMinor.FindStringSubmatch(lower); m != nil {
		return m[1]
	}
	if m := patLegacy.FindStringSubmatch(lower); m != nil {
		return m[1] + "-" + m[2] + "." + m[3] + "-" + m[4]
	}
	if m := patDotDate.FindStringSubmatch(lower); m != nil {
		return m[1]
	}
	if m := patInverted.FindStringSubmatch(lower); m != nil {
		return "claude-" + m[3] + "-" + m[1] + "." + m[2]
	}

	return name
}

// Resolution describes how an external model name was resolved.
type Resolution struct {
	ExternalModel string // original name from the client
	ResolvedModel string // name after alias + normalization
	InternalID    string // non-empty only for hidden models
	Source        string // "alias", "cache", "hidden", "passthrough"
}

// Resolver resolves external model names through a 4-layer pipeline.
type Resolver struct {
	cache          *InfoCache
	hiddenModels   map[string]string // displayName → internalID
	aliases        map[string]string // alias → target
	hiddenFromList map[string]struct{}
}

// NewResolver creates a resolver backed by the given cache and lookup maps.
func NewResolver(
	cache *InfoCache,
	hiddenModels map[string]string,
	aliases map[string]string,
	hiddenFromList []string,
) *Resolver {
	hfl := make(map[string]struct{}, len(hiddenFromList))
	for _, id := range hiddenFromList {
		hfl[id] = struct{}{}
	}
	return &Resolver{
		cache:          cache,
		hiddenModels:   hiddenModels,
		aliases:        aliases,
		hiddenFromList: hfl,
	}
}

// Resolve translates an external model name into a Resolution.
// It never returns an error — unknown models pass through to Kiro.
func (r *Resolver) Resolve(externalModel string) Resolution {
	resolved := externalModel

	// Layer 0: alias
	if target, ok := r.aliases[externalModel]; ok {
		resolved = target
	}

	// Layer 1: normalize
	normalized := NormalizeModelName(resolved)

	// Layer 2: dynamic cache
	if r.cache.IsValid(normalized) {
		return Resolution{
			ExternalModel: externalModel,
			ResolvedModel: normalized,
			Source:        "cache",
		}
	}

	// Layer 3: hidden models
	if internalID, ok := r.hiddenModels[normalized]; ok {
		return Resolution{
			ExternalModel: externalModel,
			ResolvedModel: normalized,
			InternalID:    internalID,
			Source:        "hidden",
		}
	}

	// Layer 4: passthrough
	return Resolution{
		ExternalModel: externalModel,
		ResolvedModel: normalized,
		Source:        "passthrough",
	}
}

// GetAvailableModels returns a sorted list of model IDs suitable for /v1/models.
func (r *Resolver) GetAvailableModels() []string {
	seen := make(map[string]struct{})

	for _, id := range r.cache.GetAllModelIDs() {
		seen[id] = struct{}{}
	}
	for name := range r.hiddenModels {
		seen[name] = struct{}{}
	}
	for id := range r.hiddenFromList {
		delete(seen, id)
	}
	for alias := range r.aliases {
		seen[alias] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
