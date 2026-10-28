package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// abs returns the platform-absolute form of a unix-style test path, as the code under test computes it.
func abs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(filepath.FromSlash(p))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "obsidian.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadVaultFromConfig_PrefersOpen(t *testing.T) {
	cfg := writeConfig(t, `{"vaults":{
		"a":{"path":"/v/a","ts":300},
		"b":{"path":"/v/b","ts":100,"open":true},
		"c":{"path":"/v/c","ts":200}}}`)
	for i := 0; i < 20; i++ { // map iteration is random; result must not be
		got, err := readVaultFromConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got != abs(t, "/v/b") {
			t.Fatalf("got %s, want /v/b", got)
		}
	}
}

func TestReadVaultFromConfig_HighestTimestamp(t *testing.T) {
	cfg := writeConfig(t, `{"vaults":{
		"a":{"path":"/v/a","ts":100},
		"b":{"path":"/v/b","ts":300},
		"c":{"path":"/v/c","ts":200},
		"d":{"path":"","ts":999}}}`)
	for i := 0; i < 20; i++ {
		got, err := readVaultFromConfig(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got != abs(t, "/v/b") {
			t.Fatalf("got %s, want /v/b", got)
		}
	}
}

func TestReadVaultFromConfig_Errors(t *testing.T) {
	if _, err := readVaultFromConfig(writeConfig(t, `{"vaults":{}}`)); err == nil {
		t.Error("expected error for no vaults")
	}
	if _, err := readVaultFromConfig(writeConfig(t, `not json`)); err == nil {
		t.Error("expected error for bad json")
	}
	if _, err := readVaultFromConfig(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestDetect_Priority(t *testing.T) {
	cfg := writeConfig(t, `{"vaults":{"a":{"path":"/v/cfg","ts":1}}}`)

	got, err := detect("/v/flag", "/v/env", cfg)
	if err != nil || got != abs(t, "/v/flag") {
		t.Fatalf("flag: got %q, %v", got, err)
	}
	got, err = detect("", "/v/env", cfg)
	if err != nil || got != abs(t, "/v/env") {
		t.Fatalf("env: got %q, %v", got, err)
	}
	got, err = detect("", "", cfg)
	if err != nil || got != abs(t, "/v/cfg") {
		t.Fatalf("config: got %q, %v", got, err)
	}
	if _, err = detect("", "", ""); err == nil {
		t.Fatal("expected error when nothing configured")
	}
}
