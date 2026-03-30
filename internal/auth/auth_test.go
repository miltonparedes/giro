package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// --- test helpers ---

func createTestDB(t *testing.T, rows map[string]string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("CREATE TABLE auth_kv (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}
	for k, v := range rows {
		if _, err := db.Exec("INSERT INTO auth_kv (key, value) VALUES (?, ?)", k, v); err != nil {
			t.Fatal(err)
		}
	}
	return dbPath
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func createTestCredsFile(t *testing.T, data map[string]interface{}) string {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), "creds.json")
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return filePath
}

func mockRefreshServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

// --- constructor tests ---

func TestNewKiroAuthManager_Defaults(t *testing.T) {
	m, err := NewKiroAuthManager(Options{})
	if err != nil {
		t.Fatal(err)
	}

	if m.region != "us-east-1" {
		t.Errorf("region = %q, want %q", m.region, "us-east-1")
	}
	if m.authType != KiroDesktop {
		t.Errorf("authType = %v, want KiroDesktop", m.authType)
	}
	if len(m.fingerprint) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(m.fingerprint))
	}
	if m.apiHost != "https://q.us-east-1.amazonaws.com" {
		t.Errorf("apiHost = %q, want %q", m.apiHost, "https://q.us-east-1.amazonaws.com")
	}
	if m.qHost != "https://q.us-east-1.amazonaws.com" {
		t.Errorf("qHost = %q, want %q", m.qHost, "https://q.us-east-1.amazonaws.com")
	}
}

func TestNewKiroAuthManager_CustomRegion(t *testing.T) {
	m, err := NewKiroAuthManager(Options{Region: "eu-west-1"})
	if err != nil {
		t.Fatal(err)
	}

	if m.region != "eu-west-1" {
		t.Errorf("region = %q, want %q", m.region, "eu-west-1")
	}
	if m.apiHost != "https://q.eu-west-1.amazonaws.com" {
		t.Errorf("apiHost = %q", m.apiHost)
	}
	if m.refreshURL != "https://prod.eu-west-1.auth.desktop.kiro.dev/refreshToken" {
		t.Errorf("refreshURL = %q", m.refreshURL)
	}
}

func TestNewKiroAuthManager_WithRefreshToken(t *testing.T) {
	m, err := NewKiroAuthManager(Options{
		RefreshToken: "test-refresh",
		ProfileARN:   "arn:aws:test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if m.refreshToken != "test-refresh" {
		t.Errorf("refreshToken = %q, want %q", m.refreshToken, "test-refresh")
	}
	if m.profileARN != "arn:aws:test" {
		t.Errorf("profileARN = %q, want %q", m.profileARN, "arn:aws:test")
	}
}

// --- SQLite loading tests ---

func TestLoadFromSQLite_SocialToken(t *testing.T) {
	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "sqlite-access",
		"refresh_token": "sqlite-refresh",
		"profile_arn":   "arn:aws:sqlite",
		"region":        "ap-southeast-1",
		"scopes":        []string{"scope1", "scope2"},
		"expires_at":    "2099-01-01T00:00:00Z",
	})

	dbPath := createTestDB(t, map[string]string{
		"kirocli:social:token": tokenData,
	})

	m, err := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	if err != nil {
		t.Fatal(err)
	}

	if m.accessToken != "sqlite-access" {
		t.Errorf("accessToken = %q, want %q", m.accessToken, "sqlite-access")
	}
	if m.refreshToken != "sqlite-refresh" {
		t.Errorf("refreshToken = %q, want %q", m.refreshToken, "sqlite-refresh")
	}
	if m.profileARN != "arn:aws:sqlite" {
		t.Errorf("profileARN = %q, want %q", m.profileARN, "arn:aws:sqlite")
	}
	if m.ssoRegion != "ap-southeast-1" {
		t.Errorf("ssoRegion = %q, want %q", m.ssoRegion, "ap-southeast-1")
	}
	// API region stays us-east-1 for SQLite mode
	if m.region != "us-east-1" {
		t.Errorf("region = %q, want %q", m.region, "us-east-1")
	}
	if m.sqliteTokenKey != "kirocli:social:token" {
		t.Errorf("sqliteTokenKey = %q, want %q", m.sqliteTokenKey, "kirocli:social:token")
	}
	if len(m.scopes) != 2 {
		t.Errorf("scopes len = %d, want 2", len(m.scopes))
	}
	if m.expiresAt.IsZero() {
		t.Error("expiresAt should not be zero")
	}
}

func TestLoadFromSQLite_OdicToken(t *testing.T) {
	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "odic-access",
		"refresh_token": "odic-refresh",
	})

	dbPath := createTestDB(t, map[string]string{
		"kirocli:odic:token": tokenData,
	})

	m, err := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	if err != nil {
		t.Fatal(err)
	}

	if m.accessToken != "odic-access" {
		t.Errorf("accessToken = %q, want %q", m.accessToken, "odic-access")
	}
	if m.sqliteTokenKey != "kirocli:odic:token" {
		t.Errorf("sqliteTokenKey = %q, want %q", m.sqliteTokenKey, "kirocli:odic:token")
	}
}

func TestLoadFromSQLite_LegacyToken(t *testing.T) {
	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "legacy-access",
		"refresh_token": "legacy-refresh",
	})

	dbPath := createTestDB(t, map[string]string{
		"codewhisperer:odic:token": tokenData,
	})

	m, err := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	if err != nil {
		t.Fatal(err)
	}

	if m.accessToken != "legacy-access" {
		t.Errorf("accessToken = %q, want %q", m.accessToken, "legacy-access")
	}
	if m.sqliteTokenKey != "codewhisperer:odic:token" {
		t.Errorf("sqliteTokenKey = %q, want %q", m.sqliteTokenKey, "codewhisperer:odic:token")
	}
}

func TestLoadFromSQLite_TokenKeyPriority(t *testing.T) {
	// Social token should win over ODIC token
	socialData := mustJSON(t, map[string]interface{}{
		"access_token":  "social-access",
		"refresh_token": "social-refresh",
	})
	odicData := mustJSON(t, map[string]interface{}{
		"access_token":  "odic-access",
		"refresh_token": "odic-refresh",
	})

	dbPath := createTestDB(t, map[string]string{
		"kirocli:social:token": socialData,
		"kirocli:odic:token":   odicData,
	})

	m, err := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	if err != nil {
		t.Fatal(err)
	}

	if m.accessToken != "social-access" {
		t.Errorf("accessToken = %q, want %q (social should win)", m.accessToken, "social-access")
	}
}

func TestLoadFromSQLite_WithDeviceRegistration(t *testing.T) {
	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "sso-access",
		"refresh_token": "sso-refresh",
	})
	regData := mustJSON(t, map[string]interface{}{
		"client_id":     "test-client-id",
		"client_secret": "test-client-secret",
		"region":        "eu-central-1",
	})

	dbPath := createTestDB(t, map[string]string{
		"kirocli:odic:token":               tokenData,
		"kirocli:odic:device-registration": regData,
	})

	m, err := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	if err != nil {
		t.Fatal(err)
	}

	if m.clientID != "test-client-id" {
		t.Errorf("clientID = %q, want %q", m.clientID, "test-client-id")
	}
	if m.clientSecret != "test-client-secret" {
		t.Errorf("clientSecret = %q, want %q", m.clientSecret, "test-client-secret")
	}
	if m.authType != AWSSSO {
		t.Errorf("authType = %v, want AWSSSO", m.authType)
	}
}

func TestLoadFromSQLite_DeviceRegistrationRegionFallback(t *testing.T) {
	// When token has no region, device registration region should be used
	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "access",
		"refresh_token": "refresh",
	})
	regData := mustJSON(t, map[string]interface{}{
		"client_id":     "cid",
		"client_secret": "csecret",
		"region":        "us-west-2",
	})

	dbPath := createTestDB(t, map[string]string{
		"kirocli:odic:token":               tokenData,
		"kirocli:odic:device-registration": regData,
	})

	m, err := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	if err != nil {
		t.Fatal(err)
	}

	if m.ssoRegion != "us-west-2" {
		t.Errorf("ssoRegion = %q, want %q", m.ssoRegion, "us-west-2")
	}
}

func TestLoadFromSQLite_TokenRegionOverridesRegistration(t *testing.T) {
	// Token region should take priority over registration region
	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "access",
		"refresh_token": "refresh",
		"region":        "ap-southeast-1",
	})
	regData := mustJSON(t, map[string]interface{}{
		"client_id":     "cid",
		"client_secret": "csecret",
		"region":        "us-west-2",
	})

	dbPath := createTestDB(t, map[string]string{
		"kirocli:odic:token":               tokenData,
		"kirocli:odic:device-registration": regData,
	})

	m, err := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	if err != nil {
		t.Fatal(err)
	}

	if m.ssoRegion != "ap-southeast-1" {
		t.Errorf("ssoRegion = %q, want %q (token region wins)", m.ssoRegion, "ap-southeast-1")
	}
}

func TestLoadFromSQLite_ExpiresAtFormats(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt string
	}{
		{"Z suffix", "2099-01-01T00:00:00Z"},
		{"timezone offset", "2099-01-01T00:00:00+00:00"},
		{"positive offset", "2099-01-01T05:30:00+05:30"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenData := mustJSON(t, map[string]interface{}{
				"access_token": "access",
				"expires_at":   tt.expiresAt,
			})

			dbPath := createTestDB(t, map[string]string{
				"kirocli:social:token": tokenData,
			})

			m, err := NewKiroAuthManager(Options{SQLiteDB: dbPath})
			if err != nil {
				t.Fatal(err)
			}

			if m.expiresAt.IsZero() {
				t.Error("expiresAt should not be zero")
			}
		})
	}
}

func TestLoadFromSQLite_NotFound(t *testing.T) {
	// Should not panic or error on missing file
	m, err := NewKiroAuthManager(Options{SQLiteDB: "/nonexistent/path/db.sqlite3"})
	if err != nil {
		t.Fatal(err)
	}
	if m.accessToken != "" {
		t.Errorf("accessToken should be empty, got %q", m.accessToken)
	}
}

// --- JSON file loading tests ---

func TestLoadFromFile_KiroDesktop(t *testing.T) {
	filePath := createTestCredsFile(t, map[string]interface{}{
		"refreshToken": "file-refresh",
		"accessToken":  "file-access",
		"profileArn":   "arn:aws:file",
		"region":       "eu-west-1",
		"expiresAt":    "2099-12-31T23:59:59Z",
	})

	m, err := NewKiroAuthManager(Options{CredsFile: filePath})
	if err != nil {
		t.Fatal(err)
	}

	if m.refreshToken != "file-refresh" {
		t.Errorf("refreshToken = %q, want %q", m.refreshToken, "file-refresh")
	}
	if m.accessToken != "file-access" {
		t.Errorf("accessToken = %q, want %q", m.accessToken, "file-access")
	}
	if m.profileARN != "arn:aws:file" {
		t.Errorf("profileARN = %q, want %q", m.profileARN, "arn:aws:file")
	}
	// JSON file region updates ALL URLs (unlike SQLite)
	if m.region != "eu-west-1" {
		t.Errorf("region = %q, want %q", m.region, "eu-west-1")
	}
	if m.apiHost != "https://q.eu-west-1.amazonaws.com" {
		t.Errorf("apiHost = %q", m.apiHost)
	}
	if m.authType != KiroDesktop {
		t.Errorf("authType = %v, want KiroDesktop", m.authType)
	}
}

func TestLoadFromFile_AWSSSO(t *testing.T) {
	filePath := createTestCredsFile(t, map[string]interface{}{
		"refreshToken": "sso-refresh",
		"clientId":     "sso-client-id",
		"clientSecret": "sso-client-secret",
	})

	m, err := NewKiroAuthManager(Options{CredsFile: filePath})
	if err != nil {
		t.Fatal(err)
	}

	if m.clientID != "sso-client-id" {
		t.Errorf("clientID = %q, want %q", m.clientID, "sso-client-id")
	}
	if m.clientSecret != "sso-client-secret" {
		t.Errorf("clientSecret = %q, want %q", m.clientSecret, "sso-client-secret")
	}
	if m.authType != AWSSSO {
		t.Errorf("authType = %v, want AWSSSO", m.authType)
	}
}

func TestLoadFromFile_NotFound(t *testing.T) {
	m, err := NewKiroAuthManager(Options{CredsFile: "/nonexistent/creds.json"})
	if err != nil {
		t.Fatal(err)
	}
	if m.accessToken != "" {
		t.Errorf("accessToken should be empty, got %q", m.accessToken)
	}
}

// --- SQLite priority over JSON ---

func TestSQLitePriorityOverJSON(t *testing.T) {
	// When both SQLite and JSON are provided, SQLite should win
	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "sqlite-wins",
		"refresh_token": "sqlite-refresh",
	})
	dbPath := createTestDB(t, map[string]string{
		"kirocli:social:token": tokenData,
	})

	filePath := createTestCredsFile(t, map[string]interface{}{
		"accessToken":  "json-loses",
		"refreshToken": "json-refresh",
	})

	m, err := NewKiroAuthManager(Options{
		SQLiteDB:  dbPath,
		CredsFile: filePath,
	})
	if err != nil {
		t.Fatal(err)
	}

	if m.accessToken != "sqlite-wins" {
		t.Errorf("accessToken = %q, want %q (SQLite should win)", m.accessToken, "sqlite-wins")
	}
}

// --- auth type detection ---

func TestDetectAuthType_KiroDesktop(t *testing.T) {
	m, _ := NewKiroAuthManager(Options{RefreshToken: "rt"})
	if m.authType != KiroDesktop {
		t.Errorf("authType = %v, want KiroDesktop", m.authType)
	}
}

func TestDetectAuthType_AWSSSO(t *testing.T) {
	filePath := createTestCredsFile(t, map[string]interface{}{
		"refreshToken": "rt",
		"clientId":     "cid",
		"clientSecret": "csec",
	})

	m, _ := NewKiroAuthManager(Options{CredsFile: filePath})
	if m.authType != AWSSSO {
		t.Errorf("authType = %v, want AWSSSO", m.authType)
	}
}

// --- token expiry checks ---

func TestIsTokenExpiringSoon(t *testing.T) {
	m := &KiroAuthManager{}

	// Zero time → expiring soon
	if !m.isTokenExpiringSoon() {
		t.Error("zero expiresAt should be considered expiring soon")
	}

	// Far future → not expiring soon
	m.expiresAt = time.Now().UTC().Add(2 * time.Hour)
	if m.isTokenExpiringSoon() {
		t.Error("2h future should not be expiring soon")
	}

	// Within threshold → expiring soon
	m.expiresAt = time.Now().UTC().Add(300 * time.Second)
	if !m.isTokenExpiringSoon() {
		t.Error("300s should be within threshold (600s)")
	}
}

func TestIsTokenExpired(t *testing.T) {
	m := &KiroAuthManager{}

	// Zero time → expired
	if !m.isTokenExpired() {
		t.Error("zero expiresAt should be considered expired")
	}

	// Future → not expired
	m.expiresAt = time.Now().UTC().Add(time.Hour)
	if m.isTokenExpired() {
		t.Error("1h future should not be expired")
	}

	// Past → expired
	m.expiresAt = time.Now().UTC().Add(-time.Hour)
	if !m.isTokenExpired() {
		t.Error("1h past should be expired")
	}
}

// --- Kiro Desktop refresh tests ---

func TestRefreshKiroDesktop_Success(t *testing.T) {
	ts := mockRefreshServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		ua := r.Header.Get("User-Agent")
		if !hasPrefix(ua, "KiroIDE-0.7.45-") {
			t.Errorf("User-Agent = %q, want prefix KiroIDE-0.7.45-", ua)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["refreshToken"] != "old-refresh" {
			t.Errorf("refreshToken = %q, want %q", body["refreshToken"], "old-refresh")
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken":  "new-access",
			"refreshToken": "new-refresh",
			"expiresIn":    3600,
			"profileArn":   "arn:aws:new",
		})
	})

	m, _ := NewKiroAuthManager(Options{RefreshToken: "old-refresh"})
	m.refreshURL = ts.URL

	token, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if token != "new-access" {
		t.Errorf("token = %q, want %q", token, "new-access")
	}
	if m.refreshToken != "new-refresh" {
		t.Errorf("refreshToken = %q, want %q", m.refreshToken, "new-refresh")
	}
	if m.profileARN != "arn:aws:new" {
		t.Errorf("profileARN = %q, want %q", m.profileARN, "arn:aws:new")
	}
	if m.expiresAt.IsZero() {
		t.Error("expiresAt should be set after refresh")
	}
}

func TestRefreshKiroDesktop_PartialResponse(t *testing.T) {
	// refreshToken and profileArn not in response → should keep old values
	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken": "new-access",
			"expiresIn":   7200,
		})
	})

	m, _ := NewKiroAuthManager(Options{
		RefreshToken: "old-refresh",
		ProfileARN:   "arn:aws:old",
	})
	m.refreshURL = ts.URL

	_, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if m.refreshToken != "old-refresh" {
		t.Errorf("refreshToken should stay %q, got %q", "old-refresh", m.refreshToken)
	}
	if m.profileARN != "arn:aws:old" {
		t.Errorf("profileARN should stay %q, got %q", "arn:aws:old", m.profileARN)
	}
}

func TestRefreshKiroDesktop_NoRefreshToken(t *testing.T) {
	m, _ := NewKiroAuthManager(Options{})
	_, err := m.GetAccessToken(context.Background())
	if err == nil {
		t.Error("expected error when no refresh token")
	}
}

func TestRefreshKiroDesktop_HTTPError(t *testing.T) {
	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	})

	m, _ := NewKiroAuthManager(Options{RefreshToken: "rt"})
	m.refreshURL = ts.URL

	_, err := m.GetAccessToken(context.Background())
	if err == nil {
		t.Error("expected error on 500")
	}
}

// --- token validity caching ---

func TestGetAccessToken_ReturnsCachedToken(t *testing.T) {
	callCount := 0
	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken": "fresh-token",
			"expiresIn":   3600,
		})
	})

	m, _ := NewKiroAuthManager(Options{RefreshToken: "rt"})
	m.refreshURL = ts.URL

	// First call triggers refresh
	token1, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Second call should use cached token
	token2, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if token1 != token2 {
		t.Errorf("tokens differ: %q vs %q", token1, token2)
	}
	if callCount != 1 {
		t.Errorf("refresh called %d times, want 1", callCount)
	}
}

func TestGetAccessToken_RefreshesExpiredToken(t *testing.T) {
	callCount := 0
	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken": "refreshed-token",
			"expiresIn":   3600,
		})
	})

	m, _ := NewKiroAuthManager(Options{RefreshToken: "rt"})
	m.refreshURL = ts.URL
	m.accessToken = "old-token"
	m.expiresAt = time.Now().UTC().Add(-time.Hour) // expired

	token, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if token != "refreshed-token" {
		t.Errorf("token = %q, want %q", token, "refreshed-token")
	}
	if callCount != 1 {
		t.Errorf("refresh called %d times, want 1", callCount)
	}
}

// --- AWS SSO OIDC refresh tests ---

func TestRefreshAWSSSOOIDC_Success(t *testing.T) {
	ts := mockRefreshServer(t, func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["grantType"] != "refresh_token" {
			t.Errorf("grantType = %q", body["grantType"])
		}
		if body["clientId"] != "test-cid" {
			t.Errorf("clientId = %q", body["clientId"])
		}
		if body["clientSecret"] != "test-csec" {
			t.Errorf("clientSecret = %q", body["clientSecret"])
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken":  "sso-access",
			"refreshToken": "sso-refresh",
			"expiresIn":    3600,
		})
	})

	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "old-access",
		"refresh_token": "old-refresh",
	})
	regData := mustJSON(t, map[string]interface{}{
		"client_id":     "test-cid",
		"client_secret": "test-csec",
	})
	dbPath := createTestDB(t, map[string]string{
		"kirocli:odic:token":               tokenData,
		"kirocli:odic:device-registration": regData,
	})

	m, _ := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	m.ssoOIDCURL = ts.URL
	m.expiresAt = time.Time{} // force refresh

	token, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if token != "sso-access" {
		t.Errorf("token = %q, want %q", token, "sso-access")
	}
}

func TestRefreshAWSSSOOIDC_RetryOnSQLite400(t *testing.T) {
	callCount := 0
	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken": "retry-access",
			"expiresIn":   3600,
		})
	})

	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "initial-access",
		"refresh_token": "initial-refresh",
	})
	regData := mustJSON(t, map[string]interface{}{
		"client_id":     "cid",
		"client_secret": "csec",
	})
	dbPath := createTestDB(t, map[string]string{
		"kirocli:odic:token":               tokenData,
		"kirocli:odic:device-registration": regData,
	})

	m, _ := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	m.ssoOIDCURL = ts.URL
	m.expiresAt = time.Time{} // force refresh

	token, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if token != "retry-access" {
		t.Errorf("token = %q, want %q", token, "retry-access")
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2 (initial + retry)", callCount)
	}
}

// --- graceful degradation ---

func TestGracefulDegradation_SQLiteMode(t *testing.T) {
	// When refresh fails with 400 in SQLite mode but token not expired,
	// should return the existing token
	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
	})

	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "still-valid-access",
		"refresh_token": "stale-refresh",
	})
	regData := mustJSON(t, map[string]interface{}{
		"client_id":     "cid",
		"client_secret": "csec",
	})
	dbPath := createTestDB(t, map[string]string{
		"kirocli:odic:token":               tokenData,
		"kirocli:odic:device-registration": regData,
	})

	m, _ := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	m.ssoOIDCURL = ts.URL
	// Token expiring soon but NOT expired
	m.expiresAt = time.Now().UTC().Add(5 * time.Minute)
	m.accessToken = "still-valid-access"

	token, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if token != "still-valid-access" {
		t.Errorf("token = %q, want %q (graceful degradation)", token, "still-valid-access")
	}
}

func TestGracefulDegradation_ExpiredToken(t *testing.T) {
	// When refresh fails with 400 and token IS expired → error
	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
	})

	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "expired-access",
		"refresh_token": "stale-refresh",
	})
	regData := mustJSON(t, map[string]interface{}{
		"client_id":     "cid",
		"client_secret": "csec",
	})
	dbPath := createTestDB(t, map[string]string{
		"kirocli:odic:token":               tokenData,
		"kirocli:odic:device-registration": regData,
	})

	m, _ := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	m.ssoOIDCURL = ts.URL
	m.expiresAt = time.Now().UTC().Add(-time.Hour) // expired
	m.accessToken = "expired-access"

	_, err := m.GetAccessToken(context.Background())
	if err == nil {
		t.Error("expected error when token is expired and refresh fails")
	}
}

func TestGracefulDegradation_NonSQLiteMode(t *testing.T) {
	// Non-SQLite mode should NOT degrade gracefully
	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad_request"}`))
	})

	m, _ := NewKiroAuthManager(Options{RefreshToken: "rt"})
	m.refreshURL = ts.URL
	m.accessToken = "valid-access"
	m.expiresAt = time.Now().UTC().Add(5 * time.Minute)

	_, err := m.GetAccessToken(context.Background())
	if err == nil {
		t.Error("expected error in non-SQLite mode on 400")
	}
}

// --- force refresh ---

func TestForceRefresh_Success(t *testing.T) {
	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken": "forced-access",
			"expiresIn":   3600,
		})
	})

	m, _ := NewKiroAuthManager(Options{RefreshToken: "rt"})
	m.refreshURL = ts.URL

	token, err := m.ForceRefresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if token != "forced-access" {
		t.Errorf("token = %q, want %q", token, "forced-access")
	}
}

func TestForceRefresh_NoGracefulDegradation(t *testing.T) {
	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	})

	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "valid",
		"refresh_token": "stale",
	})
	dbPath := createTestDB(t, map[string]string{
		"kirocli:social:token": tokenData,
	})

	m, _ := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	m.refreshURL = ts.URL
	m.accessToken = "valid"
	m.expiresAt = time.Now().UTC().Add(5 * time.Minute)

	_, err := m.ForceRefresh(context.Background())
	if err == nil {
		t.Error("ForceRefresh should NOT degrade gracefully")
	}
}

// --- SQLite reload in GetAccessToken ---

func TestGetAccessToken_SQLiteReloadFreshToken(t *testing.T) {
	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "kiro-cli-refreshed",
		"refresh_token": "kiro-cli-refresh",
		"expires_at":    time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339),
	})
	dbPath := createTestDB(t, map[string]string{
		"kirocli:social:token": tokenData,
	})

	m, _ := NewKiroAuthManager(Options{SQLiteDB: dbPath})
	// Simulate expiring-soon token from earlier load
	m.accessToken = "old-access"
	m.expiresAt = time.Now().UTC().Add(5 * time.Minute) // within threshold

	token, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if token != "kiro-cli-refreshed" {
		t.Errorf("token = %q, want %q (should reload from SQLite)", token, "kiro-cli-refreshed")
	}
}

// --- concurrent access ---

func TestGetAccessToken_Concurrent(t *testing.T) {
	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken": "concurrent-token",
			"expiresIn":   3600,
		})
	})

	m, _ := NewKiroAuthManager(Options{RefreshToken: "rt"})
	m.refreshURL = ts.URL

	const goroutines = 10
	var wg sync.WaitGroup
	errors := make([]error, goroutines)
	tokens := make([]string, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			token, err := m.GetAccessToken(context.Background())
			tokens[idx] = token
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	for i := range goroutines {
		if errors[i] != nil {
			t.Errorf("goroutine %d: %v", i, errors[i])
		}
		if tokens[i] != "concurrent-token" {
			t.Errorf("goroutine %d: token = %q, want %q", i, tokens[i], "concurrent-token")
		}
	}
}

// --- credential saving ---

func TestSaveToSQLite(t *testing.T) {
	tokenData := mustJSON(t, map[string]interface{}{
		"access_token":  "original",
		"refresh_token": "original",
	})
	dbPath := createTestDB(t, map[string]string{
		"kirocli:social:token": tokenData,
	})

	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken":  "saved-access",
			"refreshToken": "saved-refresh",
			"expiresIn":    3600,
		})
	})

	m, _ := NewKiroAuthManager(Options{
		SQLiteDB: dbPath,
	})
	m.refreshURL = ts.URL
	m.expiresAt = time.Time{} // force refresh

	_, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Verify saved to SQLite
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	var value string
	if err := db.QueryRow("SELECT value FROM auth_kv WHERE key = ?", "kirocli:social:token").Scan(&value); err != nil {
		t.Fatal(err)
	}

	var saved map[string]interface{}
	if err := json.Unmarshal([]byte(value), &saved); err != nil {
		t.Fatal(err)
	}

	if saved["access_token"] != "saved-access" {
		t.Errorf("saved access_token = %q, want %q", saved["access_token"], "saved-access")
	}
	if saved["refresh_token"] != "saved-refresh" {
		t.Errorf("saved refresh_token = %q, want %q", saved["refresh_token"], "saved-refresh")
	}
}

func TestSaveToFile(t *testing.T) {
	filePath := createTestCredsFile(t, map[string]interface{}{
		"refreshToken": "original",
		"extraField":   "preserved",
	})

	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken":  "saved-access",
			"refreshToken": "saved-refresh",
			"expiresIn":    3600,
		})
	})

	m, _ := NewKiroAuthManager(Options{
		CredsFile: filePath,
	})
	m.refreshURL = ts.URL
	m.expiresAt = time.Time{} // force refresh

	_, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Verify saved to file
	rawData, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}

	var saved map[string]interface{}
	if err := json.Unmarshal(rawData, &saved); err != nil {
		t.Fatal(err)
	}

	if saved["accessToken"] != "saved-access" {
		t.Errorf("saved accessToken = %q, want %q", saved["accessToken"], "saved-access")
	}
	if saved["refreshToken"] != "saved-refresh" {
		t.Errorf("saved refreshToken = %q, want %q", saved["refreshToken"], "saved-refresh")
	}
	// Extra fields should be preserved
	if saved["extraField"] != "preserved" {
		t.Errorf("extraField = %q, want %q", saved["extraField"], "preserved")
	}
}

// --- fingerprint ---

func TestFingerprint_Deterministic(t *testing.T) {
	fp1 := generateFingerprint()
	fp2 := generateFingerprint()

	if fp1 != fp2 {
		t.Errorf("fingerprint should be deterministic: %q != %q", fp1, fp2)
	}
	if len(fp1) != 64 {
		t.Errorf("fingerprint length = %d, want 64 (SHA256 hex)", len(fp1))
	}
}

func TestSha256Hex(t *testing.T) {
	result := sha256Hex("test")
	if len(result) != 64 {
		t.Errorf("sha256Hex length = %d, want 64", len(result))
	}
	// Known SHA256 of "test"
	expected := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if result != expected {
		t.Errorf("sha256Hex('test') = %q, want %q", result, expected)
	}
}

// --- parseExpiresAt ---

func TestParseExpiresAt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"Z suffix", "2024-01-15T10:30:00Z", false},
		{"positive offset", "2024-01-15T10:30:00+05:30", false},
		{"negative offset", "2024-01-15T10:30:00-08:00", false},
		{"no timezone", "2024-01-15T10:30:00", false},
		{"invalid", "not-a-date", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseExpiresAt(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.IsZero() {
				t.Error("result should not be zero")
			}
		})
	}
}

// --- expandPath ---

func TestExpandPath(t *testing.T) {
	result := expandPath("/absolute/path")
	if result != "/absolute/path" {
		t.Errorf("absolute path should stay: %q", result)
	}

	result = expandPath("~/test")
	home, err := os.UserHomeDir()
	if err == nil {
		expected := filepath.Join(home, "test")
		if result != expected {
			t.Errorf("expandPath(~/test) = %q, want %q", result, expected)
		}
	}
}

// --- exported getters ---

func TestExportedGetters(t *testing.T) {
	m, _ := NewKiroAuthManager(Options{
		RefreshToken: "rt",
		ProfileARN:   "arn:test",
		Region:       "us-west-2",
	})

	if m.Fingerprint() == "" {
		t.Error("Fingerprint() should not be empty")
	}
	if m.GetAuthType() != KiroDesktop {
		t.Errorf("GetAuthType() = %v, want KiroDesktop", m.GetAuthType())
	}
	if m.GetProfileARN() != "arn:test" {
		t.Errorf("GetProfileARN() = %q, want %q", m.GetProfileARN(), "arn:test")
	}
	if m.APIHost() != "https://q.us-west-2.amazonaws.com" {
		t.Errorf("APIHost() = %q", m.APIHost())
	}
	if m.QHost() != "https://q.us-west-2.amazonaws.com" {
		t.Errorf("QHost() = %q", m.QHost())
	}
}

// --- AuthType.String ---

func TestAuthType_String(t *testing.T) {
	if KiroDesktop.String() != "KiroDesktop" {
		t.Errorf("KiroDesktop.String() = %q", KiroDesktop.String())
	}
	if AWSSSO.String() != "AWSSSO" {
		t.Errorf("AWSSSO.String() = %q", AWSSSO.String())
	}
}

// --- HTTPError ---

func TestHTTPError(t *testing.T) {
	err := &HTTPError{StatusCode: 400, Body: "bad request"}
	if err.Error() != "HTTP 400: bad request" {
		t.Errorf("Error() = %q", err.Error())
	}

	if !isHTTP400(err) {
		t.Error("isHTTP400 should return true for 400")
	}

	err500 := &HTTPError{StatusCode: 500, Body: "server error"}
	if isHTTP400(err500) {
		t.Error("isHTTP400 should return false for 500")
	}
}

// --- Refresh and persistence for autodetected sources ---

// writeSQLiteStaleToken updates an existing kiro-cli SQLite token row with
// stale (expired) token data so the next GetAccessToken triggers a refresh.
func writeSQLiteStaleToken(t *testing.T, dbPath, tokenKey, access, refresh string) {
	t.Helper()
	data := mustJSON(t, map[string]interface{}{
		"access_token":  access,
		"refresh_token": refresh,
		"expires_at":    time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	})
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("UPDATE auth_kv SET value = ? WHERE key = ?", data, tokenKey); err != nil {
		t.Fatal(err)
	}
}

// insertSQLiteRow inserts or replaces a key/value pair in the auth_kv table.
func insertSQLiteRow(t *testing.T, dbPath, key, value string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("INSERT OR REPLACE INTO auth_kv (key, value) VALUES (?, ?)", key, value); err != nil {
		t.Fatal(err)
	}
}

// readSQLiteTokenField reads a single field from the JSON-encoded token row.
func readSQLiteTokenField(t *testing.T, dbPath, tokenKey, field string) string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var raw string
	if err := db.QueryRow("SELECT value FROM auth_kv WHERE key = ?", tokenKey).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	v, _ := m[field].(string)
	return v
}

// newMockDesktopRefreshServer returns a mock server that responds with
// the given access and refresh tokens (Kiro Desktop format).
func newMockDesktopRefreshServer(t *testing.T, access, refresh string) *httptest.Server {
	t.Helper()
	return mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken":  access,
			"refreshToken": refresh,
			"expiresIn":    3600,
		})
	})
}

// resolveAndBuildManager resolves the source for the given home, constructs
// an auth manager, and returns both.
func resolveAndBuildManager(t *testing.T, homeDir string) (*ResolvedSource, *KiroAuthManager) {
	t.Helper()
	resolved, err := ResolveSource(ResolveInput{HomeDir: homeDir})
	if err != nil {
		t.Fatal(err)
	}
	opts := resolved.BuildAuthOptions("", "", "us-east-1", "")
	m, err := NewKiroAuthManager(opts)
	if err != nil {
		t.Fatal(err)
	}
	return resolved, m
}

// TestAutodetectedKiroCLI_RefreshPersistAndRestart verifies that an
// autodetected kiro-cli source refreshes a stale token, persists the
// refreshed credentials back to the same SQLite store, and on restart
// (new auth manager from the same autodetected path) the persisted tokens
// are available without manual reconfiguration.
func TestAutodetectedKiroCLI_RefreshPersistAndRestart(t *testing.T) {
	homeDir := t.TempDir()
	dbPath := createTempKiroCLIStore(t, homeDir)
	writeSQLiteStaleToken(t, dbPath, "kirocli:social:token", "stale-access", "valid-refresh")

	ts := newMockDesktopRefreshServer(t, "refreshed-access", "refreshed-refresh")
	resolved, m := resolveAndBuildManager(t, homeDir)
	m.refreshURL = ts.URL

	// Verify autodetected source metadata.
	if resolved.Kind != SourceKiroCLI || resolved.Path != dbPath || !resolved.Writable {
		t.Fatalf("source mismatch: Kind=%q Path=%q Writable=%v", resolved.Kind, resolved.Path, resolved.Writable)
	}

	// Refresh: token is stale so GetAccessToken should call the mock.
	token, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	if token != "refreshed-access" {
		t.Errorf("token = %q, want %q", token, "refreshed-access")
	}

	// Persistence: same SQLite path should contain the refreshed values.
	if got := readSQLiteTokenField(t, dbPath, "kirocli:social:token", "access_token"); got != "refreshed-access" {
		t.Errorf("persisted access_token = %q, want %q", got, "refreshed-access")
	}
	if got := readSQLiteTokenField(t, dbPath, "kirocli:social:token", "refresh_token"); got != "refreshed-refresh" {
		t.Errorf("persisted refresh_token = %q, want %q", got, "refreshed-refresh")
	}

	// Restart: a new auth manager from the same home picks up the persisted tokens.
	_, m2 := resolveAndBuildManager(t, homeDir)
	if m2.accessToken != "refreshed-access" {
		t.Errorf("restart: accessToken = %q, want %q", m2.accessToken, "refreshed-access")
	}
	if m2.refreshToken != "refreshed-refresh" {
		t.Errorf("restart: refreshToken = %q, want %q", m2.refreshToken, "refreshed-refresh")
	}
	if m2.isTokenExpiringSoon() {
		t.Error("restart: token should NOT be expiring soon after loading freshly persisted credentials")
	}
}

// TestAutodetectedKiroIDE_RefreshPersistAndRestart verifies the same
// lifecycle for an autodetected kiro-ide (JSON credentials file) source.
func TestAutodetectedKiroIDE_RefreshPersistAndRestart(t *testing.T) {
	homeDir := t.TempDir()
	staleContent := mustJSON(t, map[string]interface{}{
		"refreshToken": "valid-refresh",
		"accessToken":  "stale-access",
		"expiresAt":    time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	})
	storePath := createTempKiroIDEStoreWithContent(t, homeDir, staleContent)

	ts := newMockDesktopRefreshServer(t, "refreshed-access", "refreshed-refresh")
	resolved, m := resolveAndBuildManager(t, homeDir)
	m.refreshURL = ts.URL

	if resolved.Kind != SourceKiroIDE || resolved.Path != storePath || !resolved.Writable {
		t.Fatalf("source mismatch: Kind=%q Path=%q Writable=%v", resolved.Kind, resolved.Path, resolved.Writable)
	}

	// Refresh.
	token, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	if token != "refreshed-access" {
		t.Errorf("token = %q, want %q", token, "refreshed-access")
	}

	// Persistence: same JSON file should contain the refreshed values.
	rawData, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]interface{}
	if err := json.Unmarshal(rawData, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["accessToken"] != "refreshed-access" {
		t.Errorf("persisted accessToken = %q, want %q", saved["accessToken"], "refreshed-access")
	}
	if saved["refreshToken"] != "refreshed-refresh" {
		t.Errorf("persisted refreshToken = %q, want %q", saved["refreshToken"], "refreshed-refresh")
	}

	// Restart.
	_, m2 := resolveAndBuildManager(t, homeDir)
	if m2.accessToken != "refreshed-access" || m2.refreshToken != "refreshed-refresh" {
		t.Errorf("restart: accessToken=%q refreshToken=%q", m2.accessToken, m2.refreshToken)
	}
	if m2.isTokenExpiringSoon() {
		t.Error("restart: token should NOT be expiring soon after loading freshly persisted credentials")
	}
}

// TestAutodetectedKiroCLI_AWSSSO_RefreshPersistAndRestart verifies the
// lifecycle for an autodetected kiro-cli source using AWS SSO OIDC
// (with device registration), including refresh, persistence, and restart.
func TestAutodetectedKiroCLI_AWSSSO_RefreshPersistAndRestart(t *testing.T) {
	homeDir := t.TempDir()
	dbPath := createTempKiroCLIStore(t, homeDir)

	// Stale token without region so SQLite reload does not override mock URL.
	writeSQLiteStaleToken(t, dbPath, "kirocli:social:token", "stale-sso-access", "valid-sso-refresh")
	regData := mustJSON(t, map[string]interface{}{
		"client_id":     "sso-client-id",
		"client_secret": "sso-client-secret",
	})
	insertSQLiteRow(t, dbPath, "kirocli:odic:device-registration", regData)

	ts := mockRefreshServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"accessToken":  "refreshed-sso-access",
			"refreshToken": "refreshed-sso-refresh",
			"expiresIn":    3600,
		})
	})

	_, m := resolveAndBuildManager(t, homeDir)
	if m.authType != AWSSSO {
		t.Fatalf("authType = %v, want AWSSSO", m.authType)
	}
	m.ssoOIDCURL = ts.URL

	// Refresh via SSO OIDC.
	token, err := m.GetAccessToken(context.Background())
	if err != nil {
		t.Fatalf("GetAccessToken failed: %v", err)
	}
	if token != "refreshed-sso-access" {
		t.Errorf("token = %q, want %q", token, "refreshed-sso-access")
	}

	// Persistence.
	if got := readSQLiteTokenField(t, dbPath, "kirocli:social:token", "access_token"); got != "refreshed-sso-access" {
		t.Errorf("persisted access_token = %q, want %q", got, "refreshed-sso-access")
	}

	// Restart.
	_, m2 := resolveAndBuildManager(t, homeDir)
	if m2.accessToken != "refreshed-sso-access" || m2.refreshToken != "refreshed-sso-refresh" {
		t.Errorf("restart: accessToken=%q refreshToken=%q", m2.accessToken, m2.refreshToken)
	}
	if m2.authType != AWSSSO {
		t.Errorf("restart: authType = %v, want AWSSSO", m2.authType)
	}
}

// TestAutodetectedSourcePersistenceDoesNotJumpStores verifies that when
// kiro-cli wins autodetection and tokens are refreshed, the persistence
// writes only to the kiro-cli store and does NOT modify the kiro-ide store.
func TestAutodetectedSourcePersistenceDoesNotJumpStores(t *testing.T) {
	homeDir := t.TempDir()
	dbPath := createTempKiroCLIStore(t, homeDir)
	writeSQLiteStaleToken(t, dbPath, "kirocli:social:token", "stale-access", "valid-refresh")

	ideOriginal := `{"refreshToken":"ide-original-refresh","accessToken":"ide-original-access"}`
	idePath := createTempKiroIDEStoreWithContent(t, homeDir, ideOriginal)

	ts := newMockDesktopRefreshServer(t, "refreshed-access", "refreshed-refresh")
	resolved, m := resolveAndBuildManager(t, homeDir)
	m.refreshURL = ts.URL

	if resolved.Kind != SourceKiroCLI {
		t.Fatalf("Kind = %q, want %q", resolved.Kind, SourceKiroCLI)
	}

	if _, err := m.GetAccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}

	// kiro-ide store must be untouched.
	ideData, err := os.ReadFile(idePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(ideData) != ideOriginal {
		t.Errorf("kiro-ide store was modified; persistence jumped stores.\noriginal: %s\n     got: %s", ideOriginal, string(ideData))
	}
}

// TestAutodetectedKiroCLI_RefreshPersistenceStableSourcePath verifies that
// the resolved source path is identical before and after a refresh cycle,
// confirming the write-back target does not drift.
func TestAutodetectedKiroCLI_RefreshPersistenceStableSourcePath(t *testing.T) {
	homeDir := t.TempDir()
	createTempKiroCLIStore(t, homeDir)

	src1, err := ResolveSource(ResolveInput{HomeDir: homeDir})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a refresh+persist cycle by updating token data on disk.
	newToken := mustJSON(t, map[string]interface{}{
		"access_token":  "after-refresh",
		"refresh_token": "after-refresh-rt",
		"expires_at":    time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	db, err := sql.Open("sqlite", src1.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE auth_kv SET value = ? WHERE key = ?", newToken, "kirocli:social:token"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()

	// Second resolution: source metadata must be identical.
	src2, err := ResolveSource(ResolveInput{HomeDir: homeDir})
	if err != nil {
		t.Fatal(err)
	}
	if src1.Kind != src2.Kind || src1.Path != src2.Path || src1.Writable != src2.Writable {
		t.Errorf("source drifted: %+v → %+v", src1, src2)
	}
}

// --- helper ---

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
