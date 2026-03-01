//go:build e2e_real

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/miltonparedes/giro/internal/auth"
	"github.com/miltonparedes/giro/internal/config"
	"github.com/miltonparedes/giro/internal/model"
	"github.com/miltonparedes/giro/internal/server"
)

func TestE2EReal_OpenAIAndAnthropicSmoke(t *testing.T) {
	refreshToken := os.Getenv("GIRO_E2E_REAL_REFRESH_TOKEN")
	credsFile := os.Getenv("GIRO_E2E_REAL_CREDS_FILE")
	sqliteDB := os.Getenv("GIRO_E2E_REAL_SQLITE_DB")
	if refreshToken == "" && credsFile == "" && sqliteDB == "" {
		t.Skip("set GIRO_E2E_REAL_REFRESH_TOKEN or GIRO_E2E_REAL_CREDS_FILE or GIRO_E2E_REAL_SQLITE_DB to run real e2e")
	}

	region := os.Getenv("GIRO_E2E_REAL_REGION")
	if region == "" {
		region = "us-east-1"
	}
	proxyAPIKey := os.Getenv("GIRO_E2E_REAL_PROXY_API_KEY")
	if proxyAPIKey == "" {
		proxyAPIKey = "e2e-real-key"
	}
	modelID := os.Getenv("GIRO_E2E_REAL_MODEL")
	if modelID == "" {
		modelID = "auto"
	}

	authManager, err := auth.NewKiroAuthManager(auth.Options{
		RefreshToken: refreshToken,
		ProfileARN:   os.Getenv("GIRO_E2E_REAL_PROFILE_ARN"),
		Region:       region,
		CredsFile:    credsFile,
		SQLiteDB:     sqliteDB,
		VPNProxyURL:  os.Getenv("GIRO_E2E_REAL_VPN_PROXY_URL"),
	})
	if err != nil {
		t.Fatalf("NewKiroAuthManager: %v", err)
	}

	cache := model.NewInfoCache(time.Hour)
	fallback := make([]model.Info, 0, len(config.FallbackModels))
	for _, id := range config.FallbackModels {
		fallback = append(fallback, model.Info{ModelID: id, MaxInputTokens: config.DefaultMaxInputTokens})
	}
	cache.Update(fallback)
	resolver := model.NewResolver(cache, config.HiddenModels, config.ModelAliases, config.HiddenFromList)

	cfg := config.Config{
		ProxyAPIKey:                    proxyAPIKey,
		StreamingReadTimeout:           120,
		FirstTokenTimeout:              30,
		FirstTokenMaxRetries:           2,
		FakeReasoning:                  false,
		FakeReasoningHandling:          "remove",
		FakeReasoningMaxTokens:         512,
		ToolDescriptionMaxLength:       10000,
		TruncationRecovery:             true,
		FakeReasoningInitialBufferSize: 20,
	}

	router := server.New(cfg, authManager, resolver, &http.Client{Timeout: 120 * time.Second})
	gateway := httptest.NewServer(router)
	defer gateway.Close()

	client := &http.Client{Timeout: 180 * time.Second}

	t.Run("openai non-stream", func(t *testing.T) {
		body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"Say: ok"}],"stream":false}`, modelID)
		req, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+proxyAPIKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(respBody))
		}
		if !strings.Contains(string(respBody), `"choices"`) {
			t.Fatalf("unexpected body: %s", string(respBody))
		}
	})

	t.Run("anthropic non-stream", func(t *testing.T) {
		body := fmt.Sprintf(`{"model":%q,"max_tokens":64,"messages":[{"role":"user","content":"Say: ok"}],"stream":false}`, modelID)
		req, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/messages", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", proxyAPIKey)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", resp.StatusCode, http.StatusOK, string(respBody))
		}
		if !strings.Contains(string(respBody), `"type":"message"`) {
			t.Fatalf("unexpected body: %s", string(respBody))
		}
	})
}
