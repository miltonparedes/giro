// Package auth manages Kiro API token lifecycle with support for
// Kiro Desktop and AWS SSO OIDC authentication.
package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // sqlite driver registration
)

const (
	// TokenRefreshThreshold is seconds before expiry to trigger a refresh.
	TokenRefreshThreshold = 600

	// tokenExpiryBuffer is subtracted from expiresIn for safety margin.
	tokenExpiryBuffer = 60

	httpTimeout = 30 * time.Second
)

// sqliteTokenKeys are searched in priority order when loading credentials.
var sqliteTokenKeys = []string{
	"kirocli:social:token",
	"kirocli:odic:token",
	"codewhisperer:odic:token",
}

// sqliteRegistrationKeys are device registration keys for AWS SSO OIDC.
var sqliteRegistrationKeys = []string{
	"kirocli:odic:device-registration",
	"codewhisperer:odic:device-registration",
}

// AuthType represents the authentication mechanism.
type AuthType int //nolint:revive // AuthType is clearer than Type for cross-package use

const (
	// KiroDesktop uses Kiro IDE credentials.
	KiroDesktop AuthType = iota
	// AWSSSO uses AWS SSO OIDC credentials from kiro-cli.
	AWSSSO
)

// String returns a human-readable name for the auth type.
func (a AuthType) String() string {
	if a == AWSSSO {
		return "AWSSSO"
	}
	return "KiroDesktop"
}

// Options configures a KiroAuthManager.
type Options struct {
	RefreshToken string //nolint:gosec // not a hardcoded credential
	ProfileARN   string
	Region       string
	CredsFile    string
	SQLiteDB     string
	VPNProxyURL  string
}

// KiroAuthManager manages the token lifecycle for the Kiro API.
type KiroAuthManager struct {
	mu             sync.Mutex
	refreshToken   string
	profileARN     string
	region         string
	credsFile      string
	sqliteDB       string
	clientID       string
	clientSecret   string
	clientIDHash   string
	ssoRegion      string
	scopes         []string
	sqliteTokenKey string
	accessToken    string
	expiresAt      time.Time
	authType       AuthType
	refreshURL     string
	ssoOIDCURL     string
	apiHost        string
	qHost          string
	fingerprint    string
	httpClient     *http.Client
}

// NewKiroAuthManager creates a new auth manager with the given options.
func NewKiroAuthManager(opts Options) (*KiroAuthManager, error) {
	region := opts.Region
	if region == "" {
		region = "us-east-1"
	}

	m := &KiroAuthManager{
		refreshToken: opts.RefreshToken,
		profileARN:   opts.ProfileARN,
		region:       region,
		credsFile:    opts.CredsFile,
		sqliteDB:     opts.SQLiteDB,
		fingerprint:  generateFingerprint(),
		httpClient:   newHTTPClient(opts.VPNProxyURL),
	}
	m.setRegionURLs(region)
	m.ssoOIDCURL = buildSSOOIDCURL(region)

	if opts.SQLiteDB != "" {
		m.loadFromSQLite(opts.SQLiteDB)
	} else if opts.CredsFile != "" {
		m.loadFromFile(opts.CredsFile)
	}

	m.detectAuthType()

	slog.Info("auth manager initialized",
		"type", m.authType.String(),
		"region", m.region,
		"api_host", m.apiHost,
		"q_host", m.qHost,
	)

	return m, nil
}

// GetAccessToken returns a valid access token, refreshing if necessary.
// It is safe for concurrent use.
func (m *KiroAuthManager) GetAccessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.accessToken != "" && !m.isTokenExpiringSoon() {
		return m.accessToken, nil
	}

	if m.sqliteDB != "" {
		m.loadFromSQLite(m.sqliteDB)
		if m.accessToken != "" && !m.isTokenExpiringSoon() {
			slog.Debug("sqlite reload provided fresh token")
			return m.accessToken, nil
		}
	}

	err := m.refreshTokenRequest(ctx)
	if err != nil {
		return m.handleRefreshError(err)
	}

	if m.accessToken == "" {
		return "", fmt.Errorf("auth: failed to obtain access token")
	}

	return m.accessToken, nil
}

// ForceRefresh forces a token refresh, typically after a 403 from the API.
// Unlike GetAccessToken, it does not gracefully degrade on failure.
func (m *KiroAuthManager) ForceRefresh(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	slog.Debug("force refresh triggered")

	if err := m.refreshTokenRequest(ctx); err != nil {
		return "", fmt.Errorf("auth: force refresh failed: %w", err)
	}

	return m.accessToken, nil
}

// Fingerprint returns the machine fingerprint used in User-Agent headers.
func (m *KiroAuthManager) Fingerprint() string {
	return m.fingerprint
}

// GetAuthType returns the detected authentication type.
func (m *KiroAuthManager) GetAuthType() AuthType {
	return m.authType
}

// GetProfileARN returns the AWS CodeWhisperer profile ARN.
func (m *KiroAuthManager) GetProfileARN() string {
	return m.profileARN
}

// APIHost returns the Kiro API host URL for the configured region.
func (m *KiroAuthManager) APIHost() string {
	return m.apiHost
}

// QHost returns the Kiro Q API host URL for the configured region.
func (m *KiroAuthManager) QHost() string {
	return m.qHost
}

func (m *KiroAuthManager) isTokenExpiringSoon() bool {
	if m.expiresAt.IsZero() {
		return true
	}
	threshold := time.Now().UTC().Add(TokenRefreshThreshold * time.Second)
	return !m.expiresAt.After(threshold)
}

func (m *KiroAuthManager) isTokenExpired() bool {
	if m.expiresAt.IsZero() {
		return true
	}
	return !time.Now().UTC().Before(m.expiresAt)
}

func (m *KiroAuthManager) handleRefreshError(err error) (string, error) {
	if m.sqliteDB != "" && isHTTP400(err) {
		slog.Warn("token refresh failed with 400 after sqlite reload")
		if m.accessToken != "" && !m.isTokenExpired() {
			slog.Warn("using existing access token until it expires")
			return m.accessToken, nil
		}
		return "", fmt.Errorf("auth: token expired and refresh failed, run 'kiro-cli login': %w", err)
	}
	return "", fmt.Errorf("auth: token refresh failed: %w", err)
}

func (m *KiroAuthManager) setRegionURLs(region string) {
	m.refreshURL = fmt.Sprintf("https://prod.%s.auth.desktop.kiro.dev/refreshToken", region)
	m.apiHost = fmt.Sprintf("https://q.%s.amazonaws.com", region)
	m.qHost = fmt.Sprintf("https://q.%s.amazonaws.com", region)
}

func buildSSOOIDCURL(region string) string {
	return fmt.Sprintf("https://oidc.%s.amazonaws.com/token", region)
}

func (m *KiroAuthManager) detectAuthType() {
	if m.clientID != "" && m.clientSecret != "" {
		m.authType = AWSSSO
	} else {
		m.authType = KiroDesktop
	}
}

func (m *KiroAuthManager) refreshTokenRequest(ctx context.Context) error {
	if m.authType == AWSSSO {
		return m.refreshAWSSSOOIDC(ctx)
	}
	return m.refreshKiroDesktop(ctx)
}

func (m *KiroAuthManager) refreshKiroDesktop(ctx context.Context) error {
	if m.refreshToken == "" {
		return fmt.Errorf("refresh token is not set")
	}

	slog.Info("refreshing token via Kiro Desktop Auth")

	payload, err := json.Marshal(map[string]string{
		"refreshToken": m.refreshToken,
	})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.refreshURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "KiroIDE-0.7.45-"+m.fingerprint)

	resp, err := m.httpClient.Do(req) //nolint:gosec // URL from trusted config
	if err != nil {
		return fmt.Errorf("refresh request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var result kiroDesktopRefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return m.applyDesktopRefresh(result)
}

func (m *KiroAuthManager) applyDesktopRefresh(result kiroDesktopRefreshResponse) error {
	if result.AccessToken == "" {
		return fmt.Errorf("response does not contain accessToken")
	}

	m.accessToken = result.AccessToken
	if result.RefreshToken != "" {
		m.refreshToken = result.RefreshToken
	}
	if result.ProfileARN != "" {
		m.profileARN = result.ProfileARN
	}

	expiresIn := result.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 3600
	}
	m.expiresAt = time.Now().UTC().Add(time.Duration(expiresIn-tokenExpiryBuffer) * time.Second)

	slog.Info("token refreshed via Kiro Desktop", "expires_at", m.expiresAt.Format(time.RFC3339))
	m.saveCredentials()
	return nil
}

func (m *KiroAuthManager) refreshAWSSSOOIDC(ctx context.Context) error {
	err := m.doAWSSSOOIDCRefresh(ctx)
	if err == nil {
		return nil
	}

	if isHTTP400(err) && m.sqliteDB != "" {
		slog.Warn("SSO OIDC refresh failed with 400, reloading from SQLite and retrying")
		m.loadFromSQLite(m.sqliteDB)
		return m.doAWSSSOOIDCRefresh(ctx)
	}

	return err
}

func (m *KiroAuthManager) doAWSSSOOIDCRefresh(ctx context.Context) error {
	if err := m.validateSSOCredentials(); err != nil {
		return err
	}

	slog.Info("refreshing token via AWS SSO OIDC")

	payload, err := json.Marshal(map[string]string{
		"grantType":    "refresh_token",
		"clientId":     m.clientID,
		"clientSecret": m.clientSecret,
		"refreshToken": m.refreshToken,
	})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.ssoOIDCURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req) //nolint:gosec // URL from trusted config
	if err != nil {
		return fmt.Errorf("SSO OIDC request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("SSO OIDC refresh failed", "status", resp.StatusCode, "body", string(body)) //nolint:gosec // logging error response
		return &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var result ssoOIDCRefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode SSO response: %w", err)
	}

	return m.applySSORefresh(result)
}

func (m *KiroAuthManager) validateSSOCredentials() error {
	if m.refreshToken == "" {
		return fmt.Errorf("refresh token is not set")
	}
	if m.clientID == "" {
		return fmt.Errorf("client ID is not set (required for AWS SSO OIDC)")
	}
	if m.clientSecret == "" {
		return fmt.Errorf("client secret is not set (required for AWS SSO OIDC)")
	}
	return nil
}

func (m *KiroAuthManager) applySSORefresh(result ssoOIDCRefreshResponse) error {
	if result.AccessToken == "" {
		return fmt.Errorf("SSO OIDC response does not contain accessToken")
	}

	m.accessToken = result.AccessToken
	if result.RefreshToken != "" {
		m.refreshToken = result.RefreshToken
	}

	expiresIn := result.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 3600
	}
	m.expiresAt = time.Now().UTC().Add(time.Duration(expiresIn-tokenExpiryBuffer) * time.Second)

	slog.Info("token refreshed via SSO OIDC", "expires_at", m.expiresAt.Format(time.RFC3339))
	m.saveCredentials()
	return nil
}

func (m *KiroAuthManager) loadFromSQLite(dbPath string) {
	expandedPath := expandPath(dbPath)
	if _, err := os.Stat(expandedPath); os.IsNotExist(err) {
		slog.Warn("sqlite database not found", "path", dbPath)
		return
	}

	db, err := sql.Open("sqlite", expandedPath)
	if err != nil {
		slog.Error("failed to open sqlite database", "error", err)
		return
	}
	defer func() { _ = db.Close() }()

	m.loadTokenFromSQLite(db)
	m.loadDeviceRegistrationFromSQLite(db)
	slog.Info("credentials loaded from sqlite", "path", dbPath)
}

func (m *KiroAuthManager) loadTokenFromSQLite(db *sql.DB) {
	for _, key := range sqliteTokenKeys {
		var value string
		err := db.QueryRow("SELECT value FROM auth_kv WHERE key = ?", key).Scan(&value)
		if err != nil {
			continue
		}

		var data sqliteTokenData
		if err := json.Unmarshal([]byte(value), &data); err != nil {
			slog.Error("failed to parse sqlite token data", "key", key, "error", err)
			continue
		}

		m.sqliteTokenKey = key
		m.applyTokenData(data)
		slog.Debug("loaded credentials from sqlite key", "key", key)
		return
	}
}

func (m *KiroAuthManager) applyTokenData(data sqliteTokenData) {
	if data.AccessToken != "" {
		m.accessToken = data.AccessToken
	}
	if data.RefreshToken != "" {
		m.refreshToken = data.RefreshToken
	}
	if data.ProfileARN != "" {
		m.profileARN = data.ProfileARN
	}
	if data.Region != "" {
		m.ssoRegion = data.Region
		m.ssoOIDCURL = buildSSOOIDCURL(data.Region)
		slog.Debug("SSO region from sqlite", "sso_region", m.ssoRegion, "api_region", m.region)
	}
	if len(data.Scopes) > 0 {
		m.scopes = data.Scopes
	}
	if data.ExpiresAt != "" {
		t, err := parseExpiresAt(data.ExpiresAt)
		if err != nil {
			slog.Warn("failed to parse expires_at from sqlite", "error", err)
		} else {
			m.expiresAt = t
		}
	}
}

func (m *KiroAuthManager) loadDeviceRegistrationFromSQLite(db *sql.DB) {
	for _, key := range sqliteRegistrationKeys {
		var value string
		err := db.QueryRow("SELECT value FROM auth_kv WHERE key = ?", key).Scan(&value)
		if err != nil {
			continue
		}

		var data sqliteRegistrationData
		if err := json.Unmarshal([]byte(value), &data); err != nil {
			slog.Error("failed to parse sqlite registration data", "key", key, "error", err)
			continue
		}

		if data.ClientID != "" {
			m.clientID = data.ClientID
		}
		if data.ClientSecret != "" {
			m.clientSecret = data.ClientSecret
		}
		if data.Region != "" && m.ssoRegion == "" {
			m.ssoRegion = data.Region
			m.ssoOIDCURL = buildSSOOIDCURL(data.Region)
			slog.Debug("SSO region from device-registration", "sso_region", m.ssoRegion)
		}
		slog.Debug("loaded device registration from sqlite", "key", key)
		return
	}
}

func (m *KiroAuthManager) loadFromFile(filePath string) {
	expandedPath := expandPath(filePath)
	rawData, err := os.ReadFile(expandedPath) //nolint:gosec // path from trusted config
	if err != nil {
		slog.Warn("credentials file not found or unreadable", "path", filePath, "error", err)
		return
	}

	var data jsonCredsFile
	if err := json.Unmarshal(rawData, &data); err != nil {
		slog.Error("failed to parse credentials file", "path", filePath, "error", err)
		return
	}

	m.applyJSONCredsData(data)

	if data.ClientIDHash != "" {
		m.clientIDHash = data.ClientIDHash
		m.loadEnterpriseDeviceRegistration(data.ClientIDHash)
	}
	if data.ClientID != "" {
		m.clientID = data.ClientID
	}
	if data.ClientSecret != "" {
		m.clientSecret = data.ClientSecret
	}

	slog.Info("credentials loaded from file", "path", filePath)
}

func (m *KiroAuthManager) applyJSONCredsData(data jsonCredsFile) {
	if data.RefreshToken != "" {
		m.refreshToken = data.RefreshToken
	}
	if data.AccessToken != "" {
		m.accessToken = data.AccessToken
	}
	if data.ProfileARN != "" {
		m.profileARN = data.ProfileARN
	}
	if data.Region != "" {
		m.region = data.Region
		m.setRegionURLs(data.Region)
		m.ssoOIDCURL = buildSSOOIDCURL(data.Region)
		slog.Info("region updated from credentials file", "region", m.region)
	}
	if data.ExpiresAt != "" {
		t, err := parseExpiresAt(data.ExpiresAt)
		if err != nil {
			slog.Warn("failed to parse expiresAt from file", "error", err)
		} else {
			m.expiresAt = t
		}
	}
}

func (m *KiroAuthManager) loadEnterpriseDeviceRegistration(clientIDHash string) {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("failed to get home directory for enterprise registration", "error", err)
		return
	}

	regPath := filepath.Join(home, ".aws", "sso", "cache", clientIDHash+".json")
	rawData, err := os.ReadFile(regPath) //nolint:gosec // path from trusted config
	if err != nil {
		slog.Warn("enterprise device registration file not found", "path", regPath)
		return
	}

	var data struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"` //nolint:gosec // JSON field name
	}
	if err := json.Unmarshal(rawData, &data); err != nil {
		slog.Error("failed to parse enterprise device registration", "error", err)
		return
	}

	if data.ClientID != "" {
		m.clientID = data.ClientID
	}
	if data.ClientSecret != "" {
		m.clientSecret = data.ClientSecret
	}
	slog.Info("enterprise device registration loaded", "path", regPath)
}

func (m *KiroAuthManager) saveCredentials() {
	if m.sqliteDB != "" {
		if err := m.saveToSQLite(); err != nil {
			slog.Error("failed to save credentials to sqlite", "error", err)
		}
	} else if err := m.saveToFile(); err != nil {
		slog.Error("failed to save credentials to file", "error", err)
	}
}

func (m *KiroAuthManager) saveToSQLite() error {
	if m.sqliteDB == "" {
		return nil
	}

	expandedPath := expandPath(m.sqliteDB)
	if _, err := os.Stat(expandedPath); os.IsNotExist(err) {
		return fmt.Errorf("sqlite database not found: %s", m.sqliteDB)
	}

	db, err := sql.Open("sqlite", expandedPath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		slog.Warn("failed to set sqlite busy_timeout", "error", err)
	}

	tokenJSON, err := m.buildSQLiteTokenJSON()
	if err != nil {
		return err
	}

	return m.writeSQLiteToken(db, tokenJSON)
}

func (m *KiroAuthManager) buildSQLiteTokenJSON() (string, error) {
	tokenData := map[string]interface{}{
		"access_token":  m.accessToken,
		"refresh_token": m.refreshToken,
	}
	if !m.expiresAt.IsZero() {
		tokenData["expires_at"] = m.expiresAt.Format(time.RFC3339)
	}

	region := m.ssoRegion
	if region == "" {
		region = m.region
	}
	tokenData["region"] = region

	if len(m.scopes) > 0 {
		tokenData["scopes"] = m.scopes
	}

	raw, err := json.Marshal(tokenData)
	if err != nil {
		return "", fmt.Errorf("marshal token data: %w", err)
	}
	return string(raw), nil
}

func (m *KiroAuthManager) writeSQLiteToken(db *sql.DB, tokenJSON string) error {
	if m.sqliteTokenKey != "" {
		result, err := db.Exec("UPDATE auth_kv SET value = ? WHERE key = ?", tokenJSON, m.sqliteTokenKey)
		if err == nil {
			if n, _ := result.RowsAffected(); n > 0 {
				slog.Debug("credentials saved to sqlite", "key", m.sqliteTokenKey)
				return nil
			}
		}
	}

	for _, key := range sqliteTokenKeys {
		result, err := db.Exec("UPDATE auth_kv SET value = ? WHERE key = ?", tokenJSON, key)
		if err != nil {
			continue
		}
		if n, _ := result.RowsAffected(); n > 0 {
			slog.Debug("credentials saved to sqlite (fallback)", "key", key)
			return nil
		}
	}

	return fmt.Errorf("no matching keys found in sqlite")
}

func (m *KiroAuthManager) saveToFile() error {
	if m.credsFile == "" {
		return nil
	}

	expandedPath := expandPath(m.credsFile)

	existing := make(map[string]interface{})
	if rawData, err := os.ReadFile(expandedPath); err == nil { //nolint:gosec // path from trusted config
		_ = json.Unmarshal(rawData, &existing)
	}

	existing["accessToken"] = m.accessToken
	existing["refreshToken"] = m.refreshToken
	if !m.expiresAt.IsZero() {
		existing["expiresAt"] = m.expiresAt.Format(time.RFC3339)
	}
	if m.profileARN != "" {
		existing["profileArn"] = m.profileARN
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	if err := os.WriteFile(expandedPath, data, 0o600); err != nil {
		return fmt.Errorf("write credentials file: %w", err)
	}

	slog.Debug("credentials saved to file", "path", m.credsFile)
	return nil
}

//nolint:gosec // JSON field names, not hardcoded credentials
type kiroDesktopRefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
	ProfileARN   string `json:"profileArn"`
}

//nolint:gosec // JSON field names, not hardcoded credentials
type ssoOIDCRefreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
}

//nolint:gosec // JSON field names, not hardcoded credentials
type sqliteTokenData struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	ProfileARN   string   `json:"profile_arn"`
	Region       string   `json:"region"`
	Scopes       []string `json:"scopes"`
	ExpiresAt    string   `json:"expires_at"`
}

//nolint:gosec // JSON field names, not hardcoded credentials
type sqliteRegistrationData struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Region       string `json:"region"`
}

//nolint:gosec // JSON field names, not hardcoded credentials
type jsonCredsFile struct {
	RefreshToken string `json:"refreshToken"`
	AccessToken  string `json:"accessToken"`
	ProfileARN   string `json:"profileArn"`
	Region       string `json:"region"`
	ExpiresAt    string `json:"expiresAt"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	ClientIDHash string `json:"clientIdHash"`
}

// HTTPError represents an HTTP error response from a token refresh endpoint.
type HTTPError struct {
	StatusCode int
	Body       string
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

func isHTTP400(err error) bool {
	he, ok := err.(*HTTPError)
	return ok && he.StatusCode == http.StatusBadRequest
}

func generateFingerprint() string {
	hostname, err := os.Hostname()
	if err != nil {
		return sha256Hex("default-kiro-gateway")
	}
	u, err := user.Current()
	if err != nil {
		return sha256Hex("default-kiro-gateway")
	}
	return sha256Hex(fmt.Sprintf("%s-%s-kiro-gateway", hostname, u.Username))
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func parseExpiresAt(s string) (time.Time, error) {
	if strings.HasSuffix(s, "Z") {
		return time.Parse(time.RFC3339, s)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}
	t, err = time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("unsupported date format: %s", s)
	}
	return t.UTC(), nil
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func newHTTPClient(proxyURL string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	return &http.Client{
		Timeout:   httpTimeout,
		Transport: transport,
	}
}
