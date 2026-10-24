package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const credentialFile = "model.key"

type credential struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	Key      string `json:"key"`
}

// APIKey prefers an explicit Alivyn override, then a provider environment
// variable, then the private key file. Keys are never stored in config.json.
func APIKey(c Config) (string, error) {
	if key := os.Getenv("ALIVYN_API_KEY"); key != "" {
		return key, nil
	}
	if c.Provider == "deepseek" {
		if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
			return key, nil
		}
	}
	path, err := credentialPath()
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", errors.New("model API key is missing; run 'alivyn model key import' or set ALIVYN_API_KEY")
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("model key file must be a regular file with 0600 permissions")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var saved credential
	if err := json.Unmarshal(data, &saved); err != nil {
		return "", errors.New("invalid model key file; import the key again")
	}
	if saved.Provider != c.Provider || saved.BaseURL != c.BaseURL {
		return "", errors.New("model key belongs to a different provider or endpoint; import a matching key")
	}
	if err := validateAPIKey(saved.Key); err != nil {
		return "", err
	}
	return saved.Key, nil
}

func SaveAPIKey(c Config, key string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if err := validateAPIKey(key); err != nil {
		return err
	}
	path, err := credentialPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".model-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if err := json.NewEncoder(file).Encode(credential{Provider: c.Provider, BaseURL: c.BaseURL, Key: key}); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func credentialPath() (string, error) {
	dir, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialFile), nil
}

func validateAPIKey(key string) error {
	if key == "" || len(key) > 8192 || strings.IndexFunc(key, unicode.IsSpace) >= 0 || strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return errors.New("invalid model API key")
	}
	return nil
}
