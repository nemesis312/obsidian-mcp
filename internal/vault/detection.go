package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DetectVault resolves vault path using priority order:
// 1. vaultFlag (the value of the --vault CLI flag, parsed by the caller)
// 2. OBSIDIAN_VAULT_PATH env var
// 3. Obsidian config file (platform-specific)
func DetectVault(vaultFlag string) (string, error) {
	return detect(vaultFlag, os.Getenv("OBSIDIAN_VAULT_PATH"), getObsidianConfigPath())
}

func detect(vaultFlag, envPath, configPath string) (string, error) {
	// Priority 1: CLI flag
	if vaultFlag != "" {
		abs, err := filepath.Abs(vaultFlag)
		if err != nil {
			return "", fmt.Errorf("invalid vault path: %w", err)
		}
		return abs, nil
	}

	// Priority 2: Environment variable
	if envPath != "" {
		abs, err := filepath.Abs(envPath)
		if err != nil {
			return "", fmt.Errorf("invalid vault path from env: %w", err)
		}
		return abs, nil
	}

	// Priority 3: Obsidian config
	if configPath != "" {
		if vaultPath, err := readVaultFromConfig(configPath); err == nil {
			return vaultPath, nil
		}
	}

	return "", fmt.Errorf("vault not found: use --vault flag or set OBSIDIAN_VAULT_PATH")
}

func getObsidianConfigPath() string {
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library/Application Support/obsidian/obsidian.json")
	case "linux":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config/obsidian/obsidian.json")
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "obsidian/obsidian.json")
	default:
		return ""
	}
}

func readVaultFromConfig(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var config struct {
		Vaults map[string]struct {
			Path string `json:"path"`
			TS   int64  `json:"ts"`
			Open bool   `json:"open"`
		} `json:"vaults"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return "", err
	}

	// Deterministic choice: a vault flagged "open" wins; otherwise the highest
	// "ts" (last opened). Remaining ties break on path so output is stable.
	best, found := "", false
	var bestOpen bool
	var bestTS int64
	for _, v := range config.Vaults {
		if v.Path == "" {
			continue
		}
		abs, err := filepath.Abs(v.Path)
		if err != nil {
			continue
		}
		better := !found ||
			(v.Open && !bestOpen) ||
			(v.Open == bestOpen && v.TS > bestTS) ||
			(v.Open == bestOpen && v.TS == bestTS && abs < best)
		if better {
			best, found, bestOpen, bestTS = abs, true, v.Open, v.TS
		}
	}
	if !found {
		return "", fmt.Errorf("no vaults in config")
	}
	return best, nil
}
