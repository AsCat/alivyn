package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const DefaultDeepSeekURL = "https://api.deepseek.com"
const DefaultDeepSeekModel = "deepseek-flash"

type Config struct {
	SchemaVersion int    `json:"schema_version"`
	Name          string `json:"name"`
	Language      string `json:"language"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"base_url"`
	Model         string `json:"model"`
}

func Directory() (string, error) {
	if value := os.Getenv("ALIVYN_HOME"); value != "" {
		return filepath.Abs(value)
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "alivyn"), nil
}

func Path() (string, error) {
	dir, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func (c Config) Validate() error {
	if c.SchemaVersion != 1 {
		return errors.New("unsupported config schema")
	}
	if strings.TrimSpace(c.Name) == "" || strings.ContainsAny(c.Name, "\r\n") {
		return errors.New("name must be nonempty and single-line")
	}
	if c.Language != "en" && c.Language != "zh-CN" {
		return errors.New("language must be en or zh-CN")
	}
	if c.Provider != "deepseek" && c.Provider != "custom" {
		return errors.New("provider must be deepseek or custom")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("base URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if strings.TrimSpace(c.Model) == "" || strings.ContainsAny(c.Model, "\r\n") {
		return errors.New("model must be nonempty and single-line")
	}
	return nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, errors.New("not initialized; run 'alivyn init'")
	}
	if err != nil {
		return Config{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Config{}, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return Config{}, errors.New("config permissions are too open; expected 0600")
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return c, c.Validate()
}

func SaveNew(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return errors.New("already initialized; existing identity was left untouched")
	}
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(c)
}
