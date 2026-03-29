package auth

import (
	"path/filepath"
	"testing"
)

// --- ResolveSource tests ---

func TestResolveSource_Precedence_SQLiteWins(t *testing.T) {
	in := ResolveInput{
		KiroCLIDBFile: "/data/cli.sqlite3",
		KiroCredsFile: "/data/creds.json",
		RefreshToken:  "tok-123",
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvSQLite {
		t.Errorf("Kind = %q, want %q", src.Kind, SourceEnvSQLite)
	}
	if src.Path != "/data/cli.sqlite3" {
		t.Errorf("Path = %q, want %q", src.Path, "/data/cli.sqlite3")
	}
	if !src.Writable {
		t.Error("Writable = false, want true")
	}
}

func TestResolveSource_Precedence_CredsFileOverToken(t *testing.T) {
	in := ResolveInput{
		KiroCredsFile: "/data/creds.json",
		RefreshToken:  "tok-123",
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Kind != SourceEnvCredsFile {
		t.Errorf("Kind = %q, want %q", src.Kind, SourceEnvCredsFile)
	}
	if src.Path != "/data/creds.json" {
		t.Errorf("Path = %q, want %q", src.Path, "/data/creds.json")
	}
	if !src.Writable {
		t.Error("Writable = false, want true")
	}
}

func TestResolveSource_RefreshTokenOnly(t *testing.T) {
	in := ResolveInput{
		RefreshToken: "tok-123",
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

func TestResolveSource_NoSource_Error(t *testing.T) {
	in := ResolveInput{}

	src, err := ResolveSource(in)
	if err == nil {
		t.Fatal("expected error when no source is configured")
	}
	if src != nil {
		t.Errorf("expected nil source, got %+v", src)
	}
}

func TestResolveSource_PathExpansion(t *testing.T) {
	in := ResolveInput{
		KiroCLIDBFile: "~/data/cli.sqlite3",
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	// expandPath should have resolved ~/ to an absolute path.
	if src.Path == "~/data/cli.sqlite3" {
		t.Error("path was not expanded")
	}
	if !filepath.IsAbs(src.Path) {
		t.Errorf("expanded path is not absolute: %q", src.Path)
	}
}

func TestResolveSource_AbsolutePathUnchanged(t *testing.T) {
	in := ResolveInput{
		KiroCredsFile: "/absolute/creds.json",
	}

	src, err := ResolveSource(in)
	if err != nil {
		t.Fatal(err)
	}

	if src.Path != "/absolute/creds.json" {
		t.Errorf("Path = %q, want %q", src.Path, "/absolute/creds.json")
	}
}

// --- ResolvedSource metadata table test ---

func TestResolvedSource_Metadata(t *testing.T) {
	tests := []struct {
		name     string
		input    ResolveInput
		kind     SourceKind
		writable bool
		hasPath  bool
	}{
		{
			name:     "env-sqlite",
			input:    ResolveInput{KiroCLIDBFile: "/db.sqlite3"},
			kind:     SourceEnvSQLite,
			writable: true,
			hasPath:  true,
		},
		{
			name:     "env-creds-file",
			input:    ResolveInput{KiroCredsFile: "/creds.json"},
			kind:     SourceEnvCredsFile,
			writable: true,
			hasPath:  true,
		},
		{
			name:     "env-refresh-token",
			input:    ResolveInput{RefreshToken: "tok"},
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
