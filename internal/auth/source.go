package auth

import (
	"errors"
	"log/slog"
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
}

// ResolveSource evaluates credential candidates in precedence order and returns
// the highest-priority source. Explicit env-backed sources are evaluated first
// in backward-compatible order: KIRO_CLI_DB_FILE > KIRO_CREDS_FILE >
// REFRESH_TOKEN.
//
// Future features will extend this to probe autodetected sources when no
// explicit source is configured.
func ResolveSource(in ResolveInput) (*ResolvedSource, error) {
	// Explicit env-backed sources in backward-compatible precedence.
	if in.KiroCLIDBFile != "" {
		path := expandPath(in.KiroCLIDBFile)
		slog.Debug("credential source candidate",
			"kind", string(SourceEnvSQLite),
			"path", path,
		)
		return &ResolvedSource{
			Kind:     SourceEnvSQLite,
			Path:     path,
			Writable: true,
		}, nil
	}

	if in.KiroCredsFile != "" {
		path := expandPath(in.KiroCredsFile)
		slog.Debug("credential source candidate",
			"kind", string(SourceEnvCredsFile),
			"path", path,
		)
		return &ResolvedSource{
			Kind:     SourceEnvCredsFile,
			Path:     path,
			Writable: true,
		}, nil
	}

	if in.RefreshToken != "" {
		slog.Debug("credential source candidate",
			"kind", string(SourceEnvRefreshToken),
		)
		return &ResolvedSource{
			Kind:     SourceEnvRefreshToken,
			Path:     "",
			Writable: false,
		}, nil
	}

	return nil, errors.New(
		"no credential source resolved: set KIRO_CLI_DB_FILE, KIRO_CREDS_FILE, or REFRESH_TOKEN",
	)
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
