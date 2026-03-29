package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

// SourceKind identifies how a credential source was discovered.
type SourceKind string

//nolint:gosec // G101: these are source-kind labels, not hardcoded credentials.
const (
	// SourceEnvRefreshToken is a bare refresh token from REFRESH_TOKEN.
	SourceEnvRefreshToken SourceKind = "env-refresh-token"
	// SourceEnvCredsFile is a JSON credentials file from KIRO_CREDS_FILE.
	SourceEnvCredsFile SourceKind = "env-creds-file"
	// SourceEnvSQLite is a SQLite database from KIRO_CLI_DB_FILE.
	SourceEnvSQLite SourceKind = "env-sqlite"
	// SourceKiroCLI is an autodetected kiro-cli SQLite store.
	SourceKiroCLI SourceKind = "kiro-cli"
	// SourceKiroIDE is an autodetected kiro-ide credentials file.
	SourceKiroIDE SourceKind = "kiro-ide"
)

// ResolvedSource captures the winning credential source after resolution.
// Handlers and protocol formatters should not inspect this type; it exists
// so startup can resolve credentials once and hand a single source to the
// auth manager.
type ResolvedSource struct {
	Kind     SourceKind // how the source was discovered
	Path     string     // resolved filesystem path; empty for bare token sources
	Writable bool       // whether refreshed credentials can be persisted back
}

// ResolveInput provides the candidate credential sources for evaluation.
type ResolveInput struct {
	KiroCLIDBFile string // explicit SQLite path (KIRO_CLI_DB_FILE)
	KiroCredsFile string // explicit creds file path (KIRO_CREDS_FILE)
	RefreshToken  string //nolint:gosec // G117: field name, not a credential value
	HomeDir       string // override for autodetection; defaults to os.UserHomeDir()
}

// ResolveSource evaluates credential candidates in precedence order and returns
// the highest-priority usable source. Explicit env-backed sources are evaluated
// first in backward-compatible order: KIRO_CLI_DB_FILE > KIRO_CREDS_FILE >
// REFRESH_TOKEN. File-backed explicit sources are validated for existence; an
// invalid explicit source is logged and skipped so that lower-priority
// candidates (including autodetection) can still resolve.
//
// When no explicit source resolves, autodetection probes platform-default
// locations for kiro-cli. If nothing resolves the function returns an error
// so startup can fail closed before binding the listen port.
func ResolveSource(in ResolveInput) (*ResolvedSource, error) {
	if src := resolveExplicitSources(in); src != nil {
		return src, nil
	}

	if src := resolveAutodetectedSources(in); src != nil {
		return src, nil
	}

	return nil, errors.New(
		"no credential source resolved: set KIRO_CLI_DB_FILE, KIRO_CREDS_FILE, or REFRESH_TOKEN, or install kiro-cli",
	)
}

// resolveExplicitSources evaluates env-backed credential sources in
// backward-compatible precedence. File-backed sources are validated; invalid
// ones are logged and skipped.
func resolveExplicitSources(in ResolveInput) *ResolvedSource {
	if in.KiroCLIDBFile != "" {
		path := expandPath(in.KiroCLIDBFile)
		if err := probeFile(path); err != nil {
			slog.Warn("explicit credential source rejected",
				"kind", string(SourceEnvSQLite),
				"path", path,
				"reason", err.Error(),
			)
		} else {
			slog.Debug("credential source candidate",
				"kind", string(SourceEnvSQLite),
				"path", path,
			)
			return &ResolvedSource{Kind: SourceEnvSQLite, Path: path, Writable: true}
		}
	}

	if in.KiroCredsFile != "" {
		path := expandPath(in.KiroCredsFile)
		if err := probeFile(path); err != nil {
			slog.Warn("explicit credential source rejected",
				"kind", string(SourceEnvCredsFile),
				"path", path,
				"reason", err.Error(),
			)
		} else {
			slog.Debug("credential source candidate",
				"kind", string(SourceEnvCredsFile),
				"path", path,
			)
			return &ResolvedSource{Kind: SourceEnvCredsFile, Path: path, Writable: true}
		}
	}

	if in.RefreshToken != "" {
		slog.Debug("credential source candidate",
			"kind", string(SourceEnvRefreshToken),
		)
		return &ResolvedSource{Kind: SourceEnvRefreshToken, Path: "", Writable: false}
	}

	return nil
}

// resolveAutodetectedSources probes platform-default credential store
// locations. Currently supports kiro-cli; kiro-ide will be added by a
// subsequent feature.
func resolveAutodetectedSources(in ResolveInput) *ResolvedSource {
	homeDir := in.HomeDir
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			slog.Debug("cannot determine home directory for autodetection", "error", err.Error())
			return nil
		}
	}

	kiroCLIPath := defaultKiroCLIDBPath(homeDir)
	if err := probeFile(kiroCLIPath); err == nil {
		slog.Debug("autodetected credential source",
			"kind", string(SourceKiroCLI),
			"path", kiroCLIPath,
		)
		return &ResolvedSource{Kind: SourceKiroCLI, Path: kiroCLIPath, Writable: true}
	}

	return nil
}

// defaultKiroCLIDBPath returns the platform-default kiro-cli SQLite path for
// the given home directory.
func defaultKiroCLIDBPath(homeDir string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(homeDir, "Library", "Application Support", "kiro-cli", "data.sqlite3")
	}
	return filepath.Join(homeDir, ".local", "share", "kiro-cli", "data.sqlite3")
}

// probeFile checks whether a path exists and refers to a regular file (not a
// directory). It does not read content; deeper validation is the auth
// manager's responsibility.
func probeFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a regular file")
	}
	return nil
}

// BuildAuthOptions constructs Options for NewKiroAuthManager from this resolved
// source. The refreshToken parameter is passed through for backward
// compatibility with sources that may carry a supplementary token; it is not
// stored in the ResolvedSource itself to keep secrets out of resolution
// metadata.
func (s *ResolvedSource) BuildAuthOptions(refreshToken, profileARN, region, vpnProxyURL string) Options {
	opts := Options{
		RefreshToken: refreshToken,
		ProfileARN:   profileARN,
		Region:       region,
		VPNProxyURL:  vpnProxyURL,
	}

	switch s.Kind {
	case SourceEnvSQLite, SourceKiroCLI:
		opts.SQLiteDB = s.Path
	case SourceEnvCredsFile, SourceKiroIDE:
		opts.CredsFile = s.Path
	}

	return opts
}
