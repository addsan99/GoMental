// Package serverconfig loads and validates configuration for headless server
// mode (`gomental serve`): listen address, workspace root, auth mode, TLS, CORS,
// and rate limits. Values resolve with precedence: explicit flag > environment
// variable > JSON config file > built-in default.
package serverconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultAddr is the listen address when none is configured.
	DefaultAddr = ":8080"

	// DefaultAuthMode is the default identity posture: trust every request on
	// the assumption the server runs on a trusted LAN / behind a reverse proxy.
	DefaultAuthMode = "trustall"

	// Default per-actor token-bucket rates (requests/sec).
	DefaultRequestRate = 50
	DefaultWriteRate   = 10

	// DefaultGitRef is the branch/tag tracked when a git remote is configured but
	// no ref is given.
	DefaultGitRef = "main"

	// Environment variable names.
	EnvAddr             = "GOMENTAL_ADDR"
	EnvWorkspace        = "GOMENTAL_WORKSPACE"
	EnvAuthMode         = "GOMENTAL_AUTH"
	EnvTLSCert          = "GOMENTAL_TLS_CERT"
	EnvTLSKey           = "GOMENTAL_TLS_KEY"
	EnvCORSOrigins      = "GOMENTAL_CORS_ORIGINS"
	EnvRequestRate      = "GOMENTAL_REQUEST_RATE"
	EnvWriteRate        = "GOMENTAL_WRITE_RATE"
	EnvGitRemote        = "GOMENTAL_GIT_REMOTE"
	EnvGitRef           = "GOMENTAL_GIT_REF"
	EnvGitPoll          = "GOMENTAL_GIT_POLL"
	EnvGitWebhookSecret = "GOMENTAL_GIT_WEBHOOK_SECRET"
	EnvReadOnly         = "GOMENTAL_READ_ONLY"
)

// Config is the resolved, validated server configuration.
type Config struct {
	// Addr is the TCP listen address, e.g. ":8080" or "127.0.0.1:8080".
	Addr string
	// WorkspaceRoot is the absolute path to the single workspace the server owns.
	WorkspaceRoot string
	// AuthMode selects the identity posture. "trustall" (default) enforces
	// nothing; other modes are the extension point for real authenticators.
	AuthMode string
	// TLSCertFile / TLSKeyFile enable HTTPS when both are set.
	TLSCertFile string
	TLSKeyFile  string
	// AllowedOrigins is the CORS allow-list for the browser SPA (exact origins);
	// empty disables CORS (same-origin only).
	AllowedOrigins []string
	// RequestRate / WriteRate are per-actor token-bucket rates (requests/sec).
	RequestRate float64
	WriteRate   float64

	// GitRemote, when non-empty, puts the server in git-viewer mode: the
	// workspace is a working copy of this remote, advanced by fetch+reset. Empty
	// disables all git behavior.
	GitRemote string
	// GitRef is the branch/tag tracked (default "main").
	GitRef string
	// GitPollInterval, when > 0, polls the remote on this interval; 0 means
	// webhook-driven only (POST /api/git/sync).
	GitPollInterval time.Duration
	// GitWebhookSecret authenticates keyless POST /api/git/sync calls (a git host
	// webhook) via the X-GoMental-Token header or ?token= query param.
	GitWebhookSecret string
	// ReadOnly rejects content-mutating routes (git is the source of truth).
	// Defaults to true when GitRemote is set, false otherwise.
	ReadOnly bool
}

// GitEnabled reports whether the server is in git-viewer mode.
func (c Config) GitEnabled() bool { return strings.TrimSpace(c.GitRemote) != "" }

// TLSEnabled reports whether both cert and key are configured.
func (c Config) TLSEnabled() bool { return c.TLSCertFile != "" && c.TLSKeyFile != "" }

// Options carries raw (usually flag-derived) values before defaulting/env fallback.
// String fields left empty fall back to env, then the config file, then defaults.
type Options struct {
	ConfigFile    string
	Addr          string
	WorkspaceRoot string
	AuthMode      string
	TLSCertFile   string
	TLSKeyFile    string
	CORSOrigins   string // comma-separated
	RequestRate   string
	WriteRate     string
	GitRemote     string
	GitRef        string
	GitPoll       string // Go duration, e.g. "2m"; empty/"0" disables polling
	GitWebhookSecret string
	ReadOnly      string // tri-state: "" = derive from GitRemote; "true"/"false" override
}

// fileConfig mirrors Config for JSON config-file loading.
type fileConfig struct {
	Addr           string   `json:"addr"`
	Workspace      string   `json:"workspace"`
	AuthMode       string   `json:"authMode"`
	TLSCertFile    string   `json:"tlsCertFile"`
	TLSKeyFile     string   `json:"tlsKeyFile"`
	AllowedOrigins []string `json:"allowedOrigins"`
	RequestRate    float64  `json:"requestRate"`
	WriteRate      float64  `json:"writeRate"`
	GitRemote        string `json:"gitRemote"`
	GitRef           string `json:"gitRef"`
	GitPoll          string `json:"gitPoll"`
	GitWebhookSecret string `json:"gitWebhookSecret"`
	ReadOnly         *bool  `json:"readOnly"`
}

// Load resolves a Config with precedence flag > env > file > default, then validates.
func Load(opts Options) (Config, error) {
	var file fileConfig
	if opts.ConfigFile != "" {
		data, err := os.ReadFile(opts.ConfigFile)
		if err != nil {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
		if err := json.Unmarshal(data, &file); err != nil {
			return Config{}, fmt.Errorf("parse config file: %w", err)
		}
	}

	cfg := Config{
		Addr:           firstNonEmpty(opts.Addr, os.Getenv(EnvAddr), file.Addr, DefaultAddr),
		WorkspaceRoot:  firstNonEmpty(opts.WorkspaceRoot, os.Getenv(EnvWorkspace), file.Workspace),
		AuthMode:       firstNonEmpty(opts.AuthMode, os.Getenv(EnvAuthMode), file.AuthMode, DefaultAuthMode),
		TLSCertFile:    firstNonEmpty(opts.TLSCertFile, os.Getenv(EnvTLSCert), file.TLSCertFile),
		TLSKeyFile:     firstNonEmpty(opts.TLSKeyFile, os.Getenv(EnvTLSKey), file.TLSKeyFile),
		AllowedOrigins: splitOrigins(firstNonEmpty(opts.CORSOrigins, os.Getenv(EnvCORSOrigins), strings.Join(file.AllowedOrigins, ","))),
		RequestRate:    firstFloat(opts.RequestRate, os.Getenv(EnvRequestRate), file.RequestRate, DefaultRequestRate),
		WriteRate:      firstFloat(opts.WriteRate, os.Getenv(EnvWriteRate), file.WriteRate, DefaultWriteRate),
		GitRemote:        firstNonEmpty(opts.GitRemote, os.Getenv(EnvGitRemote), file.GitRemote),
		GitRef:           firstNonEmpty(opts.GitRef, os.Getenv(EnvGitRef), file.GitRef, DefaultGitRef),
		GitWebhookSecret: firstNonEmpty(opts.GitWebhookSecret, os.Getenv(EnvGitWebhookSecret), file.GitWebhookSecret),
	}
	cfg.Addr = strings.TrimSpace(cfg.Addr)
	cfg.WorkspaceRoot = strings.TrimSpace(cfg.WorkspaceRoot)
	cfg.AuthMode = strings.TrimSpace(cfg.AuthMode)
	cfg.GitRemote = strings.TrimSpace(cfg.GitRemote)
	cfg.GitRef = strings.TrimSpace(cfg.GitRef)

	poll, err := parseDuration(firstNonEmpty(opts.GitPoll, os.Getenv(EnvGitPoll), file.GitPoll))
	if err != nil {
		return Config{}, fmt.Errorf("invalid git poll interval: %w", err)
	}
	cfg.GitPollInterval = poll

	// ReadOnly is tri-state: an explicit flag/env/file value wins; otherwise it
	// defaults to on iff a git remote is configured (git is the source of truth).
	if ro, set, err := resolveBool(opts.ReadOnly, os.Getenv(EnvReadOnly), file.ReadOnly); err != nil {
		return Config{}, fmt.Errorf("invalid read-only value: %w", err)
	} else if set {
		cfg.ReadOnly = ro
	} else {
		cfg.ReadOnly = cfg.GitEnabled()
	}

	if err := cfg.normalizeAndValidate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) normalizeAndValidate() error {
	if c.Addr == "" {
		return errors.New("server address is empty")
	}
	if c.WorkspaceRoot == "" {
		return errors.New("workspace root is required (set --workspace or " + EnvWorkspace + ")")
	}
	abs, err := filepath.Abs(c.WorkspaceRoot)
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}
	// In git-viewer mode the working copy may not exist yet — it is cloned on
	// startup — so a missing directory is acceptable; only reject a path that
	// exists and is not a directory. Without a git remote the workspace must
	// already be a real directory.
	info, statErr := os.Stat(abs)
	switch {
	case statErr == nil:
		if !info.IsDir() {
			return fmt.Errorf("workspace root %q is not a directory", abs)
		}
	case os.IsNotExist(statErr):
		if !c.GitEnabled() {
			return fmt.Errorf("workspace root %q is not accessible: %w", abs, statErr)
		}
	default:
		return fmt.Errorf("workspace root %q is not accessible: %w", abs, statErr)
	}
	c.WorkspaceRoot = abs

	if c.GitEnabled() && c.GitRef == "" {
		c.GitRef = DefaultGitRef
	}
	if c.GitPollInterval < 0 {
		c.GitPollInterval = 0
	}

	if c.AuthMode == "" {
		c.AuthMode = DefaultAuthMode
	}
	switch c.AuthMode {
	case "trustall":
	default:
		return fmt.Errorf("unsupported auth mode %q (supported: trustall)", c.AuthMode)
	}

	// TLS must be all-or-nothing.
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return errors.New("both --tls-cert and --tls-key are required to enable TLS")
	}
	if c.TLSEnabled() {
		if _, err := os.Stat(c.TLSCertFile); err != nil {
			return fmt.Errorf("tls cert not accessible: %w", err)
		}
		if _, err := os.Stat(c.TLSKeyFile); err != nil {
			return fmt.Errorf("tls key not accessible: %w", err)
		}
	}
	if c.RequestRate <= 0 {
		c.RequestRate = DefaultRequestRate
	}
	if c.WriteRate <= 0 {
		c.WriteRate = DefaultWriteRate
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstFloat(flagVal, envVal string, fileVal, def float64) float64 {
	for _, s := range []string{flagVal, envVal} {
		if strings.TrimSpace(s) != "" {
			if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && f > 0 {
				return f
			}
		}
	}
	if fileVal > 0 {
		return fileVal
	}
	return def
}

// parseDuration parses a Go duration string; empty or "0" means "disabled" (0).
// A bare integer is rejected to avoid the ambiguous "5" (ns? seconds?).
func parseDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, nil
	}
	return d, nil
}

// resolveBool evaluates a tri-state boolean from flag > env > file. It returns
// (value, set, error); set is false when none of the sources specified a value,
// letting the caller apply a context-dependent default.
func resolveBool(flagVal, envVal string, fileVal *bool) (value bool, set bool, err error) {
	for _, s := range []string{flagVal, envVal} {
		if strings.TrimSpace(s) == "" {
			continue
		}
		b, perr := strconv.ParseBool(strings.TrimSpace(s))
		if perr != nil {
			return false, false, fmt.Errorf("%q is not a boolean", s)
		}
		return b, true, nil
	}
	if fileVal != nil {
		return *fileVal, true, nil
	}
	return false, false, nil
}

func splitOrigins(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
