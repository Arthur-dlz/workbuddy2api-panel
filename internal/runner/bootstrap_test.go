package runner

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func unwritableFirstRunConfigPath(t *testing.T) (string, error) {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		parentFile := filepath.Join(dir, "not-a-directory")
		if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(parentFile, "config.json"), os.ErrNotExist
	}
	danglingDir := filepath.Join(dir, "dangling-dir")
	if err := os.Symlink(filepath.Join(dir, "missing-target"), danglingDir); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(danglingDir, "config.json"), os.ErrExist
}

func TestLoadOrCreateConfigFailsClosedWhenFirstRunWriteFails(t *testing.T) {
	t.Setenv("WB2A_API_KEY", "")
	t.Setenv("WB2A_LISTEN", "")
	t.Setenv("WB2A_PORT", "")
	path, wantErr := unwritableFirstRunConfigPath(t)
	_, err := loadOrCreateConfig(path)
	if err == nil {
		t.Fatal("expected first-run config write failure")
	}
	if !strings.Contains(err.Error(), "is missing and could not be created") {
		t.Fatalf("error should come from first-run creation failure, got %v", err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error should preserve bootstrap cause %v, got %v", wantErr, err)
	}
}

func TestLoadOrCreateConfigAllowsExplicitEnvironmentAPIKeyFallback(t *testing.T) {
	t.Setenv("WB2A_API_KEY", "explicit-test-key")
	t.Setenv("WB2A_LISTEN", "127.0.0.1:7777")
	path, _ := unwritableFirstRunConfigPath(t)
	cfg, err := loadOrCreateConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "explicit-test-key" || cfg.Listen != "127.0.0.1:7777" {
		t.Fatalf("explicit environment config not preserved: api_key_set=%v listen=%q", cfg.APIKey != "", cfg.Listen)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("fallback path must not exist on disk, stat err=%v", statErr)
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
