package config

import (
	"os"
	"testing"
)

// allConfigKeys lists every env var that Load() reads.
var allConfigKeys = []string{
	"HOST", "PORT", "LOG_LEVEL",
	"REFRESH_TOKEN", "PROFILE_ARN", "KIRO_REGION",
	"KIRO_CREDS_FILE", "KIRO_CLI_DB_FILE",
	"PROXY_API_KEY", "VPN_PROXY_URL",
	"STREAMING_READ_TIMEOUT", "FIRST_TOKEN_TIMEOUT", "FIRST_TOKEN_MAX_RETRIES",
	"TOOL_DESCRIPTION_MAX_LENGTH",
	"FAKE_REASONING", "FAKE_REASONING_MAX_TOKENS",
	"FAKE_REASONING_HANDLING", "FAKE_REASONING_INITIAL_BUFFER_SIZE",
	"TRUNCATION_RECOVERY", "DEBUG_MODE",
}

// clearConfigEnvs blanks every config env var so Load() returns defaults.
// t.Setenv saves the original and restores it on cleanup.
func clearConfigEnvs(t *testing.T) {
	t.Helper()
	for _, k := range allConfigKeys {
		t.Setenv(k, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearConfigEnvs(t)

	cfg := Load()

	checks := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"Host", cfg.Host, "0.0.0.0"},
		{"Port", cfg.Port, "8080"},
		{"LogLevel", cfg.LogLevel, "info"},
		{"RefreshToken", cfg.RefreshToken, ""},
		{"ProfileARN", cfg.ProfileARN, ""},
		{"KiroRegion", cfg.KiroRegion, "us-east-1"},
		{"KiroCredsFile", cfg.KiroCredsFile, ""},
		{"KiroCLIDBFile", cfg.KiroCLIDBFile, ""},
		{"ProxyAPIKey", cfg.ProxyAPIKey, ""},
		{"VPNProxyURL", cfg.VPNProxyURL, ""},
		{"StreamingReadTimeout", cfg.StreamingReadTimeout, 300.0},
		{"FirstTokenTimeout", cfg.FirstTokenTimeout, 15.0},
		{"FirstTokenMaxRetries", cfg.FirstTokenMaxRetries, 3},
		{"ToolDescriptionMaxLength", cfg.ToolDescriptionMaxLength, 10000},
		{"FakeReasoning", cfg.FakeReasoning, true},
		{"FakeReasoningMaxTokens", cfg.FakeReasoningMaxTokens, 4000},
		{"FakeReasoningHandling", cfg.FakeReasoningHandling, "as_reasoning_content"},
		{"FakeReasoningInitialBufferSize", cfg.FakeReasoningInitialBufferSize, 20},
		{"TruncationRecovery", cfg.TruncationRecovery, true},
		{"DebugMode", cfg.DebugMode, "off"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoad_AllEnvVars(t *testing.T) {
	clearConfigEnvs(t)
	envs := map[string]string{
		"HOST":                               "127.0.0.1",
		"PORT":                               "9090",
		"LOG_LEVEL":                          "debug",
		"REFRESH_TOKEN":                      "tok-123",
		"PROFILE_ARN":                        "arn:aws:test",
		"KIRO_REGION":                        "eu-central-1",
		"KIRO_CREDS_FILE":                    "/tmp/creds.json",
		"KIRO_CLI_DB_FILE":                   "/tmp/db.sqlite3",
		"PROXY_API_KEY":                      "secret",
		"VPN_PROXY_URL":                      "socks5://127.0.0.1:1080",
		"STREAMING_READ_TIMEOUT":             "600",
		"FIRST_TOKEN_TIMEOUT":                "30",
		"FIRST_TOKEN_MAX_RETRIES":            "5",
		"TOOL_DESCRIPTION_MAX_LENGTH":        "5000",
		"FAKE_REASONING":                     "true",
		"FAKE_REASONING_MAX_TOKENS":          "8000",
		"FAKE_REASONING_HANDLING":            "remove",
		"FAKE_REASONING_INITIAL_BUFFER_SIZE": "40",
		"TRUNCATION_RECOVERY":                "false",
		"DEBUG_MODE":                         "all",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	cfg := Load()

	checks := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"Host", cfg.Host, "127.0.0.1"},
		{"Port", cfg.Port, "9090"},
		{"LogLevel", cfg.LogLevel, "debug"},
		{"RefreshToken", cfg.RefreshToken, "tok-123"},
		{"ProfileARN", cfg.ProfileARN, "arn:aws:test"},
		{"KiroRegion", cfg.KiroRegion, "eu-central-1"},
		{"KiroCredsFile", cfg.KiroCredsFile, "/tmp/creds.json"},
		{"KiroCLIDBFile", cfg.KiroCLIDBFile, "/tmp/db.sqlite3"},
		{"ProxyAPIKey", cfg.ProxyAPIKey, "secret"},
		{"VPNProxyURL", cfg.VPNProxyURL, "socks5://127.0.0.1:1080"},
		{"StreamingReadTimeout", cfg.StreamingReadTimeout, 600.0},
		{"FirstTokenTimeout", cfg.FirstTokenTimeout, 30.0},
		{"FirstTokenMaxRetries", cfg.FirstTokenMaxRetries, 5},
		{"ToolDescriptionMaxLength", cfg.ToolDescriptionMaxLength, 5000},
		{"FakeReasoning", cfg.FakeReasoning, true},
		{"FakeReasoningMaxTokens", cfg.FakeReasoningMaxTokens, 8000},
		{"FakeReasoningHandling", cfg.FakeReasoningHandling, "remove"},
		{"FakeReasoningInitialBufferSize", cfg.FakeReasoningInitialBufferSize, 40},
		{"TruncationRecovery", cfg.TruncationRecovery, false},
		{"DebugMode", cfg.DebugMode, "all"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoad_BoolParsing(t *testing.T) {
	falseInputs := []string{"false", "0", "no", "disabled", "off"}
	trueInputs := []string{"true", "1", "yes", "enabled", "on", "whatever"}

	for _, v := range falseInputs {
		t.Run("false_"+v, func(t *testing.T) {
			clearConfigEnvs(t)
			t.Setenv("FAKE_REASONING", v)
			cfg := Load()
			if cfg.FakeReasoning {
				t.Errorf("FAKE_REASONING=%q: got true, want false", v)
			}
		})
	}
	for _, v := range trueInputs {
		t.Run("true_"+v, func(t *testing.T) {
			clearConfigEnvs(t)
			t.Setenv("FAKE_REASONING", v)
			cfg := Load()
			if !cfg.FakeReasoning {
				t.Errorf("FAKE_REASONING=%q: got false, want true", v)
			}
		})
	}
}

func TestLoad_BoolDefault(t *testing.T) {
	clearConfigEnvs(t)
	cfg := Load()
	if !cfg.FakeReasoning {
		t.Error("FakeReasoning default should be true")
	}
	if !cfg.TruncationRecovery {
		t.Error("TruncationRecovery default should be true")
	}
}

func TestLoad_IntParseFallback(t *testing.T) {
	clearConfigEnvs(t)
	t.Setenv("FIRST_TOKEN_MAX_RETRIES", "not-a-number")
	cfg := Load()
	if cfg.FirstTokenMaxRetries != 3 {
		t.Errorf("expected fallback 3, got %d", cfg.FirstTokenMaxRetries)
	}
}

func TestLoad_FloatParseFallback(t *testing.T) {
	clearConfigEnvs(t)
	t.Setenv("FIRST_TOKEN_TIMEOUT", "invalid")
	cfg := Load()
	if cfg.FirstTokenTimeout != 15 {
		t.Errorf("expected fallback 15, got %f", cfg.FirstTokenTimeout)
	}
}

func TestLoad_InvalidHandlingFallback(t *testing.T) {
	clearConfigEnvs(t)
	t.Setenv("FAKE_REASONING_HANDLING", "bogus")
	cfg := Load()
	if cfg.FakeReasoningHandling != "as_reasoning_content" {
		t.Errorf("expected as_reasoning_content fallback, got %s", cfg.FakeReasoningHandling)
	}
}

func TestLoad_ValidHandlingValues(t *testing.T) {
	for _, v := range []string{"as_reasoning_content", "remove", "pass", "strip_tags"} {
		t.Run(v, func(t *testing.T) {
			clearConfigEnvs(t)
			t.Setenv("FAKE_REASONING_HANDLING", v)
			cfg := Load()
			if cfg.FakeReasoningHandling != v {
				t.Errorf("expected %s, got %s", v, cfg.FakeReasoningHandling)
			}
		})
	}
}

func TestLoad_InvalidDebugModeFallback(t *testing.T) {
	clearConfigEnvs(t)
	t.Setenv("DEBUG_MODE", "invalid")
	cfg := Load()
	if cfg.DebugMode != "off" {
		t.Errorf("expected off fallback, got %s", cfg.DebugMode)
	}
}

func TestLoad_ValidDebugModeValues(t *testing.T) {
	for _, v := range []string{"off", "errors", "all"} {
		t.Run(v, func(t *testing.T) {
			clearConfigEnvs(t)
			t.Setenv("DEBUG_MODE", v)
			cfg := Load()
			if cfg.DebugMode != v {
				t.Errorf("expected %s, got %s", v, cfg.DebugMode)
			}
		})
	}
}

func TestAddr(t *testing.T) {
	cfg := Config{Host: "localhost", Port: "3000"}
	if got := cfg.Addr(); got != "localhost:3000" {
		t.Errorf("Addr() = %q, want %q", got, "localhost:3000")
	}
}

func TestValidate_NoCredentials(t *testing.T) {
	cfg := Config{}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error when no credentials set")
	}
}

func TestValidate_WithRefreshToken(t *testing.T) {
	cfg := Config{RefreshToken: "tok", StreamingReadTimeout: 300, FirstTokenTimeout: 15}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_WithCredsFile(t *testing.T) {
	cfg := Config{
		KiroCredsFile:        "/path/creds.json",
		StreamingReadTimeout: 300,
		FirstTokenTimeout:    15,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_WithCLIDBFile(t *testing.T) {
	cfg := Config{
		KiroCLIDBFile:        "/path/db.sqlite3",
		StreamingReadTimeout: 300,
		FirstTokenTimeout:    15,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_TimeoutWarning(t *testing.T) {
	cfg := Config{
		RefreshToken:         "tok",
		FirstTokenTimeout:    300,
		StreamingReadTimeout: 300,
	}
	// Should not error, only warn.
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPropagateVPNProxy_Empty(t *testing.T) {
	cfg := Config{VPNProxyURL: ""}
	cfg.PropagateVPNProxy()

	// Verify no proxy vars were set by checking they are still absent.
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		if v := os.Getenv(k); v != "" {
			t.Errorf("%s should not be set, got %q", k, v)
		}
	}
}

func TestPropagateVPNProxy_Set(t *testing.T) {
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"} {
		t.Setenv(k, "")
	}

	cfg := Config{VPNProxyURL: "socks5://127.0.0.1:1080"}
	cfg.PropagateVPNProxy()

	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		if got := os.Getenv(k); got != "socks5://127.0.0.1:1080" {
			t.Errorf("%s = %q, want %q", k, got, "socks5://127.0.0.1:1080")
		}
	}
	if got := os.Getenv("NO_PROXY"); got != "127.0.0.1,localhost" {
		t.Errorf("NO_PROXY = %q, want %q", got, "127.0.0.1,localhost")
	}
}

func TestKiroRefreshURL(t *testing.T) {
	got := KiroRefreshURL("us-east-1")
	want := "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAWSSSOOIDCUrl(t *testing.T) {
	got := AWSSSOOIDCUrl("eu-central-1")
	want := "https://oidc.eu-central-1.amazonaws.com/token"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestKiroAPIHost(t *testing.T) {
	got := KiroAPIHost("us-west-2")
	want := "https://q.us-west-2.amazonaws.com"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestKiroQHost(t *testing.T) {
	got := KiroQHost("ap-southeast-1")
	want := "https://q.ap-southeast-1.amazonaws.com"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConstants(t *testing.T) {
	if TokenRefreshThreshold != 600 {
		t.Errorf("TokenRefreshThreshold = %d, want 600", TokenRefreshThreshold)
	}
	if TokenExpiryBuffer != 60 {
		t.Errorf("TokenExpiryBuffer = %d, want 60", TokenExpiryBuffer)
	}
	if MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", MaxRetries)
	}
	if BaseRetryDelay != 1.0 {
		t.Errorf("BaseRetryDelay = %f, want 1.0", BaseRetryDelay)
	}
	if ModelCacheTTL != 3600 {
		t.Errorf("ModelCacheTTL = %d, want 3600", ModelCacheTTL)
	}
	if DefaultMaxInputTokens != 200_000 {
		t.Errorf("DefaultMaxInputTokens = %d, want 200000", DefaultMaxInputTokens)
	}
	if MaxToolNameLength != 64 {
		t.Errorf("MaxToolNameLength = %d, want 64", MaxToolNameLength)
	}
	if AppVersion != "dev" {
		t.Errorf("AppVersion = %q, want %q", AppVersion, "dev")
	}
}

func TestPackageVars(t *testing.T) {
	if v, ok := HiddenModels["claude-3.7-sonnet"]; !ok || v != "CLAUDE_3_7_SONNET_20250219_V1_0" {
		t.Errorf("HiddenModels[claude-3.7-sonnet] = %q, ok=%v", v, ok)
	}
	if v, ok := ModelAliases["auto-kiro"]; !ok || v != "auto" {
		t.Errorf("ModelAliases[auto-kiro] = %q, ok=%v", v, ok)
	}
	if len(HiddenFromList) != 1 || HiddenFromList[0] != "auto" {
		t.Errorf("HiddenFromList = %v", HiddenFromList)
	}
	if len(FallbackModels) != 5 {
		t.Errorf("FallbackModels length = %d, want 5", len(FallbackModels))
	}
	if len(FakeReasoningOpenTags) != 4 || FakeReasoningOpenTags[0] != "<thinking>" {
		t.Errorf("FakeReasoningOpenTags = %v", FakeReasoningOpenTags)
	}
}
