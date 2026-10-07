package runner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateConfigFailsClosedWhenFirstRunWriteFails(t *testing.T) {
	t.Setenv("WB2A_API_KEY", "")
	t.Setenv("WB2A_LISTEN", "")
	t.Setenv("WB2A_PORT", "")
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadOrCreateConfig(filepath.Join(parentFile, "config.json"))
	if err == nil {
		t.Fatal("expected first-run config write failure")
	}
	if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
		t.Fatalf("error should preserve bootstrap cause, got %v", err)
	}
}

func TestLoadOrCreateConfigAllowsExplicitEnvironmentAPIKeyFallback(t *testing.T) {
	t.Setenv("WB2A_API_KEY", "explicit-test-key")
	t.Setenv("WB2A_LISTEN", "127.0.0.1:7777")
	dir := t.TempDir()
	parentFile := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadOrCreateConfig(filepath.Join(parentFile, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "explicit-test-key" || cfg.Listen != "127.0.0.1:7777" {
		t.Fatalf("explicit environment config not preserved: api_key_set=%v listen=%q", cfg.APIKey != "", cfg.Listen)
	}
}

func TestLoadOrCreateConfigGeneratesNormalFirstRunConfig(t *testing.T) {
	t.Setenv("WB2A_API_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg, err := loadOrCreateConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey == "" {
		t.Fatal("generated first-run config must have an API key")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("generated config not written: %v", err)
	}
}

func TestLoadOrCreateConfigKeepsExplicitFileSemantics(t *testing.T) {
	t.Setenv("WB2A_API_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"api_key":"","listen":"127.0.0.1:7863"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadOrCreateConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "" || cfg.Listen != "127.0.0.1:7863" {
		t.Fatalf("explicit config semantics changed: api_key_set=%v listen=%q", cfg.APIKey != "", cfg.Listen)
	}
}
