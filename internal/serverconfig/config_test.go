package serverconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndPrecedence(t *testing.T) {
	root := t.TempDir()
	cfg, err := Load(Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Addr != DefaultAddr || cfg.AuthMode != DefaultAuthMode {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	if cfg.RequestRate != DefaultRequestRate || cfg.WriteRate != DefaultWriteRate {
		t.Fatalf("unexpected rate defaults: %#v", cfg)
	}
	if cfg.TLSEnabled() {
		t.Fatal("TLS should be off by default")
	}
}

func TestLoadRequiresWorkspace(t *testing.T) {
	if _, err := Load(Options{}); err == nil {
		t.Fatal("expected error without workspace")
	}
}

func TestLoadConfigFileAndOverride(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "cfg.json")
	body := `{"addr":":9999","workspace":"` + filepath.ToSlash(root) + `","requestRate":7,"allowedOrigins":["https://a.example"]}`
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// File provides addr :9999; flag overrides to :7777.
	cfg, err := Load(Options{ConfigFile: cfgPath, Addr: ":7777"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Addr != ":7777" {
		t.Fatalf("flag should override file addr, got %q", cfg.Addr)
	}
	if cfg.RequestRate != 7 {
		t.Fatalf("file requestRate should apply, got %v", cfg.RequestRate)
	}
	if len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "https://a.example" {
		t.Fatalf("unexpected origins: %#v", cfg.AllowedOrigins)
	}
}

func TestTLSAllOrNothing(t *testing.T) {
	root := t.TempDir()
	if _, err := Load(Options{WorkspaceRoot: root, TLSCertFile: "only-cert.pem"}); err == nil {
		t.Fatal("expected error when only cert is set")
	}
}

func TestUnsupportedAuthMode(t *testing.T) {
	root := t.TempDir()
	if _, err := Load(Options{WorkspaceRoot: root, AuthMode: "oidc"}); err == nil {
		t.Fatal("expected error for unsupported auth mode")
	}
}
