// Package config handles application configuration via environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

const (
	defaultFakeReasoningHandling = "as_reasoning_content"
	defaultDebugMode             = "off"
)

const (
	// TokenRefreshThreshold is seconds before expiry to trigger a refresh.
	TokenRefreshThreshold = 600
	// TokenExpiryBuffer is seconds subtracted from expiresIn for safety.
	TokenExpiryBuffer = 60
	// MaxRetries is the default retry count for non-streaming requests.
	MaxRetries = 3
	// BaseRetryDelay is the base delay in seconds for exponential backoff.
	BaseRetryDelay = 1.0
	// ModelCacheTTL is how long (seconds) the model cache stays valid.
	ModelCacheTTL = 3600
	// DefaultMaxInputTokens is the fallback max input token count.
	DefaultMaxInputTokens = 200_000
	// MaxToolNameLength is the maximum allowed tool name length.
	MaxToolNameLength = 64
)

// AppVersion is the application version string, injected at build time via
// ldflags: -X github.com/miltonparedes/giro/internal/config.AppVersion=...
var AppVersion = "dev"

// Connection pool settings for http.Transport.
const (
	MaxIdleConns        = 100
	MaxIdleConnsPerHost = 20
	IdleConnTimeout     = 30 * time.Second
	ConnectTimeout      = 30 * time.Second
	WriteTimeout        = 30 * time.Second
	PoolTimeout         = 30 * time.Second
)

const (
	kiroRefreshURLTemplate = "https://prod.%s.auth.desktop.kiro.dev/refreshToken"
	awsSSOOIDCURLTemplate  = "https://oidc.%s.amazonaws.com/token"
	kiroAPIHostTemplate    = "https://q.%s.amazonaws.com"
	kiroQHostTemplate      = "https://q.%s.amazonaws.com"
)

// HiddenModels maps display names to internal Kiro model IDs for models not
// advertised by /ListAvailableModels but still functional.
var HiddenModels = map[string]string{
	"claude-3.7-sonnet": "CLAUDE_3_7_SONNET_20250219_V1_0",
}

// ModelAliases maps custom alias names to real model IDs.
var ModelAliases = map[string]string{
	"auto-kiro": "auto",
}

// HiddenFromList contains models hidden from /v1/models but still usable.
var HiddenFromList = []string{"auto"}

// FallbackModels is the list used when /ListAvailableModels is unreachable.
var FallbackModels = []string{
	"auto",
	"claude-sonnet-4",
	"claude-haiku-4.5",
	"claude-sonnet-4.5",
	"claude-opus-4.5",
}

// FakeReasoningOpenTags are the opening tags checked for thinking detection.
var FakeReasoningOpenTags = []string{"<thinking>", "<think>", "<reasoning>", "<thought>"}

// Config holds the application configuration.
type Config struct {
	Host     string
	Port     string
	LogLevel string

	// Credentials.
	RefreshToken  string //nolint:gosec // G117: not a hardcoded credential
	ProfileARN    string
	KiroRegion    string
	KiroCredsFile string
	KiroCLIDBFile string

	// Proxy settings.
	ProxyAPIKey string //nolint:gosec // not a hardcoded credential
	VPNProxyURL string

	// Timeouts.
	StreamingReadTimeout float64
	FirstTokenTimeout    float64
	FirstTokenMaxRetries int

	// Tool handling.
	ToolDescriptionMaxLength int

	// Fake reasoning.
	FakeReasoning                  bool
	FakeReasoningMaxTokens         int
	FakeReasoningHandling          string
	FakeReasoningInitialBufferSize int

	// Features.
	TruncationRecovery bool
	DebugMode          string
}

// Addr returns the host:port address string.
func (c Config) Addr() string {
	return c.Host + ":" + c.Port
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	handling := getEnv("FAKE_REASONING_HANDLING", defaultFakeReasoningHandling)
	if !isValidHandling(handling) {
		handling = defaultFakeReasoningHandling
	}

	debugMode := getEnv("DEBUG_MODE", defaultDebugMode)
	if !isValidDebugMode(debugMode) {
		debugMode = defaultDebugMode
	}

	return Config{
		Host:     getEnv("HOST", "0.0.0.0"),
		Port:     getEnv("PORT", "8080"),
		LogLevel: getEnv("LOG_LEVEL", "info"),

		RefreshToken:  getEnv("REFRESH_TOKEN", ""),
		ProfileARN:    getEnv("PROFILE_ARN", ""),
		KiroRegion:    getEnv("KIRO_REGION", "us-east-1"),
		KiroCredsFile: getEnv("KIRO_CREDS_FILE", ""),
		KiroCLIDBFile: getEnv("KIRO_CLI_DB_FILE", ""),

		ProxyAPIKey: getEnv("PROXY_API_KEY", ""),
		VPNProxyURL: getEnv("VPN_PROXY_URL", ""),

		StreamingReadTimeout: getEnvFloat("STREAMING_READ_TIMEOUT", 300),
		FirstTokenTimeout:    getEnvFloat("FIRST_TOKEN_TIMEOUT", 15),
		FirstTokenMaxRetries: getEnvInt("FIRST_TOKEN_MAX_RETRIES", 3),

		ToolDescriptionMaxLength: getEnvInt("TOOL_DESCRIPTION_MAX_LENGTH", 10000),

		FakeReasoning:                  getEnvBool("FAKE_REASONING", true),
		FakeReasoningMaxTokens:         getEnvInt("FAKE_REASONING_MAX_TOKENS", 4000),
		FakeReasoningHandling:          handling,
		FakeReasoningInitialBufferSize: getEnvInt("FAKE_REASONING_INITIAL_BUFFER_SIZE", 20),

		TruncationRecovery: getEnvBool("TRUNCATION_RECOVERY", true),
		DebugMode:          debugMode,
	}
}

// Validate checks that the configuration is usable. It returns an error if no
// credential source is configured and logs a warning when timeout values look
// suspect.
func (c Config) Validate() error {
	if c.RefreshToken == "" && c.KiroCredsFile == "" && c.KiroCLIDBFile == "" {
		return errors.New("at least one credential source required: set REFRESH_TOKEN, KIRO_CREDS_FILE, or KIRO_CLI_DB_FILE")
	}
	if c.FirstTokenTimeout >= c.StreamingReadTimeout {
		slog.Warn("suboptimal timeout configuration: FIRST_TOKEN_TIMEOUT should be less than STREAMING_READ_TIMEOUT",
			"first_token_timeout", c.FirstTokenTimeout,
			"streaming_read_timeout", c.StreamingReadTimeout,
		)
	}
	return nil
}

// PropagateVPNProxy sets HTTP_PROXY, HTTPS_PROXY, ALL_PROXY and NO_PROXY
// environment variables when VPNProxyURL is configured.
func (c Config) PropagateVPNProxy() {
	if c.VPNProxyURL == "" {
		return
	}
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		_ = os.Setenv(k, c.VPNProxyURL)
	}
	_ = os.Setenv("NO_PROXY", "127.0.0.1,localhost")
}

// KiroRefreshURL returns the Kiro Desktop Auth token refresh URL for a region.
func KiroRefreshURL(region string) string {
	return fmt.Sprintf(kiroRefreshURLTemplate, region)
}

// AWSSSOOIDCUrl returns the AWS SSO OIDC token URL for a region.
func AWSSSOOIDCUrl(region string) string {
	return fmt.Sprintf(awsSSOOIDCURLTemplate, region)
}

// KiroAPIHost returns the Kiro API host for a region.
func KiroAPIHost(region string) string {
	return fmt.Sprintf(kiroAPIHostTemplate, region)
}

// KiroQHost returns the Kiro Q API host for a region.
func KiroQHost(region string) string {
	return fmt.Sprintf(kiroQHostTemplate, region)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch v {
	case "false", "0", "no", "disabled", "off":
		return false
	default:
		return true
	}
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func isValidHandling(s string) bool {
	switch s {
	case "as_reasoning_content", "remove", "pass", "strip_tags":
		return true
	}
	return false
}

func isValidDebugMode(s string) bool {
	switch s {
	case "off", "errors", "all":
		return true
	}
	return false
}
