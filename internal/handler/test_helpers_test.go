package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miltonparedes/giro/internal/auth"
	"github.com/miltonparedes/giro/internal/config"
	"github.com/miltonparedes/giro/internal/model"
)

func newTestAuthManager(t *testing.T, apiHost, qHost string) *auth.KiroAuthManager {
	t.Helper()

	credsPath := filepath.Join(t.TempDir(), "credentials.json")
	expiresAt := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	creds := fmt.Sprintf(
		`{"accessToken":"test-access-token","refreshToken":"test-refresh-token","expiresAt":%q}`,
		expiresAt,
	)
	if err := os.WriteFile(credsPath, []byte(creds), 0o600); err != nil {
		t.Fatalf("write creds file: %v", err)
	}

	m, err := auth.NewKiroAuthManager(auth.Options{
		Region:          "us-east-1",
		CredsFile:       credsPath,
		APIHostOverride: apiHost,
		QHostOverride:   qHost,
	})
	if err != nil {
		t.Fatalf("NewKiroAuthManager: %v", err)
	}

	return m
}

func newTestResolver(ids ...string) *model.Resolver {
	cache := model.NewInfoCache(time.Hour)
	models := make([]model.Info, 0, len(ids))
	for _, id := range ids {
		models = append(models, model.Info{ModelID: id, MaxInputTokens: config.DefaultMaxInputTokens})
	}
	cache.Update(models)
	return model.NewResolver(cache, map[string]string{}, map[string]string{}, nil)
}

func testHandlerConfig() config.Config {
	return config.Config{
		StreamingReadTimeout:           2,
		FirstTokenTimeout:              0.05,
		FirstTokenMaxRetries:           2,
		FakeReasoning:                  false,
		FakeReasoningHandling:          "remove",
		FakeReasoningMaxTokens:         256,
		ToolDescriptionMaxLength:       10000,
		TruncationRecovery:             true,
		FakeReasoningInitialBufferSize: 20,
	}
}

func newTestHTTPClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Second}
}
