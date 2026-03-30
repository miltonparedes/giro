package auth

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- test helpers ---

// createTempFile creates a regular file with minimal content at the given
// path inside dir. Parent directories are created as needed. Returns the
// absolute path.
func createTempFile(t *testing.T, dir, relPath string) string {
	t.Helper()
	path := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// createValidSQLiteFile creates a real SQLite database with an auth_kv table
// and one usable token entry at the given relative path inside dir. Parent
// directories are created as needed. Returns the absolute path.
func createValidSQLiteFile(t *testing.T, dir, relPath string) string {
	t.Helper()
	path := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("CREATE TABLE auth_kv (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}

	tokenData, err := json.Marshal(map[string]string{
		"access_token":  "fixture-access",
		"refresh_token": "fixture-refresh",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(
		"INSERT INTO auth_kv (key, value) VALUES (?, ?)",
		"kirocli:social:token", string(tokenData),
	); err != nil {
		t.Fatal(err)
	}

	return path
}

// createValidJSONCredsFile creates a JSON credentials file containing at
// least a refreshToken at the given relative path inside dir. Parent
// directories are created as needed. Returns the absolute path.
func createValidJSONCredsFile(t *testing.T, dir, relPath string) string {
	t.Helper()
	path := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	content := `{"refreshToken":"fixture-refresh","accessToken":"fixture-access"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// createTempKiroCLIStore creates the platform-default kiro-cli SQLite file
// under the given home directory with a valid auth_kv table and token entry.
// Returns the full path.
func createTempKiroCLIStore(t *testing.T, homeDir string) string {
	t.Helper()
	return createValidSQLiteFile(t, homeDir, defaultKiroCLIDBRelPath())
}

// defaultKiroCLIDBRelPath returns the relative path to the kiro-cli store
// for the current platform, useful for constructing temp fixtures.
func defaultKiroCLIDBRelPath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join("Library", "Application Support", "kiro-cli", "data.sqlite3")
	}
	return filepath.Join(".local", "share", "kiro-cli", "data.sqlite3")
}

// kiroIDECredsRelPath returns the relative path to the kiro-ide credentials
// file, useful for constructing temp fixtures.
func kiroIDECredsRelPath() string {
	return filepath.Join(".kiro", "credentials.json")
}

// createTempKiroIDEStore creates the platform-default kiro-ide credentials
// file under the given home directory with a valid refreshToken. Returns the
// full path.
func createTempKiroIDEStore(t *testing.T, homeDir string) string {
	t.Helper()
	return createValidJSONCredsFile(t, homeDir, kiroIDECredsRelPath())
}

// createTempKiroIDEStoreWithContent creates the kiro-ide credentials file
// with the given content. Returns the full path.
func createTempKiroIDEStoreWithContent(t *testing.T, homeDir, content string) string {
	t.Helper()
	relPath := kiroIDECredsRelPath()
	path := filepath.Join(homeDir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- ResolveSource: explicit-source precedence tests ---

func TestResolveSource_Precedence_SQLiteWins(t *testing.T) {
	dir := t.TempDir()
	sqlitePath := createValidSQLiteFile(t, dir, "cli.sqlite3")
	credsPath := createValidJSONCredsFile(t, dir, "creds.json")

	in := ResolveInput{
		KiroCLIDBFile: sqlitePath,
		KiroCredsFile: credsPath,
		RefreshToken:  "tok-123",
		HomeDir:       t.TempDir(), // isolate autodetection
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvSQLite {
		t.Errorf("Kind = %q, want %q", src.Kind, SourceEnvSQLite)
	}
	if src.Path != sqlitePath {
		t.Errorf("Path = %q, want %q", src.Path, sqlitePath)
	}
	if !src.Writable {
		t.Error("Writable = false, want true")
	}
}

func TestResolveSource_Precedence_CredsFileOverToken(t *testing.T) {
	dir := t.TempDir()
	credsPath := createValidJSONCredsFile(t, dir, "creds.json")

	in := ResolveInput{
		KiroCredsFile: credsPath,
		RefreshToken:  "tok-123",
		HomeDir:       t.TempDir(),
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvCredsFile {
		t.Errorf("Kind = %q, want %q", src.Kind, SourceEnvCredsFile)
	}
	if src.Path != credsPath {
		t.Errorf("Path = %q, want %q", src.Path, credsPath)
	}
	if !src.Writable {
		t.Error("Writable = false, want true")
	}
}

func TestResolveSource_RefreshTokenOnly(t *testing.T) {
	in := ResolveInput{
		RefreshToken: "tok-123",
		HomeDir:      t.TempDir(),
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvRefreshToken {
		t.Errorf("Kind = %q, want %q", src.Kind, SourceEnvRefreshToken)
	}
	if src.Path != "" {
		t.Errorf("Path = %q, want empty", src.Path)
	}
	if src.Writable {
		t.Error("Writable = true, want false")
	}
}

// --- ResolveSource: fail-closed tests ---

func TestResolveSource_NoSource_FailClosed(t *testing.T) {
	in := ResolveInput{
		HomeDir: t.TempDir(), // empty temp dir, no autodetected stores
	}

	src, err := ResolveSource(in)
	if err == nil {
		t.Fatal("expected error when no source is configured or autodetectable")
	}
	if src != nil {
		t.Errorf("expected nil source, got %+v", src)
	}
	if !strings.Contains(err.Error(), "no credential source resolved") {
		t.Errorf("error message should explain missing source, got: %s", err.Error())
	}
}

func TestResolveSource_InvalidExplicitAndNoAutodetect_FailClosed(t *testing.T) {
	in := ResolveInput{
		KiroCLIDBFile: "/nonexistent/path/cli.sqlite3",
		KiroCredsFile: "/nonexistent/path/creds.json",
		HomeDir:       t.TempDir(),
	}

	src, err := ResolveSource(in)
	if err == nil {
		t.Fatal("expected error when all explicit sources are invalid and no autodetected stores exist")
	}
	if src != nil {
		t.Errorf("expected nil source, got %+v", src)
	}
}

// --- ResolveSource: autodetection tests ---

func TestResolveSource_AutodetectKiroCLI(t *testing.T) {
	homeDir := t.TempDir()
	storePath := createTempKiroCLIStore(t, homeDir)

	in := ResolveInput{
		HomeDir: homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceKiroCLI {
		t.Errorf("Kind = %q, want %q", src.Kind, SourceKiroCLI)
	}
	if src.Path != storePath {
		t.Errorf("Path = %q, want %q", src.Path, storePath)
	}
	if !src.Writable {
		t.Error("Writable = false, want true")
	}
}

func TestResolveSource_AutodetectKiroCLI_MissingStore(t *testing.T) {
	in := ResolveInput{
		HomeDir: t.TempDir(), // no kiro-cli store
	}

	src, err := ResolveSource(in)
	if err == nil {
		t.Fatal("expected error when no autodetected store exists")
	}
	if src != nil {
		t.Errorf("expected nil source, got %+v", src)
	}
}

// --- ResolveSource: kiro-ide autodetection tests ---

// giro autodetects and uses the default kiro-ide store.
func TestResolveSource_AutodetectKiroIDE(t *testing.T) {
	homeDir := t.TempDir()
	storePath := createTempKiroIDEStore(t, homeDir)

	in := ResolveInput{
		HomeDir: homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceKiroIDE {
		t.Errorf("Kind = %q, want %q", src.Kind, SourceKiroIDE)
	}
	if src.Path != storePath {
		t.Errorf("Path = %q, want %q", src.Path, storePath)
	}
	if !src.Writable {
		t.Error("Writable = false, want true")
	}
}

func TestResolveSource_AutodetectKiroIDE_MissingStore(t *testing.T) {
	in := ResolveInput{
		HomeDir: t.TempDir(), // no kiro-cli or kiro-ide store
	}

	src, err := ResolveSource(in)
	if err == nil {
		t.Fatal("expected error when no autodetected store exists")
	}
	if src != nil {
		t.Errorf("expected nil source, got %+v", src)
	}
	if !strings.Contains(err.Error(), "kiro-ide") {
		t.Errorf("error should mention kiro-ide, got: %s", err.Error())
	}
}

// --- ResolveSource: autodetected precedence tests ---

// When both autodetected stores are healthy, kiro-cli wins
// deterministically.
func TestResolveSource_AutodetectKiroCLIWinsOverKiroIDE(t *testing.T) {
	homeDir := t.TempDir()
	kiroCLIPath := createTempKiroCLIStore(t, homeDir)
	createTempKiroIDEStore(t, homeDir) // also present

	in := ResolveInput{
		HomeDir: homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceKiroCLI {
		t.Errorf("Kind = %q, want %q (kiro-cli should beat kiro-ide)", src.Kind, SourceKiroCLI)
	}
	if src.Path != kiroCLIPath {
		t.Errorf("Path = %q, want %q", src.Path, kiroCLIPath)
	}
}

// Deterministic precedence across repeated runs.
func TestResolveSource_AutodetectPrecedenceDeterministic(t *testing.T) {
	homeDir := t.TempDir()
	createTempKiroCLIStore(t, homeDir)
	createTempKiroIDEStore(t, homeDir)

	in := ResolveInput{HomeDir: homeDir}

	// Run resolution twice and confirm the winner is identical.
	src1, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}
	src2, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src1.Kind != src2.Kind || src1.Path != src2.Path {
		t.Errorf("non-deterministic: run1=%+v, run2=%+v", src1, src2)
	}
	if src1.Kind != SourceKiroCLI {
		t.Errorf("expected kiro-cli to win deterministically, got %q", src1.Kind)
	}
}

// --- ResolveSource: autodetected fallback tests ---

// A broken higher-priority autodetected source falls back
// safely to the next viable autodetected source.
func TestResolveSource_BrokenKiroCLIFallsToKiroIDE(t *testing.T) {
	homeDir := t.TempDir()

	// Create a directory at the kiro-cli path (broken: should be a file).
	kiroCLIPath := defaultKiroCLIDBPath(homeDir)
	if err := os.MkdirAll(kiroCLIPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a valid kiro-ide store.
	kiroIDEPath := createTempKiroIDEStore(t, homeDir)

	in := ResolveInput{
		HomeDir: homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatalf("expected fallback to kiro-ide, got error: %v", err)
	}

	if src.Kind != SourceKiroIDE {
		t.Errorf("Kind = %q, want %q (should fall back from broken kiro-cli)", src.Kind, SourceKiroIDE)
	}
	if src.Path != kiroIDEPath {
		t.Errorf("Path = %q, want %q", src.Path, kiroIDEPath)
	}
}

// Both autodetected sources broken fails closed.
func TestResolveSource_BothAutodetectedBroken_FailClosed(t *testing.T) {
	homeDir := t.TempDir()

	// Create directories at both paths (broken: should be files).
	kiroCLIPath := defaultKiroCLIDBPath(homeDir)
	if err := os.MkdirAll(kiroCLIPath, 0o755); err != nil {
		t.Fatal(err)
	}
	kiroIDEPath := defaultKiroIDECredsPath(homeDir)
	if err := os.MkdirAll(kiroIDEPath, 0o755); err != nil {
		t.Fatal(err)
	}

	in := ResolveInput{
		HomeDir: homeDir,
	}

	src, err := ResolveSource(in)
	if err == nil {
		t.Fatal("expected error when both autodetected sources are broken")
	}
	if src != nil {
		t.Errorf("expected nil source, got %+v", src)
	}
}

// An invalid explicit source falls back safely to
// autodetected kiro-ide.
func TestResolveSource_InvalidExplicitFallsToAutodetectedKiroIDE(t *testing.T) {
	homeDir := t.TempDir()
	kiroIDEPath := createTempKiroIDEStore(t, homeDir)

	in := ResolveInput{
		KiroCLIDBFile: "/nonexistent/explicit.sqlite3",
		KiroCredsFile: "/nonexistent/explicit.json",
		HomeDir:       homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatalf("expected fallback to kiro-ide, got error: %v", err)
	}

	if src.Kind != SourceKiroIDE {
		t.Errorf("Kind = %q, want %q", src.Kind, SourceKiroIDE)
	}
	if src.Path != kiroIDEPath {
		t.Errorf("Path = %q, want %q", src.Path, kiroIDEPath)
	}
}

// Enterprise-style kiro-ide store with companion
// registration material resolves successfully.
func TestResolveSource_EnterpriseKiroIDE(t *testing.T) {
	homeDir := t.TempDir()
	enterpriseContent := `{
		"refreshToken": "enterprise-refresh",
		"accessToken": "enterprise-access",
		"clientIdHash": "abc123def456",
		"region": "us-east-1"
	}`
	storePath := createTempKiroIDEStoreWithContent(t, homeDir, enterpriseContent)

	in := ResolveInput{
		HomeDir: homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceKiroIDE {
		t.Errorf("Kind = %q, want %q", src.Kind, SourceKiroIDE)
	}
	if src.Path != storePath {
		t.Errorf("Path = %q, want %q", src.Path, storePath)
	}
	if !src.Writable {
		t.Error("Writable = false, want true for enterprise kiro-ide")
	}
}

// --- ResolveSource: explicit vs autodetected precedence ---

// Explicit environment configuration takes precedence over
// autodetection (including kiro-ide).
func TestResolveSource_ExplicitBeatsAutodetected(t *testing.T) {
	homeDir := t.TempDir()
	createTempKiroCLIStore(t, homeDir) // valid autodetected kiro-cli
	createTempKiroIDEStore(t, homeDir) // valid autodetected kiro-ide

	dir := t.TempDir()
	sqlitePath := createValidSQLiteFile(t, dir, "explicit.sqlite3")

	in := ResolveInput{
		KiroCLIDBFile: sqlitePath,
		HomeDir:       homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvSQLite {
		t.Errorf("Kind = %q, want %q (explicit should beat autodetected)", src.Kind, SourceEnvSQLite)
	}
	if src.Path != sqlitePath {
		t.Errorf("Path = %q, want %q", src.Path, sqlitePath)
	}
}

func TestResolveSource_ExplicitCredsFileBeatsAutodetectedKiroIDE(t *testing.T) {
	homeDir := t.TempDir()
	createTempKiroIDEStore(t, homeDir) // valid autodetected kiro-ide

	dir := t.TempDir()
	credsPath := createValidJSONCredsFile(t, dir, "explicit-creds.json")

	in := ResolveInput{
		KiroCredsFile: credsPath,
		HomeDir:       homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvCredsFile {
		t.Errorf("Kind = %q, want %q (explicit creds file should beat kiro-ide)", src.Kind, SourceEnvCredsFile)
	}
}

func TestResolveSource_RefreshTokenBeatsAutodetected(t *testing.T) {
	homeDir := t.TempDir()
	createTempKiroCLIStore(t, homeDir) // valid autodetected kiro-cli
	createTempKiroIDEStore(t, homeDir) // valid autodetected kiro-ide

	in := ResolveInput{
		RefreshToken: "tok-explicit",
		HomeDir:      homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvRefreshToken {
		t.Errorf("Kind = %q, want %q (explicit token should beat autodetected)", src.Kind, SourceEnvRefreshToken)
	}
}

// --- ResolveSource: fallback from invalid explicit sources ---

func TestResolveSource_InvalidExplicitFallsToAutodetected(t *testing.T) {
	homeDir := t.TempDir()
	storePath := createTempKiroCLIStore(t, homeDir)

	in := ResolveInput{
		KiroCLIDBFile: "/nonexistent/explicit.sqlite3",
		HomeDir:       homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceKiroCLI {
		t.Errorf("Kind = %q, want %q (should fall back to autodetected)", src.Kind, SourceKiroCLI)
	}
	if src.Path != storePath {
		t.Errorf("Path = %q, want %q", src.Path, storePath)
	}
}

func TestResolveSource_InvalidExplicitFallsToRefreshToken(t *testing.T) {
	in := ResolveInput{
		KiroCLIDBFile: "/nonexistent/explicit.sqlite3",
		RefreshToken:  "tok-fallback",
		HomeDir:       t.TempDir(),
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvRefreshToken {
		t.Errorf("Kind = %q, want %q (should fall back to refresh token)", src.Kind, SourceEnvRefreshToken)
	}
}

func TestResolveSource_InvalidExplicitFallsToNextExplicit(t *testing.T) {
	dir := t.TempDir()
	credsPath := createValidJSONCredsFile(t, dir, "creds.json")

	in := ResolveInput{
		KiroCLIDBFile: "/nonexistent/explicit.sqlite3", // invalid
		KiroCredsFile: credsPath,                       // valid
		HomeDir:       t.TempDir(),
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvCredsFile {
		t.Errorf("Kind = %q, want %q (invalid SQLite should fall to creds file)", src.Kind, SourceEnvCredsFile)
	}
	if src.Path != credsPath {
		t.Errorf("Path = %q, want %q", src.Path, credsPath)
	}
}

func TestResolveSource_DirectoryIsRejected(t *testing.T) {
	dir := t.TempDir() // a directory, not a file

	in := ResolveInput{
		KiroCLIDBFile: dir,
		HomeDir:       t.TempDir(),
	}

	src, err := ResolveSource(in)
	if err == nil {
		t.Fatal("expected error when pointed at a directory")
	}
	if src != nil {
		t.Errorf("expected nil source, got %+v", src)
	}
}

// --- ResolveSource: path expansion ---

func TestResolveSource_AbsolutePathUnchanged(t *testing.T) {
	dir := t.TempDir()
	credsPath := createValidJSONCredsFile(t, dir, "creds.json")

	in := ResolveInput{
		KiroCredsFile: credsPath,
		HomeDir:       t.TempDir(),
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Path != credsPath {
		t.Errorf("Path = %q, want %q", src.Path, credsPath)
	}
}

func TestResolveSource_TildePathExpanded(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}

	testDir := filepath.Join(home, ".giro-test-tilde-expansion")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(testDir) })

	dbPath := filepath.Join(testDir, "cli.sqlite3")

	// Create a valid SQLite DB so loadability validation passes.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE auth_kv (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	tokenData, _ := json.Marshal(map[string]string{
		"access_token": "tilde-access", "refresh_token": "tilde-refresh",
	})
	if _, err := db.Exec(
		"INSERT INTO auth_kv (key, value) VALUES (?, ?)",
		"kirocli:social:token", string(tokenData),
	); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()

	in := ResolveInput{
		KiroCLIDBFile: "~/.giro-test-tilde-expansion/cli.sqlite3",
		HomeDir:       t.TempDir(), // isolate autodetection
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Path == "~/.giro-test-tilde-expansion/cli.sqlite3" {
		t.Error("path was not expanded")
	}
	if !filepath.IsAbs(src.Path) {
		t.Errorf("expanded path is not absolute: %q", src.Path)
	}
	if src.Path != dbPath {
		t.Errorf("Path = %q, want %q", src.Path, dbPath)
	}
}

// --- ResolvedSource: metadata table test ---

func TestResolvedSource_Metadata(t *testing.T) {
	dir := t.TempDir()
	sqlitePath := createValidSQLiteFile(t, dir, "db.sqlite3")
	credsPath := createValidJSONCredsFile(t, dir, "creds.json")

	// Create kiro-cli and kiro-ide fixtures in separate home dirs.
	kiroCLIHome := t.TempDir()
	createTempKiroCLIStore(t, kiroCLIHome)
	kiroIDEHome := t.TempDir()
	createTempKiroIDEStore(t, kiroIDEHome)

	tests := []struct {
		name     string
		input    ResolveInput
		kind     SourceKind
		writable bool
		hasPath  bool
	}{
		{
			name:     "env-sqlite",
			input:    ResolveInput{KiroCLIDBFile: sqlitePath, HomeDir: t.TempDir()},
			kind:     SourceEnvSQLite,
			writable: true,
			hasPath:  true,
		},
		{
			name:     "env-creds-file",
			input:    ResolveInput{KiroCredsFile: credsPath, HomeDir: t.TempDir()},
			kind:     SourceEnvCredsFile,
			writable: true,
			hasPath:  true,
		},
		{
			name:     "env-refresh-token",
			input:    ResolveInput{RefreshToken: "tok", HomeDir: t.TempDir()},
			kind:     SourceEnvRefreshToken,
			writable: false,
			hasPath:  false,
		},
		{
			name:     "autodetected-kiro-cli",
			input:    ResolveInput{HomeDir: kiroCLIHome},
			kind:     SourceKiroCLI,
			writable: true,
			hasPath:  true,
		},
		{
			name:     "autodetected-kiro-ide",
			input:    ResolveInput{HomeDir: kiroIDEHome},
			kind:     SourceKiroIDE,
			writable: true,
			hasPath:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := ResolveSource(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if src.Kind != tt.kind {
				t.Errorf("Kind = %q, want %q", src.Kind, tt.kind)
			}
			if src.Writable != tt.writable {
				t.Errorf("Writable = %v, want %v", src.Writable, tt.writable)
			}
			if tt.hasPath && src.Path == "" {
				t.Error("expected non-empty Path")
			}
			if !tt.hasPath && src.Path != "" {
				t.Errorf("expected empty Path, got %q", src.Path)
			}
		})
	}
}

// --- ResolveSource: backward-compatible precedence (table-driven) ---
// Backward-compatible precedence: KIRO_CLI_DB_FILE > KIRO_CREDS_FILE > REFRESH_TOKEN

func TestResolveSource_BackwardCompatiblePrecedence(t *testing.T) {
	dir := t.TempDir()
	sqlitePath := createValidSQLiteFile(t, dir, "cli.sqlite3")
	credsPath := createValidJSONCredsFile(t, dir, "creds.json")

	tests := []struct {
		name     string
		input    ResolveInput
		wantKind SourceKind
	}{
		{
			name: "all-three-explicit: sqlite wins",
			input: ResolveInput{
				KiroCLIDBFile: sqlitePath,
				KiroCredsFile: credsPath,
				RefreshToken:  "tok",
				HomeDir:       t.TempDir(),
			},
			wantKind: SourceEnvSQLite,
		},
		{
			name: "creds-file-and-token: creds-file wins",
			input: ResolveInput{
				KiroCredsFile: credsPath,
				RefreshToken:  "tok",
				HomeDir:       t.TempDir(),
			},
			wantKind: SourceEnvCredsFile,
		},
		{
			name: "token-only: token wins",
			input: ResolveInput{
				RefreshToken: "tok",
				HomeDir:      t.TempDir(),
			},
			wantKind: SourceEnvRefreshToken,
		},
		{
			name: "invalid-sqlite-then-valid-creds: creds-file wins",
			input: ResolveInput{
				KiroCLIDBFile: "/nonexistent/bad.sqlite3",
				KiroCredsFile: credsPath,
				RefreshToken:  "tok",
				HomeDir:       t.TempDir(),
			},
			wantKind: SourceEnvCredsFile,
		},
		{
			name: "invalid-sqlite-invalid-creds-then-token: token wins",
			input: ResolveInput{
				KiroCLIDBFile: "/nonexistent/bad.sqlite3",
				KiroCredsFile: "/nonexistent/bad.json",
				RefreshToken:  "tok",
				HomeDir:       t.TempDir(),
			},
			wantKind: SourceEnvRefreshToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := ResolveSource(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if src.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", src.Kind, tt.wantKind)
			}
		})
	}
}

// --- ResolveSource: full precedence with kiro-ide (table-driven) ---
// Covers explicit-beats-autodetected, kiro-cli precedence, broken-source fallback, and invalid-explicit fallback.

func TestResolveSource_FullPrecedenceWithKiroIDE(t *testing.T) {
	dir := t.TempDir()
	sqlitePath := createValidSQLiteFile(t, dir, "cli.sqlite3")
	credsPath := createValidJSONCredsFile(t, dir, "creds.json")

	// Home with both autodetected stores.
	dualHome := t.TempDir()
	createTempKiroCLIStore(t, dualHome)
	createTempKiroIDEStore(t, dualHome)

	// Home with only kiro-ide.
	ideOnlyHome := t.TempDir()
	createTempKiroIDEStore(t, ideOnlyHome)

	// Home with broken kiro-cli (directory) and valid kiro-ide.
	brokenCLIHome := t.TempDir()
	kiroCLIDir := defaultKiroCLIDBPath(brokenCLIHome)
	if err := os.MkdirAll(kiroCLIDir, 0o755); err != nil {
		t.Fatal(err)
	}
	createTempKiroIDEStore(t, brokenCLIHome)

	tests := []struct {
		name     string
		input    ResolveInput
		wantKind SourceKind
	}{
		{
			name: "explicit-sqlite-beats-both-autodetected",
			input: ResolveInput{
				KiroCLIDBFile: sqlitePath,
				HomeDir:       dualHome,
			},
			wantKind: SourceEnvSQLite,
		},
		{
			name: "explicit-creds-file-beats-both-autodetected",
			input: ResolveInput{
				KiroCredsFile: credsPath,
				HomeDir:       dualHome,
			},
			wantKind: SourceEnvCredsFile,
		},
		{
			name: "explicit-token-beats-both-autodetected",
			input: ResolveInput{
				RefreshToken: "tok",
				HomeDir:      dualHome,
			},
			wantKind: SourceEnvRefreshToken,
		},
		{
			name:     "dual-autodetect-kiro-cli-wins",
			input:    ResolveInput{HomeDir: dualHome},
			wantKind: SourceKiroCLI,
		},
		{
			name:     "ide-only-home-selects-kiro-ide",
			input:    ResolveInput{HomeDir: ideOnlyHome},
			wantKind: SourceKiroIDE,
		},
		{
			name:     "broken-cli-falls-to-kiro-ide",
			input:    ResolveInput{HomeDir: brokenCLIHome},
			wantKind: SourceKiroIDE,
		},
		{
			name: "invalid-explicit-falls-to-autodetected-kiro-ide",
			input: ResolveInput{
				KiroCLIDBFile: "/nonexistent/bad.sqlite3",
				KiroCredsFile: "/nonexistent/bad.json",
				HomeDir:       ideOnlyHome,
			},
			wantKind: SourceKiroIDE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := ResolveSource(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if src.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", src.Kind, tt.wantKind)
			}
		})
	}
}

// --- BuildAuthOptions tests ---

func TestBuildAuthOptions_EnvSQLite(t *testing.T) {
	src := &ResolvedSource{
		Kind:     SourceEnvSQLite,
		Path:     "/data/cli.sqlite3",
		Writable: true,
	}

	opts := src.BuildAuthOptions("tok", "arn:test", "us-east-1", "socks5://proxy")

	if opts.SQLiteDB != "/data/cli.sqlite3" {
		t.Errorf("SQLiteDB = %q, want %q", opts.SQLiteDB, "/data/cli.sqlite3")
	}
	if opts.CredsFile != "" {
		t.Errorf("CredsFile = %q, want empty", opts.CredsFile)
	}
	if opts.RefreshToken != "tok" {
		t.Errorf("RefreshToken = %q, want %q", opts.RefreshToken, "tok")
	}
	if opts.ProfileARN != "arn:test" {
		t.Errorf("ProfileARN = %q, want %q", opts.ProfileARN, "arn:test")
	}
	if opts.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", opts.Region, "us-east-1")
	}
	if opts.VPNProxyURL != "socks5://proxy" {
		t.Errorf("VPNProxyURL = %q, want %q", opts.VPNProxyURL, "socks5://proxy")
	}
}

func TestBuildAuthOptions_EnvCredsFile(t *testing.T) {
	src := &ResolvedSource{
		Kind:     SourceEnvCredsFile,
		Path:     "/data/creds.json",
		Writable: true,
	}

	opts := src.BuildAuthOptions("tok", "arn:test", "us-east-1", "")

	if opts.CredsFile != "/data/creds.json" {
		t.Errorf("CredsFile = %q, want %q", opts.CredsFile, "/data/creds.json")
	}
	if opts.SQLiteDB != "" {
		t.Errorf("SQLiteDB = %q, want empty", opts.SQLiteDB)
	}
}

func TestBuildAuthOptions_EnvRefreshToken(t *testing.T) {
	src := &ResolvedSource{
		Kind:     SourceEnvRefreshToken,
		Path:     "",
		Writable: false,
	}

	opts := src.BuildAuthOptions("tok-secret", "arn:test", "us-east-1", "")

	if opts.RefreshToken != "tok-secret" {
		t.Errorf("RefreshToken = %q, want %q", opts.RefreshToken, "tok-secret")
	}
	if opts.SQLiteDB != "" {
		t.Errorf("SQLiteDB = %q, want empty", opts.SQLiteDB)
	}
	if opts.CredsFile != "" {
		t.Errorf("CredsFile = %q, want empty", opts.CredsFile)
	}
}

func TestBuildAuthOptions_KiroCLI(t *testing.T) {
	src := &ResolvedSource{
		Kind:     SourceKiroCLI,
		Path:     "/home/user/.local/share/kiro-cli/data.sqlite3",
		Writable: true,
	}

	opts := src.BuildAuthOptions("", "", "us-east-1", "")

	if opts.SQLiteDB != "/home/user/.local/share/kiro-cli/data.sqlite3" {
		t.Errorf("SQLiteDB = %q", opts.SQLiteDB)
	}
	if opts.CredsFile != "" {
		t.Errorf("CredsFile = %q, want empty", opts.CredsFile)
	}
}

func TestBuildAuthOptions_KiroIDE(t *testing.T) {
	src := &ResolvedSource{
		Kind:     SourceKiroIDE,
		Path:     "/home/user/.kiro/credentials.json",
		Writable: true,
	}

	opts := src.BuildAuthOptions("", "", "us-east-1", "")

	if opts.CredsFile != "/home/user/.kiro/credentials.json" {
		t.Errorf("CredsFile = %q", opts.CredsFile)
	}
	if opts.SQLiteDB != "" {
		t.Errorf("SQLiteDB = %q, want empty", opts.SQLiteDB)
	}
}

// --- log capture helper ---

// captureLogs runs fn while capturing slog output at Debug level. Returns
// the captured log text. Restores the previous default logger on cleanup.
func captureLogs(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	fn()
	return buf.String()
}

// --- Startup logs the selected source and resolved path ---

func TestResolveSource_LogsWinningSourceAndPath(t *testing.T) {
	tests := []struct {
		name       string
		setupHome  func(t *testing.T) string
		input      func(homeDir string) ResolveInput
		wantSource string
		wantPath   bool // true if a file-backed source should log a path
	}{
		{
			name: "env-sqlite",
			setupHome: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			input: func(_ string) ResolveInput {
				dir := t.TempDir()
				path := createValidSQLiteFile(t, dir, "db.sqlite3")
				return ResolveInput{KiroCLIDBFile: path, HomeDir: t.TempDir()}
			},
			wantSource: "env-sqlite",
			wantPath:   true,
		},
		{
			name: "env-creds-file",
			setupHome: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			input: func(_ string) ResolveInput {
				dir := t.TempDir()
				path := createValidJSONCredsFile(t, dir, "creds.json")
				return ResolveInput{KiroCredsFile: path, HomeDir: t.TempDir()}
			},
			wantSource: "env-creds-file",
			wantPath:   true,
		},
		{
			name: "env-refresh-token",
			setupHome: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			input: func(_ string) ResolveInput {
				return ResolveInput{RefreshToken: "tok-123", HomeDir: t.TempDir()}
			},
			wantSource: "env-refresh-token",
			wantPath:   false,
		},
		{
			name: "autodetected-kiro-cli",
			setupHome: func(t *testing.T) string {
				t.Helper()
				homeDir := t.TempDir()
				createTempKiroCLIStore(t, homeDir)
				return homeDir
			},
			input: func(homeDir string) ResolveInput {
				return ResolveInput{HomeDir: homeDir}
			},
			wantSource: "kiro-cli",
			wantPath:   true,
		},
		{
			name: "autodetected-kiro-ide",
			setupHome: func(t *testing.T) string {
				t.Helper()
				homeDir := t.TempDir()
				createTempKiroIDEStore(t, homeDir)
				return homeDir
			},
			input: func(homeDir string) ResolveInput {
				return ResolveInput{HomeDir: homeDir}
			},
			wantSource: "kiro-ide",
			wantPath:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := tt.setupHome(t)
			in := tt.input(homeDir)

			logs := captureLogs(t, func() {
				src, err := ResolveSource(in)
				if err != nil {
					t.Fatal(err)
				}
				// Verify the source kind string appears somewhere in logs.
				if src.Kind != SourceKind(tt.wantSource) {
					t.Errorf("Kind = %q, want %q", src.Kind, tt.wantSource)
				}
			})

			// The resolution layer logs kind for every candidate/source.
			if !strings.Contains(logs, tt.wantSource) {
				t.Errorf("logs should contain source kind %q, got:\n%s", tt.wantSource, logs)
			}
		})
	}
}

// --- Startup logs never expose credential secrets ---

func TestResolveSource_LogsAreSecretSafe(t *testing.T) {
	secrets := []string{
		"super-secret-refresh-token",
		"access-token-value",
		"client-secret-value",
	}

	tests := []struct {
		name  string
		input ResolveInput
	}{
		{
			name: "env-refresh-token",
			input: ResolveInput{
				RefreshToken: "super-secret-refresh-token",
				HomeDir:      t.TempDir(),
			},
		},
		{
			name: "env-sqlite-with-token",
			input: func() ResolveInput {
				dir := t.TempDir()
				path := createValidSQLiteFile(t, dir, "db.sqlite3")
				return ResolveInput{
					KiroCLIDBFile: path,
					RefreshToken:  "super-secret-refresh-token",
					HomeDir:       t.TempDir(),
				}
			}(),
		},
		{
			name: "autodetected-kiro-ide",
			input: func() ResolveInput {
				homeDir := t.TempDir()
				createTempKiroIDEStoreWithContent(t, homeDir, `{
					"refreshToken": "super-secret-refresh-token",
					"accessToken": "access-token-value",
					"clientSecret": "client-secret-value"
				}`)
				return ResolveInput{HomeDir: homeDir}
			}(),
		},
		{
			name: "invalid-explicit-falls-through",
			input: func() ResolveInput {
				homeDir := t.TempDir()
				createTempKiroIDEStore(t, homeDir)
				return ResolveInput{
					KiroCLIDBFile: "/nonexistent/bad.sqlite3",
					RefreshToken:  "super-secret-refresh-token",
					HomeDir:       homeDir,
				}
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t, func() {
				_, _ = ResolveSource(tt.input)
			})

			for _, secret := range secrets {
				if strings.Contains(logs, secret) {
					t.Errorf("logs contain secret %q:\n%s", secret, logs)
				}
			}
		})
	}
}

// --- Rejection logging for broken autodetected sources ---

func TestResolveSource_BrokenAutodetectedLogsRejection(t *testing.T) {
	homeDir := t.TempDir()

	// Create a directory at the kiro-cli path (broken).
	kiroCLIPath := defaultKiroCLIDBPath(homeDir)
	if err := os.MkdirAll(kiroCLIPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a valid kiro-ide store.
	createTempKiroIDEStore(t, homeDir)

	logs := captureLogs(t, func() {
		src, err := ResolveSource(ResolveInput{HomeDir: homeDir})
		if err != nil {
			t.Fatal(err)
		}
		if src.Kind != SourceKiroIDE {
			t.Errorf("expected kiro-ide fallback, got %q", src.Kind)
		}
	})

	// Verify rejection log contains the kind and reason.
	if !strings.Contains(logs, "kiro-cli") {
		t.Errorf("rejection log should mention kiro-cli, got:\n%s", logs)
	}
	if !strings.Contains(logs, "rejected") {
		t.Errorf("rejection log should contain 'rejected', got:\n%s", logs)
	}
	if !strings.Contains(logs, "directory") {
		t.Errorf("rejection log should explain the reason (directory), got:\n%s", logs)
	}
}

// --- Loadability validation: unit tests ---

func TestValidateSQLiteLoadable_ValidDB(t *testing.T) {
	tokenData, _ := json.Marshal(map[string]string{
		"access_token": "test-access", "refresh_token": "test-refresh",
	})
	dbPath := createTestDB(t, map[string]string{
		"kirocli:social:token": string(tokenData),
	})

	if err := validateSQLiteLoadable(dbPath); err != nil {
		t.Errorf("expected valid DB to pass, got: %v", err)
	}
}

func TestValidateSQLiteLoadable_NotADatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.sqlite3")
	if err := os.WriteFile(path, []byte("not-a-database"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateSQLiteLoadable(path); err == nil {
		t.Error("expected error for non-database file")
	}
}

func TestValidateSQLiteLoadable_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.sqlite3")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateSQLiteLoadable(path); err == nil {
		t.Error("expected error for empty/non-sqlite file")
	}
}

func TestValidateSQLiteLoadable_NoAuthKVTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "no-table.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Create the DB with a different table, no auth_kv.
	if _, err := db.Exec("CREATE TABLE other (key TEXT)"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	_ = db.Close()

	if err := validateSQLiteLoadable(dbPath); err == nil {
		t.Error("expected error for DB without auth_kv table")
	}
}

func TestValidateSQLiteLoadable_NoUsableTokenKeys(t *testing.T) {
	dbPath := createTestDB(t, map[string]string{
		"some:unrelated:key": `{"foo":"bar"}`,
	})

	if err := validateSQLiteLoadable(dbPath); err == nil {
		t.Error("expected error for DB with no usable token keys")
	}
}

func TestValidateSQLiteLoadable_TokenKeyNoRefreshToken(t *testing.T) {
	tokenData, _ := json.Marshal(map[string]string{
		"access_token": "only-access",
	})
	dbPath := createTestDB(t, map[string]string{
		"kirocli:social:token": string(tokenData),
	})

	if err := validateSQLiteLoadable(dbPath); err == nil {
		t.Error("expected error for token key without refresh_token")
	}
}

func TestValidateSQLiteLoadable_MalformedTokenJSON(t *testing.T) {
	dbPath := createTestDB(t, map[string]string{
		"kirocli:social:token": "not-json-at-all",
	})

	if err := validateSQLiteLoadable(dbPath); err == nil {
		t.Error("expected error for malformed token JSON in DB")
	}
}

func TestValidateJSONCredsLoadable_ValidFile(t *testing.T) {
	path := createTestCredsFile(t, map[string]interface{}{
		"refreshToken": "test-refresh",
	})

	if err := validateJSONCredsLoadable(path); err != nil {
		t.Errorf("expected valid file to pass, got: %v", err)
	}
}

func TestValidateJSONCredsLoadable_InvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateJSONCredsLoadable(path); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestValidateJSONCredsLoadable_MissingRefreshToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-refresh.json")
	if err := os.WriteFile(path, []byte(`{"accessToken":"test"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateJSONCredsLoadable(path); err == nil {
		t.Error("expected error for missing refreshToken")
	}
}

func TestValidateJSONCredsLoadable_EmptyJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateJSONCredsLoadable(path); err == nil {
		t.Error("expected error for empty JSON object")
	}
}

func TestValidateJSONCredsLoadable_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := validateJSONCredsLoadable(path); err == nil {
		t.Error("expected error for empty file")
	}
}

// --- Loadability validation: resolver integration tests ---

// Malformed explicit SQLite (file exists but not a valid DB) falls back.
func TestResolveSource_MalformedExplicitSQLite_FallsToNextSource(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file (not a valid SQLite DB).
	badSQLite := createTempFile(t, dir, "bad.sqlite3")
	homeDir := t.TempDir()
	kiroIDEPath := createTempKiroIDEStore(t, homeDir)

	in := ResolveInput{
		KiroCLIDBFile: badSQLite,
		HomeDir:       homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatalf("expected fallback to kiro-ide, got error: %v", err)
	}

	if src.Kind != SourceKiroIDE {
		t.Errorf("Kind = %q, want %q (malformed SQLite should be rejected)", src.Kind, SourceKiroIDE)
	}
	if src.Path != kiroIDEPath {
		t.Errorf("Path = %q, want %q", src.Path, kiroIDEPath)
	}
}

// Malformed explicit JSON (file exists but no refreshToken) falls back.
func TestResolveSource_MalformedExplicitJSON_FallsToNextSource(t *testing.T) {
	dir := t.TempDir()
	// Create a JSON file without refreshToken.
	badJSON := createTempFile(t, dir, "bad.json")
	homeDir := t.TempDir()
	kiroIDEPath := createTempKiroIDEStore(t, homeDir)

	in := ResolveInput{
		KiroCredsFile: badJSON,
		HomeDir:       homeDir,
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatalf("expected fallback to kiro-ide, got error: %v", err)
	}

	if src.Kind != SourceKiroIDE {
		t.Errorf("Kind = %q, want %q (malformed JSON should be rejected)", src.Kind, SourceKiroIDE)
	}
	if src.Path != kiroIDEPath {
		t.Errorf("Path = %q, want %q", src.Path, kiroIDEPath)
	}
}

// Malformed explicit SQLite falls to valid explicit JSON creds.
func TestResolveSource_MalformedSQLite_FallsToValidCredsFile(t *testing.T) {
	dir := t.TempDir()
	badSQLite := createTempFile(t, dir, "bad.sqlite3")
	validCreds := createValidJSONCredsFile(t, dir, "creds.json")

	in := ResolveInput{
		KiroCLIDBFile: badSQLite,
		KiroCredsFile: validCreds,
		HomeDir:       t.TempDir(),
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvCredsFile {
		t.Errorf("Kind = %q, want %q (malformed SQLite should fall to creds file)", src.Kind, SourceEnvCredsFile)
	}
	if src.Path != validCreds {
		t.Errorf("Path = %q, want %q", src.Path, validCreds)
	}
}

// Both malformed explicit sources fall to refresh token.
func TestResolveSource_BothMalformedExplicit_FallsToRefreshToken(t *testing.T) {
	dir := t.TempDir()
	badSQLite := createTempFile(t, dir, "bad.sqlite3")
	badJSON := createTempFile(t, dir, "bad.json")

	in := ResolveInput{
		KiroCLIDBFile: badSQLite,
		KiroCredsFile: badJSON,
		RefreshToken:  "tok-fallback",
		HomeDir:       t.TempDir(),
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvRefreshToken {
		t.Errorf("Kind = %q, want %q", src.Kind, SourceEnvRefreshToken)
	}
}

// Autodetected malformed kiro-cli (file exists but not a valid SQLite DB) falls back to kiro-ide.
func TestResolveSource_MalformedAutodetectedSQLite_FallsToKiroIDE(t *testing.T) {
	homeDir := t.TempDir()

	// Create an invalid file (not a SQLite DB) at the kiro-cli location.
	kiroCLIPath := defaultKiroCLIDBPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(kiroCLIPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kiroCLIPath, []byte("not-a-sqlite-db"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a valid kiro-ide store.
	kiroIDEPath := createTempKiroIDEStore(t, homeDir)

	in := ResolveInput{HomeDir: homeDir}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatalf("expected fallback to kiro-ide, got error: %v", err)
	}

	if src.Kind != SourceKiroIDE {
		t.Errorf("Kind = %q, want %q (malformed kiro-cli should fall back)", src.Kind, SourceKiroIDE)
	}
	if src.Path != kiroIDEPath {
		t.Errorf("Path = %q, want %q", src.Path, kiroIDEPath)
	}
}

// Autodetected malformed kiro-ide (JSON without refreshToken) fails closed.
func TestResolveSource_MalformedAutodetectedJSON_FailClosed(t *testing.T) {
	homeDir := t.TempDir()

	// Create a JSON file without refreshToken at kiro-ide location.
	kiroIDEPath := defaultKiroIDECredsPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(kiroIDEPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kiroIDEPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	in := ResolveInput{HomeDir: homeDir}

	src, err := ResolveSource(in)
	if err == nil {
		t.Fatal("expected error when autodetected JSON has no refreshToken")
	}
	if src != nil {
		t.Errorf("expected nil source, got %+v", src)
	}
}

// All sources are malformed → fail closed.
func TestResolveSource_AllMalformed_FailClosed(t *testing.T) {
	dir := t.TempDir()
	homeDir := t.TempDir()

	// Malformed explicit sources.
	badSQLite := createTempFile(t, dir, "bad.sqlite3")
	badJSON := createTempFile(t, dir, "bad.json")

	// Malformed autodetected kiro-cli (not a valid SQLite DB).
	kiroCLIPath := defaultKiroCLIDBPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(kiroCLIPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kiroCLIPath, []byte("not-sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Malformed autodetected kiro-ide (no refreshToken).
	kiroIDEPath := defaultKiroIDECredsPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(kiroIDEPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kiroIDEPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	in := ResolveInput{
		KiroCLIDBFile: badSQLite,
		KiroCredsFile: badJSON,
		HomeDir:       homeDir,
	}

	src, err := ResolveSource(in)
	if err == nil {
		t.Fatal("expected error when all sources are malformed")
	}
	if src != nil {
		t.Errorf("expected nil source, got %+v", src)
	}
}

// Malformed autodetected kiro-cli logs a rejection with the reason.
func TestResolveSource_MalformedAutodetectedSQLite_LogsRejection(t *testing.T) {
	homeDir := t.TempDir()

	// Create an invalid file at the kiro-cli location.
	kiroCLIPath := defaultKiroCLIDBPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(kiroCLIPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kiroCLIPath, []byte("not-sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a valid kiro-ide store for fallback.
	createTempKiroIDEStore(t, homeDir)

	logs := captureLogs(t, func() {
		src, err := ResolveSource(ResolveInput{HomeDir: homeDir})
		if err != nil {
			t.Fatal(err)
		}
		if src.Kind != SourceKiroIDE {
			t.Errorf("expected kiro-ide fallback, got %q", src.Kind)
		}
	})

	if !strings.Contains(logs, "kiro-cli") {
		t.Errorf("rejection log should mention kiro-cli, got:\n%s", logs)
	}
	if !strings.Contains(logs, "rejected") {
		t.Errorf("rejection log should contain 'rejected', got:\n%s", logs)
	}
}

// Malformed explicit JSON logs a rejection with the reason.
func TestResolveSource_MalformedExplicitJSON_LogsRejection(t *testing.T) {
	dir := t.TempDir()
	badJSON := createTempFile(t, dir, "bad.json")

	logs := captureLogs(t, func() {
		_, _ = ResolveSource(ResolveInput{
			KiroCredsFile: badJSON,
			RefreshToken:  "tok-fallback",
			HomeDir:       t.TempDir(),
		})
	})

	if !strings.Contains(logs, "env-creds-file") {
		t.Errorf("rejection log should mention env-creds-file, got:\n%s", logs)
	}
	if !strings.Contains(logs, "rejected") {
		t.Errorf("rejection log should contain 'rejected', got:\n%s", logs)
	}
	if !strings.Contains(logs, "refreshToken") {
		t.Errorf("rejection log should explain the reason (missing refreshToken), got:\n%s", logs)
	}
}

// --- SourceKind constants test ---

func TestSourceKind_StringValues(t *testing.T) {
	tests := []struct {
		kind SourceKind
		want string
	}{
		{SourceEnvRefreshToken, "env-refresh-token"},
		{SourceEnvCredsFile, "env-creds-file"},
		{SourceEnvSQLite, "env-sqlite"},
		{SourceKiroCLI, "kiro-cli"},
		{SourceKiroIDE, "kiro-ide"},
	}

	for _, tt := range tests {
		if string(tt.kind) != tt.want {
			t.Errorf("SourceKind %q: string = %q, want %q", tt.kind, string(tt.kind), tt.want)
		}
	}
}

// --- probeFile tests ---

func TestProbeFile_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := createTempFile(t, dir, "test.txt")

	if err := probeFile(path); err != nil {
		t.Errorf("expected nil error for valid file, got: %v", err)
	}
}

func TestProbeFile_NonexistentFile(t *testing.T) {
	if err := probeFile("/nonexistent/path/file.txt"); err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestProbeFile_Directory(t *testing.T) {
	dir := t.TempDir()

	err := probeFile(dir)
	if err == nil {
		t.Error("expected error for directory")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("error should mention directory, got: %s", err.Error())
	}
}

// --- defaultKiroCLIDBPath tests ---

func TestDefaultKiroCLIDBPath(t *testing.T) {
	home := "/fakehome"
	path := defaultKiroCLIDBPath(home)

	if !filepath.IsAbs(path) {
		t.Errorf("path is not absolute: %q", path)
	}
	if !strings.HasPrefix(path, home) {
		t.Errorf("path does not start with home dir: %q", path)
	}
	if !strings.HasSuffix(path, "data.sqlite3") {
		t.Errorf("path does not end with data.sqlite3: %q", path)
	}
	if !strings.Contains(path, "kiro-cli") {
		t.Errorf("path does not contain kiro-cli: %q", path)
	}
}

// --- defaultKiroIDECredsPath tests ---

func TestDefaultKiroIDECredsPath(t *testing.T) {
	home := "/fakehome"
	path := defaultKiroIDECredsPath(home)

	if !filepath.IsAbs(path) {
		t.Errorf("path is not absolute: %q", path)
	}
	if !strings.HasPrefix(path, home) {
		t.Errorf("path does not start with home dir: %q", path)
	}
	if !strings.HasSuffix(path, "credentials.json") {
		t.Errorf("path does not end with credentials.json: %q", path)
	}
	if !strings.Contains(path, ".kiro") {
		t.Errorf("path does not contain .kiro: %q", path)
	}
}

// --- fileExists tests ---

func TestFileExists_RegularFile(t *testing.T) {
	dir := t.TempDir()
	path := createTempFile(t, dir, "test.txt")
	if !fileExists(path) {
		t.Errorf("expected true for existing regular file")
	}
}

func TestFileExists_Directory(t *testing.T) {
	dir := t.TempDir()
	if !fileExists(dir) {
		t.Errorf("expected true for existing directory")
	}
}

func TestFileExists_Nonexistent(t *testing.T) {
	if fileExists("/nonexistent/path/file.txt") {
		t.Errorf("expected false for nonexistent path")
	}
}
