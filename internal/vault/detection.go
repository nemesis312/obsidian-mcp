package vault

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DetectVault resolves vault path using priority order:
// 1. --vault CLI flag
// 2. OBSIDIAN_VAULT_PATH env var
// 3. Obsidian config file (platform-specific)
func DetectVault() (string, error) {
	var vaultFlag string
	flag.StringVar(&vaultFlag, "vault", "", "Path to Obsidian vault")
	flag.Parse()

	// Priority 1: CLI flag
	if vaultFlag != "" {
		abs, err := filepath.Abs(vaultFlag)
		if err != nil {
			return "", fmt.Errorf("invalid vault path: %w", err)
		}
		return abs, nil
	}

	// Priority 2: Environment variable
	if envPath := os.Getenv("OBSIDIAN_VAULT_PATH"); envPath != "" {
		abs, err := filepath.Abs(envPath)
		if err != nil {
			return "", fmt.Errorf("invalid vault path from env: %w", err)
		}
		return abs, nil
	}

	// Priority 3: Obsidian config
	configPath := getObsidianConfigPath()
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
		} `json:"vaults"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return "", err
	}

	// Return first vault (most recently opened)
	for _, v := range config.Vaults {
		if v.Path != "" {
			abs, err := filepath.Abs(v.Path)
			if err != nil {
				continue
			}
			return abs, nil
		}
	}

	return "", fmt.Errorf("no vaults in config")
}
