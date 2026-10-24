package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIKeyImportAndPrecedence(t *testing.T) {
	t.Setenv("ALIVYN_HOME", t.TempDir())
	t.Setenv("ALIVYN_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	deepseek := Config{SchemaVersion: 1, Name: "Ari", Language: "en", Provider: "deepseek", BaseURL: DefaultDeepSeekURL, Model: DefaultDeepSeekModel}
	custom := Config{SchemaVersion: 1, Name: "Ari", Language: "en", Provider: "custom", BaseURL: "https://models.example/v1", Model: "other"}
	if err := SaveAPIKey(deepseek, "private-key\n"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.Getenv("ALIVYN_HOME"), "model.key")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private key permissions: %v, %v", info, err)
	}
	key, err := APIKey(deepseek)
	if err != nil || key != "private-key" {
		t.Fatalf("stored key not loaded: %q, %v", key, err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "provider-env")
	key, err = APIKey(deepseek)
	if err != nil || key != "provider-env" {
		t.Fatalf("provider override not used: %q, %v", key, err)
	}
	key, err = APIKey(custom)
	if err == nil || key != "" {
		t.Fatalf("DeepSeek key was accepted for custom provider: %q, %v", key, err)
	}
	t.Setenv("ALIVYN_API_KEY", "alivyn-env")
	key, err = APIKey(custom)
	if err != nil || key != "alivyn-env" {
		t.Fatalf("Alivyn override not used: %q, %v", key, err)
	}
}

func TestAPIKeyRejectsOpenPermissionsAndDoesNotLeak(t *testing.T) {
	t.Setenv("ALIVYN_HOME", t.TempDir())
	t.Setenv("ALIVYN_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	c := Config{SchemaVersion: 1, Name: "Ari", Language: "en", Provider: "deepseek", BaseURL: DefaultDeepSeekURL, Model: DefaultDeepSeekModel}
	if err := SaveAPIKey(c, "never-print-this-key"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.Getenv("ALIVYN_HOME"), "model.key")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := APIKey(c)
	if err == nil || strings.Contains(err.Error(), "never-print-this-key") {
		t.Fatalf("unsafe key file was accepted or leaked: %v", err)
	}
}
