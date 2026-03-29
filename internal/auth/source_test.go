package auth

import (
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

// createTempKiroCLIStore creates the platform-default kiro-cli SQLite file
// under the given home directory. Returns the full path.
func createTempKiroCLIStore(t *testing.T, homeDir string) string {
	t.Helper()
	return createTempFile(t, homeDir, defaultKiroCLIDBRelPath())
}

// defaultKiroCLIDBRelPath returns the relative path to the kiro-cli store
// for the current platform, useful for constructing temp fixtures.
func defaultKiroCLIDBRelPath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join("Library", "Application Support", "kiro-cli", "data.sqlite3")
	}
	return filepath.Join(".local", "share", "kiro-cli", "data.sqlite3")
}

// --- ResolveSource: explicit-source precedence tests ---

func TestResolveSource_Precedence_SQLiteWins(t *testing.T) {
	dir := t.TempDir()
	sqlitePath := createTempFile(t, dir, "cli.sqlite3")
	credsPath := createTempFile(t, dir, "creds.json")

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
	credsPath := createTempFile(t, dir, "creds.json")

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

// --- ResolveSource: explicit vs autodetected precedence ---

func TestResolveSource_ExplicitBeatsAutodetected(t *testing.T) {
	homeDir := t.TempDir()
	createTempKiroCLIStore(t, homeDir) // valid autodetected kiro-cli

	dir := t.TempDir()
	sqlitePath := createTempFile(t, dir, "explicit.sqlite3")

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

func TestResolveSource_RefreshTokenBeatsAutodetected(t *testing.T) {
	homeDir := t.TempDir()
	createTempKiroCLIStore(t, homeDir) // valid autodetected kiro-cli

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
	credsPath := createTempFile(t, dir, "creds.json")

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
	credsPath := createTempFile(t, dir, "creds.json")

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
	if err := os.WriteFile(dbPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

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
	sqlitePath := createTempFile(t, dir, "db.sqlite3")
	credsPath := createTempFile(t, dir, "creds.json")

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
// VAL-STARTUP-011: KIRO_CLI_DB_FILE > KIRO_CREDS_FILE > REFRESH_TOKEN

func TestResolveSource_BackwardCompatiblePrecedence(t *testing.T) {
	dir := t.TempDir()
	sqlitePath := createTempFile(t, dir, "cli.sqlite3")
	credsPath := createTempFile(t, dir, "creds.json")

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
